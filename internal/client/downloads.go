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
type DownloadFileRequest struct {
	URL   string `json:"url"`
	Type  string `json:"type"`
	UType string `json:"uType"`
}

// TriggerDownload calls POST /api/utilities/downloads.
// The API returns no ID; use ListDownloads to poll by URL.
func (c *Client) TriggerDownload(url string) error {
	req := DownloadFileRequest{
		URL:   url,
		Type:  "http",
		UType: "Packer",
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
