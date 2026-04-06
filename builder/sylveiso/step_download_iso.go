// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"

	getter "github.com/hashicorp/go-getter/v2"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepDownloadISO triggers a download of the ISO through the Sylve download
// manager and polls until the download status is "done". The ISO UUID is then
// stored in the state bag under the key "iso_uuid".
type StepDownloadISO struct {
	Config *Config
}

// downloadISOPollInterval is overridable in tests.
var downloadISOPollInterval = 1 * time.Second

// downloadISOTotalTimeout bounds how long StepDownloadISO polls the Sylve
// download list; tests shorten it to exercise the timeout branch.
var downloadISOTotalTimeout = 30 * time.Minute

// isoProgressTracker wraps the Packer UI ProgressTracker to render an
// in-place progress bar for server-side Sylve ISO downloads. The download
// itself happens on the Sylve host; we feed fake bytes proportional to the
// reported progress percentage so Packer's own progress bar renders exactly
// the same way as commonsteps.StepDownload does.
type isoProgressTracker struct {
	pw          *io.PipeWriter
	lastPct     int
	fakeTotalSz int64
}

// advance writes (newPct - lastPct) worth of fake bytes so the bar advances.
func (t *isoProgressTracker) advance(newPct int) {
	if newPct <= t.lastPct {
		return
	}
	delta := int64(newPct-t.lastPct) * t.fakeTotalSz / 100
	if delta <= 0 {
		return
	}
	// Write a zero-filled slice; content is discarded by the reader.
	t.pw.Write(make([]byte, delta))
	t.lastPct = newPct
}

// done advances to 100% and closes the pipe, which finalises the bar.
func (t *isoProgressTracker) done() {
	t.advance(100)
	t.pw.Close()
}

// cancel closes the pipe without finishing, which aborts the bar.
func (t *isoProgressTracker) cancel() {
	t.pw.CloseWithError(io.ErrUnexpectedEOF)
}

func (s *StepDownloadISO) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	ui.Say("Retrieving ISO")
	ui.Say(fmt.Sprintf("Trying %s", s.Config.ISODownloadURL))

	existing, err := c.FindDownloadByURL(s.Config.ISODownloadURL)
	if err != nil {
		existing = nil
	}

	if existing != nil {
		switch existing.Status {
		case client.DownloadStatusDone:
			ui.Say(fmt.Sprintf("ISO already downloaded: uuid=%s", existing.UUID))
			state.Put("iso_uuid", existing.UUID)
			return multistep.ActionContinue
		case client.DownloadStatusFailed:
			err := fmt.Errorf("existing ISO download failed: %s", existing.Error)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		default:
			ui.Say(fmt.Sprintf("ISO download already in progress (status=%s), waiting...", existing.Status))
		}
	} else {
		if err := c.TriggerDownload(s.Config.ISODownloadURL); err != nil {
			err = fmt.Errorf("trigger ISO download: %w", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}

	filename := path.Base(s.Config.ISODownloadURL)

	// Get the actual file size via HEAD request so the progress bar denominator
	// is exact from the start rather than inferred from partial download data.
	remoteSize := getRemoteFileSize(ctx, s.Config.ISODownloadURL)

	// Set up Packer's native progress bar via getter.ProgressTracker.
	var tracker *isoProgressTracker
	if remoteSize > 0 {
		if pt, ok := ui.(getter.ProgressTracker); ok {
			pr, pw := io.Pipe()
			tracker = &isoProgressTracker{pw: pw, fakeTotalSz: remoteSize}
			trackedStream := pt.TrackProgress(filename, 0, remoteSize, pr)
			go func() {
				io.Copy(io.Discard, trackedStream)
				trackedStream.Close()
			}()
		}
	}

	timeout := downloadISOTotalTimeout
	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ctx.Done():
			if tracker != nil {
				tracker.cancel()
			}
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(downloadISOPollInterval):
		}

		if time.Now().After(deadline) {
			if tracker != nil {
				tracker.cancel()
			}
			err := fmt.Errorf("ISO download timed out after %s", timeout)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		dl, err := c.FindDownloadByURL(s.Config.ISODownloadURL)
		if err != nil {
			continue
		}

		switch dl.Status {
		case "done":
			if tracker != nil {
				tracker.done()
			}
			ui.Say(fmt.Sprintf("ISO download complete: uuid=%s", dl.UUID))
			state.Put("iso_uuid", dl.UUID)
			return multistep.ActionContinue
		case "failed":
			if tracker != nil {
				tracker.cancel()
			}
			err := fmt.Errorf("ISO download failed: %s", dl.Error)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		default:
			if tracker != nil {
				tracker.advance(dl.Progress)
			}
		}
	}
}

func (s *StepDownloadISO) Cleanup(_ multistep.StateBag) {}

// getRemoteFileSize issues a HEAD request to url and returns Content-Length.
// Returns 0 if the server does not advertise the size.
func getRemoteFileSize(ctx context.Context, url string) int64 {
	hc := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	return 0
}
