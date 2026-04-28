// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// discoverIPTotalTimeout and discoverIPPollInterval are tunable in tests.
var (
	discoverIPTotalTimeout = 60 * time.Second
	discoverIPPollInterval = 10 * time.Second
)

// StepDiscoverIP discovers the installed VM's IP address by polling the Sylve
// DHCP lease API until the guest's MAC address appears. The discovered IP is
// stored under "instance_ip" for StepConnect.
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

	ui.Say(fmt.Sprintf("Waiting for IP of MAC %s via Sylve DHCP...", mac))

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
			err := fmt.Errorf("no IP found for MAC %s after %s (Sylve DHCP lease not seen)", mac, timeout)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		// Poll the Sylve DHCP lease API.
		lease, err := c.FindLeaseByMAC(mac)
		if err != nil {
			ui.Say(fmt.Sprintf("Sylve DHCP poll error: %s", err))
		} else if lease != nil {
			ui.Say(fmt.Sprintf("Discovered IP %s for MAC %s via Sylve DHCP", lease.IP, mac))
			state.Put("instance_ip", lease.IP)
			return multistep.ActionContinue
		}
	}
}

func (s *StepDiscoverIP) Cleanup(_ multistep.StateBag) {}
