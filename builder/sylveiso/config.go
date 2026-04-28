// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package sylveiso

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"golang.org/x/crypto/ssh"
)

// Config is the configuration for the sylve-iso builder.
type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	commonsteps.HTTPConfig `mapstructure:",squash"`
	communicator.Config    `mapstructure:",squash"`

	// SylveURL is the base URL of the Sylve instance.
	// Defaults to "https://localhost:8181".
	SylveURL string `mapstructure:"sylve_url"`

	// SylveToken is a pre-issued Bearer token for the Sylve API.
	// Supply this OR (SylveUser + SylvePassword). Falls back to the SYLVE_TOKEN
	// environment variable. When absent, the builder performs a login at the
	// start of Run() and a logout when it finishes.
	SylveToken string `mapstructure:"sylve_token"`

	// SylveUser is the Sylve account username used for login-based auth.
	// Falls back to the SYLVE_USER environment variable.
	// Required when sylve_token is not set.
	SylveUser string `mapstructure:"sylve_user"`

	// SylvePassword is the Sylve account password used for login-based auth.
	// Falls back to the SYLVE_PASSWORD environment variable.
	// Required when sylve_token is not set.
	SylvePassword string `mapstructure:"sylve_password"`

	// SylveAuthType is the Sylve authentication type sent in the login request.
	// Valid values: "sylve" (native DB account, default) or "pam" (PAM auth).
	// Falls back to the SYLVE_AUTH_TYPE environment variable.
	SylveAuthType string `mapstructure:"sylve_auth_type"`

	// SylveAPILoginTimeout is how long to keep retrying password login when the
	// Sylve API is unreachable (e.g. service still starting). HCL duration, e.g.
	// "2m", "5m". Ignored when sylve_token is set. Defaults to 2m. Override with
	// SYLVE_API_LOGIN_TIMEOUT when unset in HCL.
	SylveAPILoginTimeout string `mapstructure:"sylve_api_login_timeout"`

	// sylveAPILoginTimeoutDur is set in Prepare from SylveAPILoginTimeout.
	sylveAPILoginTimeoutDur time.Duration

	// TLSSkipVerify disables TLS certificate verification.
	// Defaults to true because Sylve ships a self-signed certificate.
	TLSSkipVerify bool `mapstructure:"tls_skip_verify"`

	// ISODownloadURL is a URL passed to the Sylve download manager.
	// The plugin triggers the download and polls until status == "done".
	// Required.
	ISODownloadURL string `mapstructure:"iso_download_url"`

	// VMName is the name prefix for the temporary VM.
	// A UUID suffix is appended automatically to avoid collisions.
	VMName string `mapstructure:"vm_name"`

	// CPUCores is the number of vCPU cores per socket. Defaults to 2.
	CPUCores int `mapstructure:"cpu_cores"`

	// CPUSockets is the number of vCPU sockets. Defaults to 1.
	CPUSockets int `mapstructure:"cpu_sockets"`

	// CPUThreads is the number of threads per core. Defaults to 1.
	CPUThreads int `mapstructure:"cpu_threads"`

	// RAM is the amount of memory in MiB. Defaults to 1024.
	RAM int `mapstructure:"ram"`

	// StoragePool is the ZFS pool name on the Sylve host.
	// Falls back to the SYLVE_POOL environment variable. When still empty the
	// plugin picks the first pool from Sylve's basic settings.
	StoragePool string `mapstructure:"storage_pool"`

	// StorageSizeMB is the install disk size in MiB. Defaults to 65536.
	StorageSizeMB int `mapstructure:"storage_size_mb"`

	// StorageType is the zvol type. Defaults to "zvol".
	StorageType string `mapstructure:"storage_type"`

	// StorageEmulationType is the disk emulation type for the install disk.
	// Defaults to "virtio-blk".
	StorageEmulationType string `mapstructure:"storage_emulation_type"`

	// SwitchName is the name of a DHCP-enabled Sylve virtual switch.
	// Falls back to the SYLVE_SWITCH environment variable.
	SwitchName string `mapstructure:"switch_name"`

	// SwitchEmulationType is the NIC emulation type. Defaults to "e1000".
	SwitchEmulationType string `mapstructure:"switch_emulation_type"`

	// VNCPort is the VNC port resolved from the [VNCPortMin, VNCPortMax] range.
	// It is set automatically during Prepare and must not be set manually.
	VNCPort int `mapstructure:"vnc_port"`

	// VNCPortMin is the lower bound of the VNC port range (inclusive).
	// Defaults to 5900. Must be >= 5900.
	VNCPortMin int `mapstructure:"vnc_port_min"`

	// VNCPortMax is the upper bound of the VNC port range (inclusive).
	// Defaults to 6000.
	VNCPortMax int `mapstructure:"vnc_port_max"`

	// VNCHost is the hostname or IP to connect for VNC. Defaults to the
	// host portion of sylve_url so remote Packer runs (e.g. from macOS) reach
	// the correct Sylve server without extra configuration.
	VNCHost string `mapstructure:"vnc_host"`

	// VNCPassword is an optional VNC password. Sylve's vncPassword field is
	// optional (no binding:"required" in the Sylve API), so this may be left empty.
	VNCPassword string `mapstructure:"vnc_password"`

	// VNCResolution is the VNC display resolution passed to Sylve on VM creation.
	// Defaults to "1024x768".
	VNCResolution string `mapstructure:"vnc_resolution"`

	// Loader is the VM firmware loader. Defaults to "uefi". Set to "bios" for
	// legacy boot.
	Loader string `mapstructure:"loader"`

	// TimeOffset is the guest clock offset. "utc" (default) or "localtime".
	TimeOffset string `mapstructure:"time_offset"`

	// ACPI enables ACPI support in the VM. Defaults to true.
	ACPI bool `mapstructure:"acpi"`

	// APIC enables APIC support in the VM. Defaults to true.
	APIC bool `mapstructure:"apic"`

	// BootWait is the duration to wait before sending VNC boot commands.
	// Defaults to "10s".
	BootWait string `mapstructure:"boot_wait"`

	// BootKeyInterval is the delay between each key group in boot commands.
	// Defaults to 100ms.
	BootKeyInterval time.Duration `mapstructure:"boot_key_interval"`

	// BootCommand is the list of VNC keyboard sequences to type at boot.
	BootCommand []string `mapstructure:"boot_command"`

	// InstallWaitTimeout removed: after boot_command completes the VM is
	// already running; the SSH communicator polls until SSH is reachable.

	// ShutdownCommand is the command sent over SSH to shut down the provisioned VM.
	// Defaults to "/sbin/poweroff".
	ShutdownCommand string `mapstructure:"shutdown_command"`

	// RestartAfterInstall enables the StepRestartAfterInstall phase.
	// Defaults to false. When true, the plugin waits for the installer VM to stop
	// (guest-initiated reboot causes Bhyve to exit), disables the ISO storage,
	// and restarts the VM so it boots into the freshly installed OS.
	// Set to true for OS images whose installer performs a self-reboot before SSH
	// becomes available (e.g. Alpine, Debian, FreeBSD, OpenBSD unattended installs).
	RestartAfterInstall bool `mapstructure:"restart_after_install"`

	// Destroy controls whether the VM is deleted from Sylve after a successful
	// build. Defaults to false (keep the VM). Set to true to delete the VM when
	// the build succeeds. On failure, deletion follows keep_on_error; on
	// cancellation the VM is deleted regardless of destroy.
	Destroy bool `mapstructure:"destroy"`

	// KeepOnError controls whether the VM is kept alive when the build fails.
	// Defaults to false (VM is deleted on error). Set to true to keep the VM
	// running for post-failure debugging (equivalent to -on-error=abort).
	KeepOnError bool `mapstructure:"keep_on_error"`

	ctx interpolate.Context
}

// bootWaitDuration parses BootWait as a time.Duration.
func (c *Config) bootWaitDuration() (time.Duration, error) {
	if c.BootWait == "" {
		return 10 * time.Second, nil
	}
	return time.ParseDuration(c.BootWait)
}

// Prepare validates and normalises the configuration.
// No side effects — no API calls, no file creation.
func (c *Config) Prepare(raws ...interface{}) ([]string, []string, error) {
	err := config.Decode(c, &config.DecodeOpts{
		PluginType:         "packer.builder.sylve-iso",
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{"boot_command"},
		},
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	var errs *packersdk.MultiError

	// Apply defaults.
	if c.SylveURL == "" {
		// SYLVE_HOST lets callers specify just the hostname (e.g. from an env
		// secret or CI variable) without constructing a full URL.
		if host := os.Getenv("SYLVE_HOST"); host != "" {
			c.SylveURL = "https://" + host + ":8181"
		} else {
			c.SylveURL = "https://localhost:8181"
		}
	}
	if c.SylveToken == "" {
		c.SylveToken = os.Getenv("SYLVE_TOKEN")
	}
	if c.SylveUser == "" {
		c.SylveUser = os.Getenv("SYLVE_USER")
	}
	if c.SylvePassword == "" {
		c.SylvePassword = os.Getenv("SYLVE_PASSWORD")
	}
	if c.SylveAuthType == "" {
		c.SylveAuthType = os.Getenv("SYLVE_AUTH_TYPE")
	}
	if c.SylveAuthType == "" {
		c.SylveAuthType = "sylve"
	}
	if c.SwitchName == "" {
		c.SwitchName = os.Getenv("SYLVE_SWITCH")
	}
	if c.StoragePool == "" {
		c.StoragePool = os.Getenv("SYLVE_POOL")
	}
	if c.SylveAPILoginTimeout == "" {
		if env := os.Getenv("SYLVE_API_LOGIN_TIMEOUT"); env != "" {
			c.SylveAPILoginTimeout = env
		}
	}
	loginWait := 2 * time.Minute
	if c.SylveAPILoginTimeout != "" {
		d, err := time.ParseDuration(c.SylveAPILoginTimeout)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs, fmt.Errorf("invalid sylve_api_login_timeout: %w", err))
		} else {
			loginWait = d
			if loginWait < 0 {
				errs = packersdk.MultiErrorAppend(errs, errors.New("sylve_api_login_timeout must be >= 0"))
			}
		}
	}
	c.sylveAPILoginTimeoutDur = loginWait
	// TLSSkipVerify defaults to true because Sylve ships a self-signed cert.
	// This cannot be expressed as a zero-value default (bool zero = false).
	// We always set it here; users must explicitly set tls_skip_verify = false
	// if they configure Sylve with a CA-signed certificate.
	// [SECURITY DESIGN] intentional: default is insecure-skip-verify so out-of-box
	// Sylve installations work without manual certificate trust configuration.
	if !c.TLSSkipVerify {
		c.TLSSkipVerify = true
	}
	if c.CPUCores == 0 {
		c.CPUCores = 2
	}
	if c.CPUSockets == 0 {
		c.CPUSockets = 1
	}
	if c.CPUThreads == 0 {
		c.CPUThreads = 1
	}
	if c.RAM == 0 {
		c.RAM = 1024
	}
	if c.VMName == "" {
		c.VMName = "packer-{{uuid}}"
	}
	if c.StorageSizeMB == 0 {
		c.StorageSizeMB = 65536
	}
	if c.StorageType == "" {
		c.StorageType = "zvol"
	}
	if c.StorageEmulationType == "" {
		c.StorageEmulationType = "virtio-blk"
	}
	if c.SwitchEmulationType == "" {
		c.SwitchEmulationType = "e1000"
	}
	if c.VNCPortMin == 0 {
		c.VNCPortMin = 5900
	}
	if c.VNCPortMax == 0 {
		c.VNCPortMax = 5999
	}
	if c.VNCHost == "" {
		// Default to the hostname from SylveURL so Packer running remotely
		// (e.g. from macOS) connects VNC to the Sylve host automatically.
		if u, err := url.Parse(c.SylveURL); err == nil && u.Hostname() != "" {
			c.VNCHost = u.Hostname()
		} else {
			c.VNCHost = "127.0.0.1"
		}
	}
	if c.VNCResolution == "" {
		c.VNCResolution = "1024x768"
	}
	if c.Loader == "" {
		c.Loader = "uefi"
	}
	if c.TimeOffset == "" {
		c.TimeOffset = "utc"
	}
	if c.BootKeyInterval == 0 {
		// 200 ms gives enough margin for the WebSocket→Sylve proxy round-trip.
		// At 100 ms the write mutex for FramebufferUpdateRequests competes with
		// the key-down/key-up gap and causes dropped keystrokes.
		c.BootKeyInterval = 200 * time.Millisecond
	}
	// Always enable ACPI and APIC; Bhyve requires both for correct guest
	// power management and interrupt routing.
	c.ACPI = true
	c.APIC = true
	if c.ShutdownCommand == "" {
		c.ShutdownCommand = "/sbin/poweroff"
	}
	// Destroy defaults to false (safe default: keep the VM). Set destroy = true
	// in HCL to delete the VM after a successful build. On failure, keep_on_error
	// controls whether the VM is kept; cancellation still triggers deletion.

	// Validate auth: require either a pre-issued token OR user+password.
	if c.SylveToken == "" && (c.SylveUser == "" || c.SylvePassword == "") {
		errs = packersdk.MultiErrorAppend(errs, errors.New(
			"authentication required: provide sylve_token (or SYLVE_TOKEN) "+
				"OR both sylve_user (SYLVE_USER) and sylve_password (SYLVE_PASSWORD)"))
	}
	if c.ISODownloadURL == "" {
		errs = packersdk.MultiErrorAppend(errs, errors.New("iso_download_url is required"))
	}
	if c.VNCPortMin < 5900 {
		errs = packersdk.MultiErrorAppend(errs, errors.New("vnc_port_min must be >= 5900"))
	}
	if c.VNCPortMax < c.VNCPortMin {
		errs = packersdk.MultiErrorAppend(errs, errors.New("vnc_port_max must be >= vnc_port_min"))
	}
	if _, err := c.bootWaitDuration(); err != nil {
		errs = packersdk.MultiErrorAppend(errs, fmt.Errorf("invalid boot_wait: %w", err))
	}
	// Prepare HTTP config (serves http_directory to the guest).
	if httpErrs := c.HTTPConfig.Prepare(&c.ctx); len(httpErrs) > 0 {
		errs = packersdk.MultiErrorAppend(errs, httpErrs...)
	}

	// Auto-bastion: when no explicit ssh_bastion_host is set, route SSH through
	// the Sylve host so Packer does not need a direct route to the VM subnet.
	// Skipped when Packer is already running on the Sylve host (VM subnet is
	// directly reachable via the bridge interface).
	// Auth resolution order:
	//   explicit HCL ssh_bastion_* fields
	//   > SYLVE_SSH_PROXY_KEY env
	//   > ~/.ssh/config IdentityFile for the host
	//   > default key paths (~/.ssh/id_ed25519, id_ecdsa_sk, id_ecdsa, id_dsa, id_rsa)
	//   > SSH agent (SSHBastionAgentAuth = true)
	if c.Config.SSHBastionHost == "" {
		u, parseErr := url.Parse(c.SylveURL)
		if parseErr == nil && u.Hostname() != "" && !sylveHostIsLocal(u.Hostname()) {
			c.Config.SSHBastionHost = u.Hostname()
			log.Printf("[DEBUG] sylve ssh-proxy: bastion host=%s (Sylve URL is remote)", c.Config.SSHBastionHost)

			// Resolve username: explicit HCL > ~/.ssh/config User > $USER.
			// SylveUser is the Sylve API account and is intentionally NOT used
			// here — the SSH system user on the Sylve host is typically different
			// from the Sylve web application user.
			if c.Config.SSHBastionUsername == "" {
				sshUser, sshKeyFile, sshProxyJump := sshConfigForHost(u.Hostname())
				log.Printf("[DEBUG] sylve ssh-proxy: ~/.ssh/config lookup for %s: user=%q identityFile=%q proxyJump=%q",
					u.Hostname(), sshUser, sshKeyFile, sshProxyJump)
				if sshProxyJump != "" && strings.ToLower(sshProxyJump) != "none" {
					log.Printf("[WARN] sylve ssh-proxy: ~/.ssh/config specifies ProxyJump=%q for host %s, "+
						"but the plugin's built-in SSH bastion supports only one hop. "+
						"The ProxyJump directive is ignored. "+
						"If you need multi-hop access, establish a local port-forward first "+
						"(e.g. ssh -fNL 2222:%s:22 %s) and set SYLVE_HOST=localhost with "+
						"explicit ssh_bastion_host/ssh_bastion_port in HCL.",
						sshProxyJump, u.Hostname(), u.Hostname(), sshProxyJump)
				}
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

				// Resolve auth: explicit HCL fields already set? Leave them.
				// Priority: SYLVE_SSH_PROXY_KEY env > ~/.ssh/config IdentityFile
				// > OpenSSH default key paths > SSH agent. SylvePassword is
				// intentionally excluded — it is the Sylve API password and is
				// unrelated to the SSH system account on the Sylve host.
				if c.Config.SSHBastionPassword == "" && c.Config.SSHBastionPrivateKeyFile == "" {
					envKey := os.Getenv("SYLVE_SSH_PROXY_KEY")
					switch {
					case envKey != "":
						c.Config.SSHBastionPrivateKeyFile = envKey
						log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=key (from SYLVE_SSH_PROXY_KEY)")
					case sshKeyFile != "":
						c.Config.SSHBastionPrivateKeyFile = sshKeyFile
						log.Printf("[DEBUG] sylve ssh-proxy: bastion auth=key %q (from ~/.ssh/config)", sshKeyFile)
					default:
						// No explicit key configured. Mirror OpenSSH behaviour: probe
						// default key paths before falling back to agent auth.
						if home, homeErr := os.UserHomeDir(); homeErr == nil {
							for _, name := range []string{"id_ed25519", "id_ecdsa_sk", "id_ecdsa", "id_dsa", "id_rsa"} {
								keyPath := filepath.Join(home, ".ssh", name)
								if _, statErr := os.Stat(keyPath); statErr == nil {
									// Verify the key can be parsed without a passphrase.
									// Skip FIDO2/hardware keys and passphrase-protected keys
									// that require interactive input crypto/ssh cannot provide.
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
		} else if parseErr == nil && u.Hostname() != "" {
			log.Printf("[DEBUG] sylve ssh-proxy: skipping bastion — Sylve host %s is local", u.Hostname())
		}
	}

	// Prepare communicator config (SSH fields).
	if commErrs := c.Config.Prepare(&c.ctx); len(commErrs) > 0 {
		errs = packersdk.MultiErrorAppend(errs, commErrs...)
	}

	if errs != nil && len(errs.Errors) > 0 {
		return nil, nil, errs
	}

	return nil, nil, nil
}

// sylveHostIsLocal reports whether hostname resolves to an IP address that is
// assigned to a local network interface. When true, Packer is running on the
// same machine as Sylve and can reach the VM subnet directly — no SSH bastion
// is needed.
func sylveHostIsLocal(hostname string) bool {
	// Loopback / literal localhost always means local.
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}

	// Collect all local interface addresses.
	addrs, err := net.InterfaceAddrs()
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

	// Resolve the hostname and check against local IPs.
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
	home, err := os.UserHomeDir()
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
		// Unquote simple double-quoted values.
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
		// Stop once all three values are found.
		if user != "" && identityFile != "" && proxyJump != "" {
			return user, identityFile, proxyJump
		}
	}
	return user, identityFile, proxyJump
}
