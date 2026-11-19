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
		if r.URL.Path != "/api/vm/templates" || r.Method != http.MethodGet {
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
		if r.URL.Path != "/api/vm/templates" {
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
		if r.URL.Path != "/api/vm/templates" {
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
		if r.URL.Path != "/api/vm/templates/5/vms" || r.Method != http.MethodPost {
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

func TestFreeRIDs_FirstFree(t *testing.T) {
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
	free, err := c.FreeRIDs()
	if err != nil {
		t.Fatalf("FreeRIDs: %v", err)
	}
	// RIDs 1, 2 and 4 are used: 3 first, then 5 on, up to 9999.
	if len(free) != 9996 || free[0] != 3 || free[1] != 5 || free[len(free)-1] != 9999 {
		t.Errorf("FreeRIDs = %d free, starting %v, want 9996 starting [3 5]", len(free), free[:2])
	}
}

func TestFreeRIDs_ListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", false)
	_, err := c.FreeRIDs()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFreeRIDs_AllUsed(t *testing.T) {
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
	_, err := c.FreeRIDs()
	if err == nil {
		t.Fatal("expected error when all RIDs are used")
	}
}

func TestConvertVMToTemplate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ConvertToTemplateRequest
		if r.URL.Path != "/api/vm/7/templates" || r.Method != http.MethodPost ||
			json.NewDecoder(r.Body).Decode(&body) != nil || body.Name != "base" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(APIResponse[templateCaptureTask]{Status: "success", Data: templateCaptureTask{TaskID: 42}})
	}))
	t.Cleanup(srv.Close)

	taskID, err := newTestClient(srv.URL, "tok").ConvertVMToTemplate(7, "base")
	if err != nil || taskID != 42 {
		t.Fatalf("ConvertVMToTemplate = %d, %v; want 42, nil", taskID, err)
	}
}

func TestConvertVMToTemplate_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"vm_must_be_shut_off"}`, http.StatusConflict)
	}))
	t.Cleanup(srv.Close)

	if _, err := newTestClient(srv.URL, "tok").ConvertVMToTemplate(7, "base"); err == nil {
		t.Fatal("ConvertVMToTemplate: want error for HTTP 409, got nil")
	}
}

// templateTaskServer answers GET /api/tasks/lifecycle/recent for vm-template
// guest 7 with the given tasks.
func templateTaskServer(t *testing.T, tasks []LifecycleTask) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/tasks/lifecycle/recent" || q.Get("guestType") != "vm-template" || q.Get("guestId") != "7" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[[]LifecycleTask]{Status: "success", Data: tasks})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTemplateTask_Found(t *testing.T) {
	srv := templateTaskServer(t, []LifecycleTask{{ID: 41, Status: "success"}, {ID: 42, Status: "failed", Error: "boom"}})

	task, err := newTestClient(srv.URL, "tok").TemplateTask(7, 42)
	if err != nil || task.Status != LifecycleTaskFailed || task.Error != "boom" {
		t.Fatalf("TemplateTask = %+v, %v; want task 42 failed with boom", task, err)
	}
}

func TestTemplateTask_NotListed(t *testing.T) {
	srv := templateTaskServer(t, []LifecycleTask{{ID: 41, Status: "success"}})

	if _, err := newTestClient(srv.URL, "tok").TemplateTask(7, 42); err == nil {
		t.Fatal("TemplateTask: want error for an unlisted task, got nil")
	}
}

func TestTemplateTask_Error(t *testing.T) {
	srv := templateTaskServer(t, nil)

	if _, err := newTestClient(srv.URL, "tok").TemplateTask(8, 42); err == nil {
		t.Fatal("TemplateTask: want error for HTTP 404, got nil")
	}
}
