// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

// ssh_bastion.go provides the auto-bastion helpers used by Prepare() in
// config.go. The logic is identical to the implementation in the sylveiso
// package: when Packer runs on a machine that is not the Sylve host, guest
// VMs are only reachable via the Sylve host's internal bridge. These helpers
// detect that case and configure the SDK communicator's SSH bastion fields so
// no manual ssh_bastion_* configuration is required.

import (
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// interfaceAddrsFn and userHomeDirFn are overridable in tests.
var (
	interfaceAddrsFn = net.InterfaceAddrs
	userHomeDirFn    = os.UserHomeDir
)

// sylveHostIsLocal reports whether hostname resolves to an IP address assigned
// to a local network interface. When true, Packer is running on the same
// machine as Sylve and can reach the VM subnet directly — no SSH bastion is
// needed.
func sylveHostIsLocal(hostname string) bool {
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}

	addrs, err := interfaceAddrsFn()
	if err != nil {
		return false
	}
	localIPs := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		switch v := a.(type) {
		case *net.IPNet:
			localIPs[v.IP.String()] = struct{}{}
		case *net.IPAddr:
			localIPs[v.IP.String()] = struct{}{}
		}
	}

	resolved, err := net.LookupHost(hostname)
	if err != nil {
		return false
	}
	for _, ip := range resolved {
		if _, ok := localIPs[ip]; ok {
			return true
		}
	}
	return false
}

// sshConfigForHost parses ~/.ssh/config and returns the User, first
// IdentityFile, and ProxyJump configured for hostname. Returns empty strings
// when the file cannot be read or no matching Host block is found.
// Only the most common directives (Host, User, IdentityFile, ProxyJump) are
// read; the parser handles * and ? wildcards in Host patterns.
// The ProxyJump value is returned for diagnostic purposes only — the plugin's
// built-in bastion supports a single hop and cannot honour ProxyJump chains.
func sshConfigForHost(hostname string) (user, identityFile, proxyJump string) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", "", ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return "", "", ""
	}

	var inBlock bool
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := strings.Join(fields[1:], " ")
		value = strings.Trim(value, "\"")

		if key == "host" {
			inBlock = false
			for _, pattern := range fields[1:] {
				matched, matchErr := path.Match(strings.ToLower(pattern), strings.ToLower(hostname))
				if matchErr == nil && matched {
					inBlock = true
					break
				}
			}
			continue
		}
		if !inBlock {
			continue
		}
		switch key {
		case "user":
			if user == "" {
				user = value
			}
		case "identityfile":
			if identityFile == "" {
				if strings.HasPrefix(value, "~/") {
					value = filepath.Join(home, value[2:])
				}
				identityFile = value
			}
		case "proxyjump":
			if proxyJump == "" {
				proxyJump = value
			}
		}
		if user != "" && identityFile != "" && proxyJump != "" {
			return user, identityFile, proxyJump
		}
	}
	return user, identityFile, proxyJump
}

// applyAutoBastion sets SSHBastionHost and its auth fields on c when the Sylve
// host is remote and no explicit ssh_bastion_host has been provided in HCL.
// Auth resolution order:
//
//	explicit HCL ssh_bastion_* fields
//	> SYLVE_SSH_PROXY_KEY env var
//	> ~/.ssh/config IdentityFile for the host
//	> default key paths (~/.ssh/id_ed25519, id_ecdsa_sk, id_ecdsa, id_dsa, id_rsa)
//	> SSH agent (SSHBastionAgentAuth = true)
func applyAutoBastion(c *Config, sylveHostname string) {
	c.Config.SSHBastionHost = sylveHostname
	log.Printf("[DEBUG] sylve ssh-proxy: bastion host=%s (Sylve URL is remote)", sylveHostname)

	// Resolve ~/.ssh/config unconditionally so both username and key resolution
	// can use it regardless of which fields are already set.
	sshUser, sshKeyFile, sshProxyJump := sshConfigForHost(sylveHostname)
	log.Printf("[DEBUG] sylve ssh-proxy: ~/.ssh/config lookup for %s: user=%q identityFile=%q proxyJump=%q",
		sylveHostname, sshUser, sshKeyFile, sshProxyJump)
	if sshProxyJump != "" && strings.ToLower(sshProxyJump) != "none" {
		log.Printf("[WARN] sylve ssh-proxy: ~/.ssh/config specifies ProxyJump=%q for host %s, "+
			"but the plugin's built-in SSH bastion supports only one hop. "+
			"The ProxyJump directive is ignored. "+
			"If you need multi-hop access, establish a local port-forward first "+
			"(e.g. ssh -fNL 2222:%s:22 %s) and set SYLVE_HOST=localhost with "+
			"explicit ssh_bastion_host/ssh_bastion_port in HCL.",
			sshProxyJump, sylveHostname, sylveHostname, sshProxyJump)
	}

	// Resolve username only when not already provided via HCL.
	if c.Config.SSHBastionUsername == "" {
		switch {
		case sshUser != "":
			c.Config.SSHBastionUsername = sshUser
			log.Printf("[DEBUG] sylve ssh-proxy: bastion username=%q (from ~/.ssh/config)", c.Config.SSHBastionUsername)
		default:
			if u, ok := os.LookupEnv("USER"); ok && u != "" {
				c.Config.SSHBastionUsername = u
			}
			log.Printf("[DEBUG] sylve ssh-proxy: bastion username=%q (from $USER)", c.Config.SSHBastionUsername)
		}
	}

	// Resolve auth independently of username so that an explicit
	// ssh_bastion_username in HCL does not suppress key/agent discovery.
	if c.Config.SSHBastionPassword == "" && c.Config.SSHBastionPrivateKeyFile == "" && !c.Config.SSHBastionAgentAuth {
		envKey := os.Getenv("SYLVE_SSH_PROXY_KEY")
		switch {
		case envKey != "":
			c.Config.SSHBastionPrivateKeyFile = envKey
			log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=key (from SYLVE_SSH_PROXY_KEY)")
		case sshKeyFile != "":
			c.Config.SSHBastionPrivateKeyFile = sshKeyFile
			log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=key %q (from ~/.ssh/config)", sshKeyFile)
		default:
			if home, homeErr := os.UserHomeDir(); homeErr == nil {
				for _, name := range []string{"id_ed25519", "id_ecdsa_sk", "id_ecdsa", "id_dsa", "id_rsa"} {
					keyPath := filepath.Join(home, ".ssh", name)
					if _, statErr := os.Stat(keyPath); statErr == nil {
						keyBytes, readErr := os.ReadFile(keyPath)
						if readErr != nil {
							log.Printf("[DEBUG] sylve ssh-proxy: skipping %q: read error: %v", keyPath, readErr)
							continue
						}
						if _, parseErr := ssh.ParsePrivateKey(keyBytes); parseErr != nil {
							log.Printf("[DEBUG] sylve ssh-proxy: skipping %q: not usable without passphrase (%v)", keyPath, parseErr)
							continue
						}
						c.Config.SSHBastionPrivateKeyFile = keyPath
						log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=key %q (default key)", keyPath)
						break
					}
				}
			}
			if c.Config.SSHBastionPrivateKeyFile == "" {
				c.Config.SSHBastionAgentAuth = true
				log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=agent")
			}
		}
	}
}

// resolveBastionSSHParams returns the SSH username and authentication
// parameters for tunnelling through sylveHostname. It uses the same
// resolution order as applyAutoBastion so that the WinRM tunnel and the SSH
// auto-bastion authenticate with consistent credentials.
//
// Auth resolution order:
//
//	SYLVE_SSH_PROXY_KEY env var
//	> ~/.ssh/config IdentityFile for the host
//	> default key paths (~/.ssh/id_ed25519, id_ecdsa_sk, id_ecdsa, id_dsa, id_rsa)
//	> SSH agent (agentAuth = true)
func resolveBastionSSHParams(sylveHostname string) (user, keyFile string, agentAuth bool) {
	sshUser, sshKeyFile, sshProxyJump := sshConfigForHost(sylveHostname)
	if sshProxyJump != "" && strings.ToLower(sshProxyJump) != "none" {
		log.Printf("[WARN] sylve ssh-proxy: ProxyJump=%q for host %s is ignored by the WinRM tunnel (single-hop only)",
			sshProxyJump, sylveHostname)
	}

	// Username: prefer ~/.ssh/config User, fall back to $USER.
	switch {
	case sshUser != "":
		user = sshUser
	default:
		user, _ = os.LookupEnv("USER")
	}

	// Auth: SYLVE_SSH_PROXY_KEY > ~/.ssh/config IdentityFile > default keys > agent.
	envKey := os.Getenv("SYLVE_SSH_PROXY_KEY")
	switch {
	case envKey != "":
		keyFile = envKey
		log.Printf("[DEBUG] sylve ssh-proxy: tunnel auth=key (from SYLVE_SSH_PROXY_KEY)")
	case sshKeyFile != "":
		keyFile = sshKeyFile
		log.Printf("[DEBUG] sylve ssh-proxy: tunnel auth=key %q (from ~/.ssh/config)", keyFile)
	default:
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			for _, name := range []string{"id_ed25519", "id_ecdsa_sk", "id_ecdsa", "id_dsa", "id_rsa"} {
				keyPath := filepath.Join(home, ".ssh", name)
				if _, statErr := os.Stat(keyPath); statErr != nil {
					continue
				}
				keyBytes, readErr := os.ReadFile(keyPath)
				if readErr != nil {
					continue
				}
				if _, parseErr := ssh.ParsePrivateKey(keyBytes); parseErr != nil {
					continue
				}
				keyFile = keyPath
				log.Printf("[DEBUG] sylve ssh-proxy: tunnel auth=key %q (default key)", keyFile)
				break
			}
		}
		if keyFile == "" {
			agentAuth = true
			log.Printf("[DEBUG] sylve ssh-proxy: tunnel auth=agent")
		}
	}
	return
}

// bastionSSHDialAddrForTests, when non-empty, replaces sylveHostname+":22" when
// dialling the bastion SSH server. Tests set this alongside newTestSSHServer so
// the client reaches the ephemeral listener instead of tcp/22.
var bastionSSHDialAddrForTests string

// dialBastionSSH establishes an SSH client connection to sylveHostname:22
// using the provided authentication parameters. Called by StepWinRMTunnel to
// open the port-forward session.
func dialBastionSSH(sylveHostname, user, keyFile string, agentAuth bool) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod
	switch {
	case keyFile != "":
		keyBytes, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read SSH key %q: %w", keyFile, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse SSH key %q: %w", keyFile, err)
		}
		authMethods = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case agentAuth:
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, fmt.Errorf("SSH agent auth requested but SSH_AUTH_SOCK is not set")
		}
		agentConn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("connect to SSH agent: %w", err)
		}
		agentClient := agent.NewClient(agentConn)
		authMethods = []ssh.AuthMethod{ssh.PublicKeysCallback(agentClient.Signers)}
	default:
		return nil, fmt.Errorf("no SSH auth method available for WinRM tunnel" +
			" (set SYLVE_SSH_PROXY_KEY, configure SSH agent, or add an identity file to ~/.ssh/config)")
	}

	cfg := &ssh.ClientConfig{
		User: user,
		Auth: authMethods,
		// Accept the host key without verification — consistent with the SSH
		// auto-bastion which also does not pin host keys.
		// [SECURITY DESIGN] intentional: out-of-box Sylve installations use
		// self-signed certs and ephemeral host keys; requiring pinning would
		// break the zero-config use case.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // nosemgrep: avoid-ssh-insecure-ignore-host-key — see [SECURITY DESIGN] above; bastion dials the configured Sylve host without pinned keys
	}
	addr := sylveHostname + ":22"
	if bastionSSHDialAddrForTests != "" {
		addr = bastionSSHDialAddrForTests
	}
	return ssh.Dial("tcp", addr, cfg)
}
