// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepUploadISO gives Sylve the ISO that commonsteps.StepDownload downloaded
// and checked against iso_checksum (state "iso_path"): it uploads it as
// "sha256-<hex>-<file name>" -- or reuses an upload of that name, which can
// only hold that content -- and puts the download's UUID into "iso_uuid" for
// StepCreateVM, as StepDownloadISO does. Used only when iso_checksum is set.
type StepUploadISO struct {
	Config *Config
}

// uploadPollInterval and uploadMaxWait bound the wait for Sylve to finish the
// completed upload; overridable in tests.
var (
	uploadPollInterval = 2 * time.Second
	uploadMaxWait      = 10 * time.Minute
)

// verifiedISOName is the name of the upload of the ISO at url with the sha256
// in checksum ("sha256:<hex>"): its content is in its name.
func verifiedISOName(checksum, url string) string {
	return "sha256-" + strings.TrimPrefix(checksum, "sha256:") + "-" + path.Base(url)
}

// Run uploads (or reuses) the verified ISO and waits until Sylve reports the
// download done. It halts the build when Sylve cannot be reached, the upload
// fails, the download fails, the wait times out or the build is cancelled.
func (s *StepUploadISO) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)
	name := verifiedISOName(s.Config.ISOChecksum, s.Config.ISODownloadURL)
	halt := func(err error) multistep.StepAction {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	dl, err := c.FindUploadedDownload(name)
	if err != nil {
		return halt(err)
	}
	if dl == nil {
		ui.Say(fmt.Sprintf("Uploading the verified ISO to Sylve as %q...", name))
		if _, err := c.UploadFile(ctx, state.Get("iso_path").(string), name); err != nil {
			return halt(err)
		}
	} else {
		ui.Say(fmt.Sprintf("Sylve holds the verified ISO already: %q", name))
	}

	deadline := time.Now().Add(uploadMaxWait)
	for {
		if dl != nil && dl.Status == client.DownloadStatusDone {
			state.Put("iso_uuid", dl.UUID)
			return multistep.ActionContinue
		}
		if dl != nil && dl.Status == client.DownloadStatusFailed {
			return halt(fmt.Errorf("sylve failed to store the uploaded ISO %q: %s", name, dl.Error))
		}
		if time.Now().After(deadline) {
			return halt(fmt.Errorf("the uploaded ISO %q was not done within %s", name, uploadMaxWait))
		}
		select {
		case <-ctx.Done():
			return halt(ctx.Err())
		case <-time.After(uploadPollInterval):
		}
		if dl, err = c.FindUploadedDownload(name); err != nil {
			return halt(err)
		}
	}
}

// Cleanup does nothing: the upload stays for the next build.
func (s *StepUploadISO) Cleanup(_ multistep.StateBag) {}
