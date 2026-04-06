// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestIsRetriableTransportError_Nil(t *testing.T) {
	if isRetriableTransportError(nil) {
		t.Error("nil should not be retriable")
	}
}

func TestIsRetriableTransportError_SyscallErrors(t *testing.T) {
	if !isRetriableTransportError(syscall.ECONNRESET) {
		t.Error("ECONNRESET should be retriable")
	}
	if !isRetriableTransportError(syscall.ETIMEDOUT) {
		t.Error("ETIMEDOUT should be retriable")
	}
}

func TestIsRetriableTransportError_NetTimeout(t *testing.T) {
	err := &net.DNSError{Err: "i/o timeout", IsTimeout: true}
	if !isRetriableTransportError(err) {
		t.Error("net.Error with Timeout() should be retriable")
	}
}

func TestIsRetriableTransportError_StringSubstrings(t *testing.T) {
	cases := []string{
		"connection reset by peer",
		"broken pipe",
		"net/http: TLS handshake timeout",
		"EOF",
		"context deadline exceeded",
	}
	for _, s := range cases {
		if !isRetriableTransportError(errors.New(s)) {
			t.Errorf("expected retriable: %q", s)
		}
	}
	if isRetriableTransportError(errors.New("permanent failure: no match")) {
		t.Error("generic error should not be retriable")
	}
}

func TestRetryBackoff_ZeroIsNoOp(t *testing.T) {
	start := time.Now()
	retryBackoff(0)
	if time.Since(start) > 20*time.Millisecond {
		t.Fatal("retryBackoff(0) must not sleep")
	}
}

func TestRetryBackoff_CapsAtMaxDelay(t *testing.T) {
	start := time.Now()
	retryBackoff(12)
	elapsed := time.Since(start)
	if elapsed < 3900*time.Millisecond {
		t.Fatalf("expected ~%v cap sleep, got %v", retryMaxDelay, elapsed)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("sleep took too long: %v", elapsed)
	}
}

func TestIsRetriableLoginWaitError_Nil(t *testing.T) {
	if IsRetriableLoginWaitError(nil) {
		t.Error("nil should not be retriable")
	}
}

func TestIsRetriableLoginWaitError_403(t *testing.T) {
	err := errors.New(`API error 403 on POST /auth/login: {}`)
	if IsRetriableLoginWaitError(err) {
		t.Error("403 must not be retriable for login wait")
	}
}

func TestIsRetriableLoginWaitError_502And504(t *testing.T) {
	for _, code := range []string{"502", "504"} {
		err := errors.New("API error " + code + " on POST /auth/login: {}")
		if !IsRetriableLoginWaitError(err) {
			t.Errorf("%s should be retriable", code)
		}
	}
}

func TestIsRetriableLoginWaitError_500NotRetriable(t *testing.T) {
	err := errors.New(`API error 500 on POST /auth/login: {}`)
	if IsRetriableLoginWaitError(err) {
		t.Error("500 should not be retriable for login wait")
	}
}

func TestIsRetriableLoginWaitError_GenericAPIErrorNotRetriable(t *testing.T) {
	err := errors.New(`API error 400 on POST /auth/login: bad request`)
	if IsRetriableLoginWaitError(err) {
		t.Error("400 should not be retriable for login wait")
	}
}
