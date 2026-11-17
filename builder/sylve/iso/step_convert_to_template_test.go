// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// fakeTemplateSylve is a Sylve API for StepConvertToTemplate: VM id 3 with
// RID 9, converted by task 42 into template "base" (id 5).
type fakeTemplateSylve struct {
	activeCalls  int32    // the first activeCalls polls report an active task
	activeStatus int      // non-zero: the active-task poll answers this status
	convertCode  int      // non-zero: the conversion answers this status
	statuses     []string // successive statuses of task 42; the last one repeats
	taskErrors   int32    // the first taskErrors task polls answer HTTP 500
	noTemplate   bool     // the template list lacks "base"
	taskPolls    int32
	converted    int32
}

func (f *fakeTemplateSylve) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/lifecycle/active/vm/3":
			if f.activeStatus != 0 {
				w.WriteHeader(f.activeStatus)
				return
			}
			var data map[string]interface{}
			if atomic.AddInt32(&f.activeCalls, -1) >= 0 {
				data = map[string]interface{}{"id": 1}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "success", Data: data})
		case r.URL.Path == "/api/vm/9/templates" && r.Method == http.MethodPost:
			atomic.AddInt32(&f.converted, 1)
			if f.convertCode != 0 {
				w.WriteHeader(f.convertCode)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"success","data":{"taskId":42}}`))
		case r.URL.Path == "/api/tasks/lifecycle/recent":
			n := atomic.AddInt32(&f.taskPolls, 1)
			if n <= f.taskErrors {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			status := f.statuses[min(int(n-f.taskErrors), len(f.statuses))-1]
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.LifecycleTask]{
				Status: "success",
				Data:   []client.LifecycleTask{{ID: 42, Status: status, Error: "disk copy failed"}},
			})
		case r.URL.Path == "/api/vm/templates":
			list := []client.SimpleTemplate{{ID: 4, Name: "other"}}
			if !f.noTemplate {
				list = append(list, client.SimpleTemplate{ID: 5, Name: "base"})
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleTemplate]{Status: "success", Data: list})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runConvertStep runs StepConvertToTemplate for template "base" against srv
// with millisecond polling and returns its action and state.
func runConvertStep(t *testing.T, ctx context.Context, srv *httptest.Server, withVMID bool) (multistep.StepAction, multistep.StateBag) {
	t.Helper()
	origPoll, origMax := templatePollInterval, templateMaxWait
	t.Cleanup(func() { templatePollInterval, templateMaxWait = origPoll, origMax })
	templatePollInterval, templateMaxWait = time.Millisecond, 2*time.Second

	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	if withVMID {
		state.Put("vm_id", uint(3))
	}
	step := &StepConvertToTemplate{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, TemplateName: "base"}}
	defer step.Cleanup(state)
	return step.Run(ctx, state), state
}

// stepError returns the error the step put into the state bag, or "".
func stepError(state multistep.StateBag) string {
	if err, ok := state.GetOk("error"); ok {
		return err.(error).Error()
	}
	return ""
}

func TestStepConvertToTemplate_NoTemplateNameDoesNothing(t *testing.T) {
	state := new(multistep.BasicStateBag)
	step := &StepConvertToTemplate{Config: &Config{SylveURL: "http://127.0.0.1:1"}}
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if _, ok := state.GetOk("template_id"); ok {
		t.Fatal("template_id set without template_name")
	}
}

func TestStepConvertToTemplate_SavesTemplate(t *testing.T) {
	f := &fakeTemplateSylve{activeCalls: 2, statuses: []string{"queued", "running", "success"}}
	got, state := runConvertStep(t, context.Background(), f.serve(t), true)
	if got != multistep.ActionContinue {
		t.Fatalf("Run() = %v (%s), want ActionContinue", got, stepError(state))
	}
	if state.Get("template_id") != uint(5) || state.Get("template_name") != "base" {
		t.Fatalf("state template = %v / %v, want 5 / base", state.Get("template_id"), state.Get("template_name"))
	}
}

func TestStepConvertToTemplate_WithoutVMIDSkipsTaskWait(t *testing.T) {
	f := &fakeTemplateSylve{activeStatus: http.StatusInternalServerError, statuses: []string{"success"}}
	if got, state := runConvertStep(t, context.Background(), f.serve(t), false); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v (%s), want ActionContinue", got, stepError(state))
	}
}

func TestStepConvertToTemplate_TaskPollErrorsAreRetried(t *testing.T) {
	f := &fakeTemplateSylve{taskErrors: 2, statuses: []string{"success"}}
	if got, state := runConvertStep(t, context.Background(), f.serve(t), true); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v (%s), want ActionContinue", got, stepError(state))
	}
}

func TestStepConvertToTemplate_HaltsWhenSylveRejects(t *testing.T) {
	f := &fakeTemplateSylve{convertCode: http.StatusConflict}
	got, state := runConvertStep(t, context.Background(), f.serve(t), true)
	if got != multistep.ActionHalt || !strings.Contains(stepError(state), "409") {
		t.Fatalf("Run() = %v (%s), want ActionHalt with HTTP 409", got, stepError(state))
	}
}

func TestStepConvertToTemplate_HaltsWithSylvesErrorWhenTaskFails(t *testing.T) {
	f := &fakeTemplateSylve{statuses: []string{"running", "failed"}}
	got, state := runConvertStep(t, context.Background(), f.serve(t), true)
	if got != multistep.ActionHalt || !strings.Contains(stepError(state), "disk copy failed") {
		t.Fatalf("Run() = %v (%s), want ActionHalt naming Sylve's error", got, stepError(state))
	}
}

func TestStepConvertToTemplate_HaltsWhenTemplateMissing(t *testing.T) {
	f := &fakeTemplateSylve{statuses: []string{"success"}, noTemplate: true}
	got, state := runConvertStep(t, context.Background(), f.serve(t), true)
	if got != multistep.ActionHalt || !strings.Contains(stepError(state), "template not found") {
		t.Fatalf("Run() = %v (%s), want ActionHalt with template not found", got, stepError(state))
	}
}

func TestStepConvertToTemplate_HaltsWhenTaskNeverEnds(t *testing.T) {
	f := &fakeTemplateSylve{statuses: []string{"running"}}
	srv := f.serve(t)
	got, state := runConvertStep(t, context.Background(), srv, true)
	if got != multistep.ActionHalt || !strings.Contains(stepError(state), "no result within") {
		t.Fatalf("Run() = %v (%s), want ActionHalt after the wait", got, stepError(state))
	}
}

func TestStepConvertToTemplate_TimeoutNamesLastPollError(t *testing.T) {
	f := &fakeTemplateSylve{activeStatus: http.StatusInternalServerError}
	got, state := runConvertStep(t, context.Background(), f.serve(t), true)
	if got != multistep.ActionHalt || !strings.Contains(stepError(state), "500") || f.converted != 0 {
		t.Fatalf("Run() = %v (%s, converted %d), want ActionHalt naming HTTP 500 before converting",
			got, stepError(state), f.converted)
	}
}

func TestStepConvertToTemplate_HaltsWhenCancelled(t *testing.T) {
	f := &fakeTemplateSylve{activeCalls: 1 << 20}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, state := runConvertStep(t, ctx, f.serve(t), true)
	if got != multistep.ActionHalt || stepError(state) != context.Canceled.Error() {
		t.Fatalf("Run() = %v (%s), want ActionHalt with %v", got, stepError(state), context.Canceled)
	}
}
