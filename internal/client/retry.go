// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"time"
)

// Retry limits for transient Sylve API failures (listener restart, brief network
// loss). Each logical do() may perform up to maxHTTPAttempts HTTP round-trips.
const (
	maxHTTPAttempts = 5
	retryBaseDelay  = 200 * time.Millisecond
	retryMaxDelay   = 4 * time.Second
)

// isRetriableTransportError reports whether err is likely temporary (worth retrying).
func isRetriableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "TLS handshake timeout") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "context deadline exceeded")
}

func isRetriableHTTPStatus(code int) bool {
	return code == 502 || code == 503 || code == 504
}

func retryBackoff(attempt int) {
	if attempt <= 0 {
		return
	}
	d := retryBaseDelay * time.Duration(1<<uint(attempt-1))
	if d > retryMaxDelay {
		d = retryMaxDelay
	}
	time.Sleep(d)
}

// IsRetriableLoginWaitError reports whether a failed Login is worth retrying
// while waiting for Sylve to start (transport errors, gateway timeouts).
// Wrong credentials (HTTP 401/403) are not retriable.
func IsRetriableLoginWaitError(err error) bool {
	if err == nil {
		return false
	}
	if isRetriableTransportError(err) {
		return true
	}
	s := err.Error()
	if strings.Contains(s, "API error 401 ") || strings.Contains(s, "API error 403 ") {
		return false
	}
	if strings.Contains(s, "API error 502 ") || strings.Contains(s, "API error 503 ") || strings.Contains(s, "API error 504 ") {
		return true
	}
	return false
}
