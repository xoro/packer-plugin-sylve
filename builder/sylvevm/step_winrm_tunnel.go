// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

// step_winrm_tunnel.go — automatic WinRM-over-SSH tunnel.
//
// When communicator = "winrm" and the Sylve host is remote (not the same
// machine as the Packer host), guest VMs receive DHCP addresses on Sylve's
// internal bridge subnet that is not routable from outside the Sylve host.
// This step opens a single SSH connection to the Sylve host and starts a TCP
// port-forward from a random localhost port to the VM's WinRM port. It then
// overrides Config.WinRMHost and Config.WinRMPort so that the SDK
// communicator.StepConnect uses the tunnel transparently — no manual
// port-forward or ssh_bastion configuration is required.
//
// SSH auth uses the same resolution order as the SSH auto-bastion (see
// ssh_bastion.go and applyAutoBastion).
//
// The step is a no-op when:
//   - communicator is not "winrm"
//   - winrm_host is already set in HCL (user manages routing explicitly)
//   - The Sylve host resolves to a local interface (Packer runs on the Sylve host)

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	gossh "golang.org/x/crypto/ssh"
)

// sylveHostIsLocalFn and dialBastionSSHFn are package-level function
// variables so that tests can substitute local implementations without
// affecting the production code path.
var (
	sylveHostIsLocalFn  = sylveHostIsLocal
	dialBastionSSHFn    = dialBastionSSH
	winRMTunnelListenFn = func(network, address string) (net.Listener, error) {
		return net.Listen(network, address)
	}
)

// StepWinRMTunnel creates an SSH port-forward from a random localhost port to
// the VM's WinRM port through the Sylve host. Cleanup closes the listener and
// the underlying SSH connection.
type StepWinRMTunnel struct {
	Config   *Config
	listener net.Listener
	sshConn  *gossh.Client
}

// Run establishes the WinRM tunnel when conditions are met (see package doc).
func (s *StepWinRMTunnel) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	// Only activate for WinRM communicator.
	if s.Config.Config.Type != "winrm" {
		return multistep.ActionContinue
	}

	// Respect an explicit winrm_host override in HCL — the user handles routing.
	if s.Config.Config.WinRMHost != "" {
		return multistep.ActionContinue
	}

	vmIP, ok := state.Get("instance_ip").(string)
	if !ok || vmIP == "" {
		state.Put("error", fmt.Errorf("winrm tunnel: instance_ip not set in state bag"))
		return multistep.ActionHalt
	}

	u, parseErr := url.Parse(s.Config.SylveURL)
	if parseErr != nil || u.Hostname() == "" {
		return multistep.ActionContinue
	}
	sylveHostname := u.Hostname()

	if sylveHostIsLocalFn(sylveHostname) {
		// Packer runs on the Sylve host: the VM subnet is reachable directly.
		return multistep.ActionContinue
	}

	ui := state.Get("ui").(packersdk.Ui)
	ui.Say(fmt.Sprintf("Setting up WinRM tunnel through %s...", sylveHostname))

	tunnelUser, keyFile, agentAuth := resolveBastionSSHParams(sylveHostname)
	sshConn, err := dialBastionSSHFn(sylveHostname, tunnelUser, keyFile, agentAuth)
	if err != nil {
		state.Put("error", fmt.Errorf("winrm tunnel: %w", err))
		return multistep.ActionHalt
	}
	s.sshConn = sshConn

	ln, err := winRMTunnelListenFn("tcp", "127.0.0.1:0")
	if err != nil {
		_ = sshConn.Close()
		state.Put("error", fmt.Errorf("winrm tunnel: local listen: %w", err))
		return multistep.ActionHalt
	}
	s.listener = ln

	localPort := ln.Addr().(*net.TCPAddr).Port
	winrmPort := s.Config.Config.WinRMPort
	if winrmPort == 0 {
		winrmPort = 5985
	}
	remoteAddr := fmt.Sprintf("%s:%d", vmIP, winrmPort)
	go s.forwardConns(ln, sshConn, remoteAddr)

	ui.Say(fmt.Sprintf("WinRM tunnel active: 127.0.0.1:%d -> %s (via %s)", localPort, remoteAddr, sylveHostname))

	// Override the WinRM target so communicator.StepConnect connects through
	// the tunnel without needing a direct route to the VM's bridge subnet.
	// instance_ip must also be updated because communicator.StepConnect uses
	// the Host function (which reads instance_ip) to build the endpoint URL,
	// taking precedence over WinRMHost in the config.
	s.Config.Config.WinRMHost = "127.0.0.1"
	s.Config.Config.WinRMPort = localPort
	state.Put("instance_ip", "127.0.0.1")

	return multistep.ActionContinue
}

// forwardConns accepts connections on ln and forwards each one through a new
// TCP channel on sshConn to remoteAddr.
func (s *StepWinRMTunnel) forwardConns(ln net.Listener, sshConn *gossh.Client, remoteAddr string) {
	for {
		local, err := ln.Accept()
		if err != nil {
			// Listener closed by Cleanup — normal shutdown.
			return
		}
		go func(local net.Conn) {
			defer local.Close()
			remote, err := sshConn.Dial("tcp", remoteAddr)
			if err != nil {
				return
			}
			defer remote.Close()
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = io.Copy(remote, local) }()
			go func() { defer wg.Done(); _, _ = io.Copy(local, remote) }()
			wg.Wait()
		}(local)
	}
}

// Cleanup closes the port-forward listener and the underlying SSH connection.
func (s *StepWinRMTunnel) Cleanup(_ multistep.StateBag) {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.sshConn != nil {
		_ = s.sshConn.Close()
	}
}
