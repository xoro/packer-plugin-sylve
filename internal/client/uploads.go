// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

// DownloadTypePath is the Download.Type Sylve gives a file uploaded through its
// downloader (as opposed to "http" or "torrent" for what Sylve fetched itself).
const DownloadTypePath = "path"

// downloaderUploadReceipt is the data of Sylve's answer to
// POST /api/utilities/downloader-uploads: the staged upload.
type downloaderUploadReceipt struct {
	UploadID string `json:"uploadId"`
}

// downloaderUploadCompletion is the data of Sylve's answer to
// POST /api/utilities/downloader-uploads/:id/complete.
type downloaderUploadCompletion struct {
	DownloadID uint `json:"downloadId"`
}

// UploadFile streams the local file at path to Sylve's downloader staging
// (POST /api/utilities/downloader-uploads, multipart field "filepond") under
// name, then completes it into a download (POST .../:id/complete) and returns
// that download's ID. Sylve stores it as Type "path" named name. The transfer
// has no timeout of its own (an ISO can take minutes); ctx cancels it. When
// the completion fails, the staged upload is aborted (DELETE .../:id).
// Side effects: one file read, one upload, one download created in Sylve.
func (c *Client) UploadFile(ctx context.Context, path, name string) (uint, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("upload %q: %w", name, err)
	}
	defer f.Close()

	body, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		part, err := form.CreateFormFile("filepond", name)
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = form.Close()
		}
		_ = writer.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/utilities/downloader-uploads", body)
	if err != nil {
		_ = body.Close()
		return 0, fmt.Errorf("upload %q: build request: %w", name, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	// The API client's 60 s timeout would cut the transfer off; ctx bounds it instead.
	resp, err := (&http.Client{Transport: c.HTTPClient.Transport}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("upload %q: %w", name, err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return 0, fmt.Errorf("upload %q: read response: %w", name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("upload %q: API error %d: %s", name, resp.StatusCode, string(raw))
	}
	var receipt APIResponse[downloaderUploadReceipt]
	if err := json.Unmarshal(raw, &receipt); err != nil || receipt.Data.UploadID == "" {
		return 0, fmt.Errorf("upload %q: no upload ID in %s", name, string(raw))
	}

	var done APIResponse[downloaderUploadCompletion]
	completePath := "/utilities/downloader-uploads/" + receipt.Data.UploadID
	if err := c.post(completePath+"/complete", map[string]string{"downloadType": "uncategorized"}, &done); err != nil {
		_ = c.delete(completePath)
		return 0, fmt.Errorf("upload %q: complete: %w", name, err)
	}
	return done.Data.DownloadID, nil
}

// FindUploadedDownload returns the download Sylve holds for a file uploaded
// under name (Type "path", Name name), or nil when there is none. A download
// Sylve fetched by URL never matches, whatever its name. Returns an error when
// the list cannot be read.
func (c *Client) FindUploadedDownload(name string) (*Download, error) {
	downloads, err := c.ListDownloads()
	if err != nil {
		return nil, fmt.Errorf("find uploaded download %q: %w", name, err)
	}
	for i := range downloads {
		if downloads[i].Type == DownloadTypePath && downloads[i].Name == name {
			return &downloads[i], nil
		}
	}
	return nil, nil
}
