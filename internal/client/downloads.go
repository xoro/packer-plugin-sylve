// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import "fmt"

// DownloadStatus mirrors utilitiesModels.DownloadStatus.
type DownloadStatus string

const (
	DownloadStatusPending    DownloadStatus = "pending"
	DownloadStatusProcessing DownloadStatus = "processing"
	DownloadStatusDone       DownloadStatus = "done"
	DownloadStatusFailed     DownloadStatus = "failed"
)

// Download mirrors the Sylve downloads model.
type Download struct {
	ID       uint           `json:"id"`
	UUID     string         `json:"uuid"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Type     string         `json:"type"`
	UType    string         `json:"uType"`
	Status   DownloadStatus `json:"status"`
	Progress int            `json:"progress"`
	Size     int64          `json:"size"`
	Error    string         `json:"error"`
}

// DownloadFileRequest is the body sent to POST /api/utilities/downloads.
// The "type" field (http/magnet/local) no longer exists in the request; Sylve
// auto-detects it from the URL. DownloadType classifies the download's
// purpose and must be one of "base-rootfs", "cloud-init", or "uncategorized";
// any other value (including the old ad hoc "Packer" string) is rejected with
// 422 download_request_unprocessable.
type DownloadFileRequest struct {
	URL          string `json:"url"`
	DownloadType string `json:"downloadType"`
}

// TriggerDownload calls POST /api/utilities/downloads.
// The API returns no ID; use ListDownloads to poll by URL.
func (c *Client) TriggerDownload(url string) error {
	req := DownloadFileRequest{
		URL:          url,
		DownloadType: "uncategorized",
	}
	var resp APIResponse[interface{}]
	if err := c.post("/utilities/downloads", req, &resp); err != nil {
		return fmt.Errorf("trigger download %q: %w", url, err)
	}
	return nil
}

// ListDownloads calls GET /api/utilities/downloads.
func (c *Client) ListDownloads() ([]Download, error) {
	var resp APIResponse[[]Download]
	if err := c.get("/utilities/downloads", &resp); err != nil {
		return nil, fmt.Errorf("list downloads: %w", err)
	}
	return resp.Data, nil
}

// FindDownloadByURL returns the Download entry whose URL matches, or nil if not found.
func (c *Client) FindDownloadByURL(url string) (*Download, error) {
	downloads, err := c.ListDownloads()
	if err != nil {
		return nil, err
	}
	for i := range downloads {
		if downloads[i].URL == url {
			return &downloads[i], nil
		}
	}
	return nil, nil
}

// DeleteDownload calls DELETE /api/utilities/downloads/:id. Use this to clear
// a stale "failed" download record (e.g. from a transient network error on a
// previous build) before retriggering a fresh download for the same URL.
func (c *Client) DeleteDownload(id uint) error {
	path := fmt.Sprintf("/utilities/downloads/%d", id)
	if err := c.delete(path); err != nil {
		return fmt.Errorf("delete download id=%d: %w", id, err)
	}
	return nil
}
