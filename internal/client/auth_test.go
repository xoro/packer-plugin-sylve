// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.Username != "admin" || req.Password != "secret" || req.AuthType != "sylve" {
			t.Errorf("unexpected login body: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[LoginResponse]{
			Status: "ok",
			Data:   LoginResponse{Token: "mytoken"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "", false)
	token, err := c.Login("admin", "secret", "sylve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mytoken" {
		t.Errorf("token = %q, want %q", token, "mytoken")
	}
}

func TestLogin_SetsTokenOnClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[LoginResponse]{
			Data: LoginResponse{Token: "tok123"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "oldtoken", false)
	_, err := c.Login("u", "p", "sylve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Token != "tok123" {
		t.Errorf("c.Token = %q after login, want %q", c.Token, "tok123")
	}
}

func TestLogin_Error_NonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "", false)
	_, err := c.Login("admin", "wrong", "sylve")
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_Success_ClearsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/logout" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		// Sylve returns an HTML page for /auth/logout — client must tolerate this.
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>OK</html>"))
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken", false)
	if err := c.Logout(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Token != "" {
		t.Errorf("c.Token = %q after logout, want empty string", c.Token)
	}
}

func TestLogout_Error_NonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if err := c.Logout(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
