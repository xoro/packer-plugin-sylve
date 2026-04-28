// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepCreateVM sends POST /api/vm to create the temporary installation VM.
// After creation it queries the simple VM list to resolve the database ID,
// libvirt RID and first NIC MAC address, storing them in the state bag under
// "vm_id", "vm_rid" and "vm_mac".
// Cleanup deletes the VM when the build failed or was cancelled, or when
// destroy=true on success (StepDeleteVM may have deleted already). When
// destroy=false and the build succeeded, Cleanup leaves the VM on Sylve.
type StepCreateVM struct {
	Config *Config
	vmID   uint
	vmRID  uint
	ctx    context.Context
}

// createVMPollInterval is overridable in tests.
var createVMPollInterval = 2 * time.Second

// createVMListDeadline bounds how long Run waits for the new VM to appear in
// the simple list API; tests may shorten it to hit the not-found path.
var createVMListDeadline = 60 * time.Second

// randReadFn backs RID generation; tests may replace it to simulate failure.
var randReadFn = rand.Read

// isRemoteHostForVNCPort mirrors isRemoteHost for selectVNCPort; tests may
// replace it to exercise the remote TCP probe branch without depending on DNS.
var isRemoteHostForVNCPort = isRemoteHost

// exclusiveListenFn attempts to bind addr on TCP without SO_REUSEADDR, so that
// TIME_WAIT sockets left by previous VNC connections are detected as occupied.
// Bhyve's fbuf device binds without SO_REUSEADDR; Go's net.Listen uses
// SO_REUSEADDR and would silently succeed on a port with a TIME_WAIT socket,
// causing bhyve to fail when it tries the same bind milliseconds later.
// The function returns nil when the port is exclusively free, non-nil otherwise.
// Overridable in tests to simulate TIME_WAIT conditions without real sockets.
var exclusiveListenFn = func(addr string) error {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var setSockoptErr error
			if err := c.Control(func(fd uintptr) {
				// Clear SO_REUSEADDR that Go's net package sets by default.
				// The subsequent bind(2) then fails when a TIME_WAIT socket
				// occupies the port, matching bhyve's behaviour.
				setSockoptErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
			}); err != nil {
				return err
			}
			return setSockoptErr
		},
	}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}

// dialTCPForVNCPortProbe is the TCP dial used when VNCHost appears remote;
// tests may replace it to simulate a successful probe without a real service.
var dialTCPForVNCPortProbe = func(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// netInterfacesForRemoteHost is used by isRemoteHost; tests may replace it to
// simulate net.Interfaces failures.
var netInterfacesForRemoteHost = net.Interfaces

// lookupHostFn is used by isRemoteHost; tests may replace it to simulate DNS failures.
var lookupHostFn = net.LookupHost

// ifaceAddrsFn is used by isRemoteHost when walking interfaces; tests may replace it.
var ifaceAddrsFn = func(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

func (s *StepCreateVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	s.ctx = ctx
	ui := state.Get("ui").(packersdk.Ui)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	isoUUID, _ := state.Get("iso_uuid").(string)

	vmName := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(s.Config.VMName, "_")

	// Select a VNC port not already assigned to any existing Sylve VM.
	// The listener is kept open and stored in the state bag so the view
	// server can take it over without a TOCTOU race.
	if err := s.selectVNCPort(c, state); err != nil {
		state.Put("error", fmt.Errorf("vnc port selection: %w", err))
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	storageSizeBytes := int64(s.Config.StorageSizeMB) * 1024 * 1024

	// RID must be non-zero; pick a random slot in 1..99 and retry on collision
	// or stale-artifact errors caused by a previous interrupted build leaving a
	// ZFS dataset behind for that RID.
	const maxRIDAttempts = 10
	var createErr error
	for attempt := 0; attempt < maxRIDAttempts; attempt++ {
		var ridBuf [1]byte
		if _, err := randReadFn(ridBuf[:]); err != nil {
			state.Put("error", fmt.Errorf("generate random RID: %w", err))
			return multistep.ActionHalt
		}
		rid := uint(binary.BigEndian.Uint16([]byte{0, ridBuf[0]})%99) + 1

		req := client.CreateVMRequest{
			Name: vmName,
			RID:  &rid,
			ISO:  isoUUID,

			StoragePool:          s.Config.StoragePool,
			StorageType:          s.Config.StorageType,
			StorageSize:          &storageSizeBytes,
			StorageEmulationType: s.Config.StorageEmulationType,

			SwitchName:          s.Config.SwitchName,
			SwitchEmulationType: s.Config.SwitchEmulationType,

			CPUSockets: s.Config.CPUSockets,
			CPUCores:   s.Config.CPUCores,
			CPUThreads: s.Config.CPUThreads,

			RAM: s.Config.RAM * 1024 * 1024,

			VNCPort:       s.Config.VNCPort,
			VNCPassword:   s.Config.VNCPassword,
			VNCResolution: s.Config.VNCResolution,
			VNCWait:       boolPtr(false),
			Loader:        s.Config.Loader,

			TimeOffset: s.Config.TimeOffset,

			ACPI: &s.Config.ACPI,
			APIC: &s.Config.APIC,
		}

		if dbg, err := json.Marshal(req); err == nil {
			log.Printf("[DEBUG] CreateVM request JSON: %s", string(dbg))
		}
		ui.Say(fmt.Sprintf("Creating VM %q...", vmName))
		createErr = c.CreateVM(req)
		if createErr == nil {
			break
		}
		errStr := createErr.Error()
		if strings.Contains(errStr, "vm_create_stale_artifacts_detected") ||
			strings.Contains(errStr, "rid_or_name_already_in_use") {
			ui.Say(fmt.Sprintf("RID collision/stale artifacts (attempt %d/%d), retrying with new RID...", attempt+1, maxRIDAttempts))
			continue
		}
		// Any other error is not retryable.
		break
	}
	if createErr != nil {
		state.Put("error", fmt.Errorf("create VM: %w", createErr))
		ui.Error(createErr.Error())
		return multistep.ActionHalt
	}

	ui.Say("Waiting for VM to appear in Sylve...")
	var found *client.SimpleVM
	deadline := time.Now().Add(createVMListDeadline)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(createVMPollInterval):
		}
		vms, err := c.ListVMsSimple()
		if err != nil {
			continue
		}
		for i := range vms {
			if vms[i].Name == vmName {
				found = &vms[i]
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		err := fmt.Errorf("VM %q not found in Sylve after creation", vmName)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	vm, err := c.GetVMByRID(found.RID)
	if err != nil {
		state.Put("error", fmt.Errorf("get VM after creation: %w", err))
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	var mac string
	if len(vm.Networks) > 0 {
		mac = vm.Networks[0].MACAddress()
	}

	// Sylve assigns BootOrder=0 to the ISO and BootOrder=1 to the zvol.
	// SyncVMDisks orders storages by boot_order ASC when building the bhyve
	// command line, so the ISO ends up in the lower-numbered slot and the
	// bhyve UEFI always boots it first — even after the OS has been installed.
	// Raising the ISO to BootOrder=100 puts the zvol in slot 10 (tried first)
	// and the ISO in slot 11 (fallback).  The UEFI then boots the zvol when it
	// has an EFI bootloader (installed OS) and falls back to the ISO when it is
	// empty (first installer boot).
	for _, st := range vm.Storages {
		if st.Type == "image" {
			log.Printf("[DEBUG] Setting ISO storage id=%d boot order to 100...", st.ID)
			name := st.Name
			if name == "" {
				name = "iso"
			}
			if fixErr := c.UpdateStorageBootOrder(int(st.ID), name, st.Emulation, 100); fixErr != nil {
				ui.Say(fmt.Sprintf("Warning: could not set ISO boot order id=%d: %s", st.ID, fixErr))
			}
			break
		}
	}

	// Disable startAtBoot so Sylve never auto-restarts the VM after a stop.
	// With startAtBoot=true (Sylve's default) Sylve queues a restart immediately
	// when the VM stops; this restart races with the plugin's DisableISOStorage
	// + second StartVM sequence, causing the second boot to launch with the ISO
	// still enabled and creating a third (phantom) bhyve invocation.
	if err := c.DisableStartAtBoot(vm.RID); err != nil {
		ui.Say(fmt.Sprintf("Warning: could not disable startAtBoot for rid=%d: %s", vm.RID, err))
	}

	// Re-fetch so the debug summary reflects the updated boot orders.
	if updated, err := c.GetVMByRID(vm.RID); err == nil {
		vm = updated
	}

	s.vmID = vm.ID
	s.vmRID = vm.RID
	state.Put("vm_id", vm.ID)
	state.Put("vm_rid", vm.RID)
	// vm_rid_final is never zeroed and is read by buildArtifact after StepDeleteVM
	// has cleared vm_rid. This allows the artifact to report the correct RID even
	// when destroy=true.
	state.Put("vm_rid_final", vm.RID)
	state.Put("vm_mac", mac)

	// Stash the ISO storage so StepRestartAfterInstall can disable it before
	// the second start.  UEFI NVRAM persists across VM starts and stores the
	// CD as the first boot entry after the installer run; disabling the ISO
	// storage causes SyncVMDisks to omit it from the bhyve command line,
	// forcing UEFI to boot from the zvol on the installed-OS start.
	for _, st := range vm.Storages {
		if st.Type == "image" {
			state.Put("iso_storage_id", int(st.ID))
			isoName := st.Name
			if isoName == "" {
				isoName = "iso"
			}
			state.Put("iso_storage_name", isoName)
			state.Put("iso_storage_emulation", st.Emulation)
			break
		}
	}

	for _, st := range vm.Storages {
		log.Printf("[DEBUG]   storage id=%d type=%-8s emulation=%-10s bootOrder=%d name=%s",
			st.ID, st.Type, st.Emulation, st.BootOrder, st.Name)
	}
	ui.Say(fmt.Sprintf("VM created: id=%d rid=%d mac=%s acpi=%v apic=%v", vm.ID, vm.RID, mac, vm.ACPI, vm.APIC))
	return multistep.ActionContinue
}

func boolPtr(b bool) *bool { return &b }

func (s *StepCreateVM) Cleanup(state multistep.StateBag) {
	if s.vmRID == 0 {
		return
	}
	// If StepDeleteVM already deleted the VM it zeroes vm_rid in the state bag.
	if rid, _ := state.Get("vm_rid").(uint); rid == 0 {
		return
	}
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	// Also treat a context cancellation (Ctrl+C during provisioners) as cancelled.
	// When StepProvision catches ctx.Done() it returns ActionHalt — so StateHalted
	// is set but StateCancelled is not. Checking ctx.Err() catches that case.
	if s.ctx != nil && s.ctx.Err() != nil {
		cancelled = true
	}
	// When keep_on_error=true and the build failed (halted), leave the VM
	// running so the operator can inspect it. Cancellation (Ctrl+C) always
	// cleans up regardless of keep_on_error.
	if s.Config.KeepOnError && halted && !cancelled {
		ui := state.Get("ui").(packersdk.Ui)
		ui.Say(fmt.Sprintf("keep_on_error=true: leaving VM rid=%d running for inspection", s.vmRID))
		return
	}
	// Successful build with destroy=false: StepDeleteVM does not delete; skip
	// Cleanup deletion too. Without this, the VM would be removed here despite
	// destroy=false because multistep runs Cleanup on every step after Run.
	if !s.Config.Destroy && !halted && !cancelled {
		return
	}
	ui := state.Get("ui").(packersdk.Ui)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)
	ui.Say(fmt.Sprintf("Cleanup: deleting VM rid=%d", s.vmRID))
	if err := c.DeleteVM(s.vmRID); err != nil {
		ui.Error(fmt.Sprintf("Cleanup: failed to delete VM rid=%d: %s", s.vmRID, err))
	}
}

// selectVNCPort queries Sylve for all existing VM VNC port assignments and
// picks the first port in [VNCPortMin, VNCPortMax] that satisfies all
// conditions:
//
//  1. Not already assigned to another Sylve VM (checked via API).
//  2. Not bound on the local machine running Packer (127.0.0.1:port must be
//     available to bind).
//  3. When VNCHost is a remote machine (does not resolve to a local interface):
//     a TCP connect to VNCHost:port must be refused, ruling out any publicly
//     reachable service on that port.
//     Note: Bhyve binds VNC to 127.0.0.1 so this probe cannot detect other
//     Bhyve VMs — that is handled by condition 1.  When Packer runs on the
//     same host as Sylve, condition 2 (listen probe on 127.0.0.1) catches
//     Bhyve-occupied ports directly, making the TCP dial redundant.
//
// The listener for the chosen port is kept open and stored in the state bag
// under "vnc_view_listener" so the view server can Accept() on it directly
// without a TOCTOU race. It stores the chosen port in s.Config.VNCPort.
func (s *StepCreateVM) selectVNCPort(c *client.Client, state multistep.StateBag) error {
	vms, err := c.ListVMsSimple()
	if err != nil {
		return fmt.Errorf("list VMs (VNC port selection): %w", err)
	}
	used := make(map[int]struct{}, len(vms))
	for _, vm := range vms {
		used[vm.VNCPort] = struct{}{}
	}

	remoteHost := isRemoteHostForVNCPort(s.Config.VNCHost)

	for port := s.Config.VNCPortMin; port <= s.Config.VNCPortMax; port++ {
		if _, taken := used[port]; taken {
			continue
		}
		// When VNCHost is on a remote machine, do a best-effort TCP probe to
		// detect publicly-reachable services using this port on that machine.
		// This does NOT detect Bhyve VMs (loopback-only) — condition 1 covers those.
		if remoteHost {
			conn, dialErr := dialTCPForVNCPortProbe("tcp", fmt.Sprintf("%s:%d", s.Config.VNCHost, port), 500*time.Millisecond)
			if dialErr == nil {
				// Something answered on the remote host — port is occupied.
				_ = conn.Close()
				continue
			}
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		// Verify the port is exclusively free before selecting it. Go's
		// net.Listen uses SO_REUSEADDR and silently succeeds on ports with
		// TIME_WAIT sockets left by previous VNC connections; bhyve binds
		// without SO_REUSEADDR and would fail on the same port. Skipping
		// TIME_WAIT ports here ensures the selected port is one bhyve can
		// actually bind.
		if err := exclusiveListenFn(addr); err != nil {
			continue
		}
		// Bind the port now and keep the listener open. The view server will
		// Accept() on this listener directly, eliminating any TOCTOU race.
		// When Packer runs on the same host as Sylve this also catches Bhyve's
		// loopback-bound ports (127.0.0.1:port).
		ln, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			continue
		}
		s.Config.VNCPort = port
		state.Put("vnc_view_listener", ln)
		return nil
	}
	return fmt.Errorf("no free VNC port in range %d-%d (all in use on Sylve host or local machine)",
		s.Config.VNCPortMin, s.Config.VNCPortMax)
}

// isRemoteHost returns true when host does not resolve to any local network
// interface address, i.e. Packer is running on a different machine from host.
func isRemoteHost(host string) bool {
	addrs, err := lookupHostFn(host)
	if err != nil {
		// Cannot resolve — assume remote to err on the side of more checking.
		return true
	}
	ifaces, err := netInterfacesForRemoteHost()
	if err != nil {
		return true
	}
	localIPs := make(map[string]struct{})
	for _, iface := range ifaces {
		ifAddrs, err := ifaceAddrsFn(iface)
		if err != nil {
			continue
		}
		for _, a := range ifAddrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				localIPs[ip.String()] = struct{}{}
			}
		}
	}
	for _, addr := range addrs {
		if _, local := localIPs[addr]; local {
			return false
		}
	}
	return true
}
