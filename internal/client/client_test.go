// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// newTestClient creates a Client pointed at the given test server URL.
func newTestClient(serverURL, token string) *Client {
	c := New(serverURL, token, true)
	c.BaseURL = serverURL
	return c
}

// respondJSON writes a JSON-encoded value to w with the given status code.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}

// ---------------------------------------------------------------------------
// New
// ---------------------------------------------------------------------------

func TestNew_Fields(t *testing.T) {
	c := New("https://host:8181", "mytoken", false)
	if c.BaseURL != "https://host:8181" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://host:8181")
	}
	if c.Token != "mytoken" {
		t.Errorf("Token = %q, want %q", c.Token, "mytoken")
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient is nil")
	}
}

// ---------------------------------------------------------------------------
// Authorization header
// ---------------------------------------------------------------------------

func TestDo_SetsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "bearer-token-123")
	if err := c.get("/anything", &APIResponse[interface{}]{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Bearer bearer-token-123"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// ---------------------------------------------------------------------------
// Content-Type header
// ---------------------------------------------------------------------------

func TestDo_SetsContentType_WithBody(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	type req struct{ Name string }
	c := newTestClient(srv.URL, "tok")
	if err := c.post("/anything", req{Name: "test"}, &APIResponse[interface{}]{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotCT, "application/json")
	}
}

func TestDo_NoContentType_WithoutBody(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	if err := c.get("/anything", &APIResponse[interface{}]{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCT != "" {
		t.Errorf("Content-Type = %q for GET, want empty", gotCT)
	}
}

// ---------------------------------------------------------------------------
// Successful responses
// ---------------------------------------------------------------------------

func TestGet_DecodesResponse(t *testing.T) {
	type payload struct{ Value string }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, APIResponse[payload]{
			Status: "ok",
			Data:   payload{Value: "hello"},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	var out APIResponse[payload]
	if err := c.get("/test", &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Data.Value != "hello" {
		t.Errorf("Data.Value = %q, want %q", out.Data.Value, "hello")
	}
}

func TestPost_SendsBody(t *testing.T) {
	type req struct{ Name string }
	var gotBody req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	if err := c.post("/test", req{Name: "packer"}, &APIResponse[interface{}]{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody.Name != "packer" {
		t.Errorf("received body Name = %q, want %q", gotBody.Name, "packer")
	}
}

func TestDelete_UsesDeleteMethod(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	if err := c.delete("/resource/1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("HTTP method = %q, want DELETE", gotMethod)
	}
}

// ---------------------------------------------------------------------------
// Error responses
// ---------------------------------------------------------------------------

func TestDo_Error_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 404, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	err := c.get("/missing", &APIResponse[interface{}]{})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not contain status code '404'", err.Error())
	}
}

func TestDo_Error_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	err := c.get("/bad", &APIResponse[interface{}]{})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not contain '500'", err.Error())
	}
}

func TestDo_Error_ConnectionRefused(t *testing.T) {
	// Point at a port that should have nothing listening.
	c := New("http://127.0.0.1:19999", "tok", true)
	err := c.get("/anything", nil)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// ---------------------------------------------------------------------------
// Path construction
// ---------------------------------------------------------------------------

func TestDo_PathPrefixedWithAPI(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	_ = c.get("/network/switch", &APIResponse[interface{}]{})
	if gotPath != "/api/network/switch" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/network/switch")
	}
}

// ---------------------------------------------------------------------------
// do: JSON decode error
// ---------------------------------------------------------------------------

func TestDo_Error_MalformedBaseURL(t *testing.T) {
	c := &Client{
		BaseURL:    "http://a b",
		Token:      "tok",
		HTTPClient: http.DefaultClient,
	}
	err := c.get("/x", &APIResponse[interface{}]{})
	if err == nil {
		t.Fatal("expected error for malformed BaseURL, got nil")
	}
	if !strings.Contains(err.Error(), "build request") {
		t.Errorf("error %q does not mention build request", err.Error())
	}
}

func TestDo_Error_MarshalBody(t *testing.T) {
	c := newTestClient("http://unused.example", "tok")
	// Channels cannot be JSON-encoded; json.Marshal returns an error.
	err := c.post("/x", map[string]any{"c": make(chan int)}, nil)
	if err == nil {
		t.Fatal("expected error when marshaling request body, got nil")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error %q does not mention marshal request", err.Error())
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDo_Error_ReadResponseBody(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1", "tok")
	c.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(errReader{}),
				Request:    req,
			}, nil
		}),
	}
	err := c.get("/anything", &APIResponse[interface{}]{})
	if err == nil {
		t.Fatal("expected error when reading response body, got nil")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("error %q does not mention read response", err.Error())
	}
}

func TestDo_Error_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return HTTP 200 but with non-JSON body to trigger json.Unmarshal error.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json!!!"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	var out APIResponse[interface{}]
	err := c.get("/anything", &out)
	if err == nil {
		t.Fatal("expected error for malformed JSON response, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error %q does not mention 'decode response'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// put
// ---------------------------------------------------------------------------

func TestPut_SendsBody(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	type result struct {
		Echo string `json:"echo"`
	}
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		var p payload
		_ = json.NewDecoder(r.Body).Decode(&p)
		respondJSON(w, 200, APIResponse[result]{Data: result{Echo: p.Value}})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "tok")
	var out APIResponse[result]
	if err := c.put("/test", payload{Value: "hello"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if out.Data.Echo != "hello" {
		t.Errorf("echo = %q, want %q", out.Data.Echo, "hello")
	}
}

// ---------------------------------------------------------------------------
// Retries (transient API / network)
// ---------------------------------------------------------------------------

func TestDo_RetriesOn503ThenSuccess(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&n, 1)
		if c <= 2 {
			http.Error(w, "bad", http.StatusServiceUnavailable)
			return
		}
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "t")
	var out APIResponse[interface{}]
	if err := c.get("/ping", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if n != 3 {
		t.Fatalf("HTTP attempts = %d, want 3", n)
	}
}

func TestDo_RetriesOnTransportErrorThenSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	var attempts int32
	base := http.DefaultTransport
	c := newTestClient(srv.URL, "t")
	c.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			a := atomic.AddInt32(&attempts, 1)
			if a <= 2 {
				return nil, fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED)
			}
			return base.RoundTrip(req)
		}),
	}
	var out APIResponse[interface{}]
	if err := c.get("/ping", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("HTTP attempts = %d, want 3", attempts)
	}
}

func TestDo_NoRetryOn500(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		http.Error(w, "err", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "t")
	var out APIResponse[interface{}]
	if err := c.get("/ping", &out); err == nil {
		t.Fatal("expected error")
	}
	if n != 1 {
		t.Fatalf("HTTP attempts = %d, want 1", n)
	}
}

type errReadCloser struct{ err error }

func (e errReadCloser) Read(p []byte) (int, error) { return 0, e.err }
func (errReadCloser) Close() error                 { return nil }

func TestDo_ReadResponseBodyError(t *testing.T) {
	c := newTestClient("http://unused.example", "t")
	c.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       errReadCloser{err: errors.New("simulated read failure")},
			}, nil
		}),
	}
	var out APIResponse[interface{}]
	err := c.get("/ping", &out)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Fatalf("err=%v", err)
	}
}

func TestDo_JSONDecodeErrorOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "t")
	var out APIResponse[interface{}]
	err := c.get("/ping", &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err=%v", err)
	}
}

func TestDo_MarshalRequestBodyError(t *testing.T) {
	c := newTestClient("http://unused.example", "t")
	c.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected RoundTrip")
		}),
	}
	body := map[string]interface{}{"x": make(chan int)}
	err := c.do(http.MethodPost, "/ping", body, nil)
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Fatalf("err=%v", err)
	}
}

func TestGet_NewRequestInvalidBaseURL(t *testing.T) {
	c := &Client{
		BaseURL:    "http://192.168.0.1\r\n/evil",
		Token:      "t",
		HTTPClient: http.DefaultClient,
	}
	var out APIResponse[interface{}]
	err := c.get("/ping", &out)
	if err == nil {
		t.Fatal("expected error from http.NewRequest")
	}
	if !strings.Contains(err.Error(), "build request") {
		t.Fatalf("err=%v", err)
	}
}

func TestIsRetriableLoginWaitError_401(t *testing.T) {
	err := errors.New(`sylve login as "u": execute request POST /auth/login: API error 401 on POST /auth/login: {}`)
	if IsRetriableLoginWaitError(err) {
		t.Error("401 must not be retriable for login wait")
	}
}

func TestIsRetriableLoginWaitError_503(t *testing.T) {
	err := errors.New(`sylve login as "u": execute request POST /auth/login: API error 503 on POST /auth/login: {}`)
	if !IsRetriableLoginWaitError(err) {
		t.Error("503 should be retriable for login wait")
	}
}

func TestIsRetriableTransportError_Refused(t *testing.T) {
	err := errors.New(`Get "https://h:8181/api/x": dial tcp 127.0.0.1:8181: connect: connection refused`)
	if !isRetriableTransportError(err) {
		t.Error("expected connection refused to be retriable")
	}
}

// ---------------------------------------------------------------------------
// IsNotFound
// ---------------------------------------------------------------------------

func TestIsNotFound_NilError(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) = true, want false")
	}
}

func TestIsNotFound_404Error(t *testing.T) {
	err := errors.New("API error 404 on GET /vm/1: not found")
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%q) = false, want true", err)
	}
}

func TestIsNotFound_OtherError(t *testing.T) {
	err := errors.New("API error 500 on GET /vm/1: internal error")
	if IsNotFound(err) {
		t.Errorf("IsNotFound(%q) = true, want false", err)
	}
}
