// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// discoverIPTotalTimeout and discoverIPPollInterval are tunable in tests.
var (
	discoverIPTotalTimeout = 90 * time.Second
	discoverIPPollInterval = 10 * time.Second
)

// execARPCommandLinux / execARPCommandBSD run fixed OS ARP cache commands (no
// user-controlled argv); tests may replace these vars.
var execARPCommandLinux = func() ([]byte, error) {
	return exec.Command("ip", "neigh", "show").Output()
}

var execARPCommandBSD = func() ([]byte, error) {
	return exec.Command("arp", "-an").Output()
}

// arpPreferLinux selects execARPCommandLinux vs execARPCommandBSD. The default
// matches runtime.GOOS; tests set it true on non-Linux to exercise the Linux
// ARP command path without relying on the CI OS matrix.
var arpPreferLinux = (runtime.GOOS == "linux")

// isHostReachableFn is the TCP probe used to validate ARP cache candidates;
// tests may replace it to exercise the ARP success path without real dials.
var isHostReachableFn = isHostReachable

// findIPByMACInARPCacheFn is the ARP-table lookup used by StepDiscoverIP;
// tests may replace it to exercise the ARP success path without a real cache hit.
var findIPByMACInARPCacheFn = findIPByMACInARPCache

// parseMACForARPCache parses a MAC token from an ARP line; tests may replace it
// to exercise the parse-error skip branch in findIPByMACInARPCache.
var parseMACForARPCache = func(candidate string) (net.HardwareAddr, error) {
	return net.ParseMAC(padMAC(candidate))
}

// StepDiscoverIP discovers the installed VM's IP address by:
//  1. Polling the Sylve DHCP lease API (works when Sylve's DHCP server is
//     configured on the switch, i.e. the switch bridge has an IP address).
//  2. Parsing the local system ARP cache via "arp -an" (macOS/BSD) or
//     "ip neigh show" (Linux) — this works when the VM is on the same physical
//     LAN segment as the Packer host (the switch bridges to a physical port)
//     and the VM has sent a gratuitous ARP on boot.
//
// Both methods are tried on every poll cycle; whichever returns an IP first
// wins. The discovered IP is stored under "instance_ip" for StepConnect.
type StepDiscoverIP struct {
	Config *Config
}

func (s *StepDiscoverIP) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	mac, _ := state.Get("vm_mac").(string)
	if mac == "" {
		err := fmt.Errorf("vm_mac not set in state bag; cannot discover IP")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	mac = strings.ToLower(mac)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	ui.Say(fmt.Sprintf("Waiting for IP of MAC %s (Sylve DHCP + local ARP cache)...", mac))

	timeout := discoverIPTotalTimeout
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(discoverIPPollInterval):
		}

		if time.Now().After(deadline) {
			err := fmt.Errorf("no IP found for MAC %s after %s (tried Sylve DHCP and local ARP cache)", mac, timeout)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		// Method 1: Sylve DHCP lease API.
		lease, err := c.FindLeaseByMAC(mac)
		if err != nil {
			ui.Say(fmt.Sprintf("Sylve DHCP poll error: %s", err))
		} else if lease != nil {
			ui.Say(fmt.Sprintf("Discovered IP %s for MAC %s via Sylve DHCP", lease.IP, mac))
			state.Put("instance_ip", lease.IP)
			return multistep.ActionContinue
		}

		// Method 2: Local system ARP cache.
		// When the VM's switch is bridged to a physical port (private=false),
		// the VM's ARP traffic propagates to the LAN.  The Packer host receives
		// the VM's gratuitous ARP on boot and caches the MAC->IP mapping.  This
		// works without any special privileges or network access to Sylve.
		//
		// The ARP cache may contain stale entries from a previous build phase
		// (e.g. the installer VM had a different IP than the installed OS after
		// reboot).  We collect all IPs for the MAC and probe each one to confirm
		// the host is actually reachable before accepting it.
		if ips, arpErr := findIPByMACInARPCacheFn(mac); arpErr != nil {
			ui.Say(fmt.Sprintf("ARP cache lookup error: %s", arpErr))
		} else {
			for _, ip := range ips {
				if isHostReachableFn(ip) {
					ui.Say(fmt.Sprintf("Discovered IP %s for MAC %s via local ARP cache", ip, mac))
					state.Put("instance_ip", ip)
					return multistep.ActionContinue
				}
			}
		}
	}
}

func (s *StepDiscoverIP) Cleanup(_ multistep.StateBag) {}

// ipRE matches a bare IPv4 address anywhere in a line.
var ipRE = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)

// macRE matches a MAC address with 1 or 2 hex digits per octet, covering both
// the canonical form "22:16:09:02:65:69" and the macOS compact form
// "22:16:9:2:65:69".
var macRE = regexp.MustCompile(`([0-9a-fA-F]{1,2}(?::[0-9a-fA-F]{1,2}){5})`)

// padMAC zero-pads each octet in a colon-separated MAC string to two hex
// digits so that net.ParseMAC (which requires exactly 2 digits per octet) can
// parse compact macOS ARP output such as "22:16:9:2:65:69".
func padMAC(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return s
	}
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}

// isHostReachable dials ip:22 with a short timeout and returns true if the
// host is up.  Both a successful connection and "connection refused" are
// treated as reachable (host up, sshd not yet listening).  Timeouts and
// "host is down" / "no route to host" errors are treated as unreachable,
// which filters out stale ARP entries from previous build phases.
func isHostReachable(ip string) bool {
	conn, err := net.DialTimeout("tcp", ip+":22", 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	return strings.Contains(err.Error(), "connection refused")
}

// findIPByMACInARPCache reads the OS ARP neighbour table and returns all IPv4
// addresses associated with mac.  It shells out to:
//   - "arp -an"         on macOS / BSD
//   - "ip neigh show"   on Linux
//
// Neither command requires elevated privileges.  MAC comparison is done by
// parsing both addresses with net.ParseMAC after zero-padding single-digit
// octets, so "22:16:09:02:65:69" and the macOS compact "22:16:9:2:65:69"
// match correctly.  Multiple IPs may be returned when the ARP cache contains
// both a stale entry from an earlier build phase and a fresh entry.
func findIPByMACInARPCache(mac string) ([]string, error) {
	target, err := net.ParseMAC(padMAC(mac))
	if err != nil {
		return nil, fmt.Errorf("parse target MAC %q: %w", mac, err)
	}

	var out []byte
	if arpPreferLinux {
		out, err = execARPCommandLinux()
	} else {
		// macOS, FreeBSD, OpenBSD, NetBSD
		out, err = execARPCommandBSD()
	}
	if err != nil {
		return nil, fmt.Errorf("ARP cache read: %w", err)
	}

	var matches []string
	for _, line := range strings.Split(string(out), "\n") {
		// Extract every MAC-like token from this line and compare bytes.
		for _, candidate := range macRE.FindAllString(line, -1) {
			hw, parseErr := parseMACForARPCache(candidate)
			if parseErr != nil {
				continue
			}
			if hw.String() != target.String() {
				continue
			}
			// MAC matched — extract the first IPv4 address from the same line.
			if m := ipRE.FindStringSubmatch(line); m != nil {
				matches = append(matches, m[1])
			}
		}
	}
	return matches, nil
}
