// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListTemplatesSimple_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/templates/simple" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[[]SimpleTemplate]{
			Status: "success",
			Data: []SimpleTemplate{
				{ID: 1, Name: "freebsd-base", SourceVMName: "freebsd-vm"},
				{ID: 2, Name: "windows-base", SourceVMName: "windows-vm"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	templates, err := c.ListTemplatesSimple()
	if err != nil {
		t.Fatalf("ListTemplatesSimple: %v", err)
	}
	if len(templates) != 2 {
		t.Fatalf("got %d templates, want 2", len(templates))
	}
	if templates[0].Name != "freebsd-base" {
		t.Errorf("templates[0].Name = %q, want %q", templates[0].Name, "freebsd-base")
	}
}

func TestListTemplatesSimple_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.ListTemplatesSimple()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindTemplateByName_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/templates/simple" {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[[]SimpleTemplate]{
			Status: "success",
			Data: []SimpleTemplate{
				{ID: 1, Name: "alpha"},
				{ID: 2, Name: "beta"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	tmpl, err := c.FindTemplateByName("beta")
	if err != nil {
		t.Fatalf("FindTemplateByName: %v", err)
	}
	if tmpl.ID != 2 {
		t.Errorf("ID = %d, want 2", tmpl.ID)
	}
}

func TestFindTemplateByName_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/templates/simple" {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[[]SimpleTemplate]{
			Status: "success",
			Data:   []SimpleTemplate{{ID: 1, Name: "alpha"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.FindTemplateByName("gamma")
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestFindTemplateByName_ListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.FindTemplateByName("any")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateVMFromTemplate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/templates/create/5" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	err := c.CreateVMFromTemplate(5, CreateFromTemplateRequest{Name: "new-vm", RID: 10})
	if err != nil {
		t.Fatalf("CreateVMFromTemplate: %v", err)
	}
}

func TestCreateVMFromTemplate_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	err := c.CreateVMFromTemplate(99, CreateFromTemplateRequest{Name: "vm", RID: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindNextFreeRID_FirstFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/simple" {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[[]SimpleVM]{
			Status: "success",
			Data: []SimpleVM{
				{RID: 1, State: DomainStateRunning},
				{RID: 2, State: DomainStateShutoff},
				{RID: 4, State: DomainStateRunning},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	rid, err := c.FindNextFreeRID()
	if err != nil {
		t.Fatalf("FindNextFreeRID: %v", err)
	}
	// RID 1 and 2 are used, 3 is free.
	if rid != 3 {
		t.Errorf("RID = %d, want 3", rid)
	}
}

func TestFindNextFreeRID_ListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.FindNextFreeRID()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindNextFreeRID_AllUsed(t *testing.T) {
	// Build a full list of VMs using RIDs 1-9999.
	allVMs := make([]SimpleVM, 9999)
	for i := range allVMs {
		allVMs[i] = SimpleVM{RID: uint(i + 1), State: DomainStateRunning}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/simple" {
			http.NotFound(w, r)
			return
		}
		resp := APIResponse[[]SimpleVM]{Status: "success", Data: allVMs}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.FindNextFreeRID()
	if err == nil {
		t.Fatal("expected error when all RIDs are used")
	}
}
