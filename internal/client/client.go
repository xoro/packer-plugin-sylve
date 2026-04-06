// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package client provides an HTTP client for the Sylve REST API.
// Base URL: https://<host>:8181/api
// Authentication: Bearer token via Authorization header.
package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// APIResponse is the standard Sylve API response envelope.
type APIResponse[T any] struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    T      `json:"data"`
	Error   string `json:"error"`
}

// DomainState mirrors libvirt.DomainState integer values returned in VM.State.
type DomainState int32

const (
	DomainStateNoState     DomainState = 0
	DomainStateRunning     DomainState = 1
	DomainStateBlocked     DomainState = 2
	DomainStatePaused      DomainState = 3
	DomainStateShutdown    DomainState = 4
	DomainStateShutoff     DomainState = 5
	DomainStateCrashed     DomainState = 6
	DomainStatePMSuspended DomainState = 7
)

// Client is a Sylve API client.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// New creates a new Client. If tlsSkipVerify is true the TLS certificate is
// not validated (required for Sylve's self-signed certificate).
func New(baseURL, token string, tlsSkipVerify bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: tlsSkipVerify,    // [SECURITY DESIGN] intentional: Sylve ships self-signed cert
			MinVersion:         tls.VersionTLS12, // [SECURITY DESIGN] floor TLS 1.2; Sylve server determines actual version
		},
	}
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
	}
}

// do executes an HTTP request and decodes the response body into out.
// out may be nil when no response body is expected.
// Transient transport errors and HTTP 502/503/504 are retried with exponential
// backoff so brief Sylve restarts or network blips do not fail the build.
func (c *Client) do(method, path string, body, out interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		log.Printf("[DEBUG] sylve client %s /api%s body: %s", method, path, string(bodyBytes))
	}

	var lastErr error
	for attempt := 1; attempt <= maxHTTPAttempts; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequest(method, c.BaseURL+"/api"+path, bodyReader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("execute request %s %s: %w", method, path, err)
			if attempt < maxHTTPAttempts && isRetriableTransportError(err) {
				log.Printf("[DEBUG] sylve client %s /api%s retry %d/%d after transport error: %v", method, path, attempt, maxHTTPAttempts, err)
				retryBackoff(attempt)
				continue
			}
			return lastErr
		}

		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		log.Printf("[DEBUG] sylve client %s /api%s status=%d response: %s", method, path, resp.StatusCode, string(raw))

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil {
				if err := json.Unmarshal(raw, out); err != nil {
					return fmt.Errorf("decode response: %w", err)
				}
			}
			return nil
		}

		lastErr = fmt.Errorf("API error %d on %s %s: %s", resp.StatusCode, method, path, string(raw))
		if attempt < maxHTTPAttempts && isRetriableHTTPStatus(resp.StatusCode) {
			log.Printf("[DEBUG] sylve client %s /api%s retry %d/%d after HTTP %d", method, path, attempt, maxHTTPAttempts, resp.StatusCode)
			retryBackoff(attempt)
			continue
		}
		return lastErr
	}
	return lastErr
}

// get is a convenience wrapper for GET requests.
func (c *Client) get(path string, out interface{}) error {
	return c.do(http.MethodGet, path, nil, out)
}

// IsNotFound reports whether err was caused by an HTTP 404 response from the
// Sylve API. Use this to distinguish "resource does not exist" from transient
// network or server errors.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "API error 404 ")
}

// post is a convenience wrapper for POST requests.
func (c *Client) post(path string, body, out interface{}) error {
	return c.do(http.MethodPost, path, body, out)
}

// put is a convenience wrapper for PUT requests.
func (c *Client) put(path string, body, out interface{}) error {
	return c.do(http.MethodPut, path, body, out)
}

// delete is a convenience wrapper for DELETE requests.
func (c *Client) delete(path string) error {
	return c.do(http.MethodDelete, path, nil, nil)
}
