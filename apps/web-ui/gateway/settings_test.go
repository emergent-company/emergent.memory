package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- project record ---

func TestGetCurrentProject(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p1","name":"Home","orgId":"o1","project_info":"family","chat_prompt_template":"hi","auto_extract_objects":true,"auto_merge_extraction_branches":false,"budget_usd":25.5}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj")
	p, err := m.GetCurrentProject(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj" || gotMethod != http.MethodGet {
		t.Errorf("got %s %s, want GET /api/projects/proj", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth = %q", gotAuth)
	}
	if p == nil {
		t.Fatal("project = nil")
	}
	if p.ID != "p1" || p.Name != "Home" || p.ProjectInfo != "family" || p.ChatPromptTemplate != "hi" {
		t.Errorf("project = %+v", p)
	}
	if p.AutoExtractObjects == nil || !*p.AutoExtractObjects {
		t.Errorf("auto_extract_objects = %v, want true", p.AutoExtractObjects)
	}
	if p.AutoMergeExtractionBranches == nil || *p.AutoMergeExtractionBranches {
		t.Errorf("auto_merge_extraction_branches = %v, want false", p.AutoMergeExtractionBranches)
	}
	if p.BudgetUSD == nil || *p.BudgetUSD != 25.5 {
		t.Errorf("budget_usd = %v, want 25.5", p.BudgetUSD)
	}
}

func TestGetCurrentProjectSessionProject(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"sess-proj","name":"Session Project"}`)
	}))
	defer srv.Close()

	// A session's active project id wins over the static server project id.
	m := NewMemoryClient(srv.URL, "static-token", "static-proj")
	ctx := withSessionContext(context.Background(), &sessionContext{Token: "sess-token", ProjectID: "sess-proj"})
	p, err := m.GetCurrentProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj" {
		t.Errorf("path = %q, want /api/projects/sess-proj", gotPath)
	}
	if p == nil || p.ID != "sess-proj" {
		t.Errorf("project = %+v", p)
	}
}

func TestGetCurrentProjectNoProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"project":null,"message":"no project"}`)
	}))
	defer srv.Close()

	// No project id resolvable (empty static project, no session) → the
	// token-bound /api/projects/current fallback returns a null project.
	m := NewMemoryClient(srv.URL, "tok", "")
	p, err := m.GetCurrentProject(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Fatalf("project = %+v, want nil", p)
	}
}

func TestGetCurrentProjectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"memory down"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetCurrentProject(context.Background()); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestUpdateProject(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"p1","name":"Renamed","auto_extract_objects":false}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	name := "Renamed"
	info := "notes"
	off := false
	upd := &ProjectUpdate{Name: name, ProjectInfo: &info, AutoExtractObjects: &off}
	p, err := m.UpdateProject(context.Background(), "p1", upd)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/p1" || gotMethod != http.MethodPatch {
		t.Errorf("got %s %s, want PATCH /api/projects/p1", gotMethod, gotPath)
	}
	if gotBody["name"] != "Renamed" || gotBody["project_info"] != "notes" {
		t.Errorf("body = %v", gotBody)
	}
	if gotBody["auto_extract_objects"] != false {
		t.Errorf("auto_extract_objects = %v, want false (explicit)", gotBody["auto_extract_objects"])
	}
	if _, ok := gotBody["budget_usd"]; ok {
		t.Errorf("unset pointer must be omitted, body = %v", gotBody)
	}
	if p.Name != "Renamed" {
		t.Errorf("result = %+v", p)
	}
}

func TestUpdateProjectForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"forbidden","message":"token lacks projects:write"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.UpdateProject(context.Background(), "p1", &ProjectUpdate{Name: "x"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should carry the status: %v", err)
	}
}

// --- agent definition overrides ---

func TestListAgentOverrides(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[
			{"agentName":"memory","override":{"systemPrompt":"be terse","model":{"name":"gpt-4o"},"tools":["web_search"],"maxSteps":10}},
			{"agentName":"diane","override":{"tools":["calendar"]}}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	out, err := m.ListAgentOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/agent-definitions/overrides" || gotMethod != http.MethodGet {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if len(out) != 2 {
		t.Fatalf("got %d overrides, want 2", len(out))
	}
	if out[0].AgentName != "memory" || out[0].Override == nil || *out[0].Override.SystemPrompt != "be terse" || out[0].Override.Model == nil || out[0].Override.Model.Name != "gpt-4o" {
		t.Errorf("entry0 = %+v", out[0])
	}
	if len(out[0].Override.Tools) != 1 || out[0].Override.Tools[0] != "web_search" || *out[0].Override.MaxSteps != 10 {
		t.Errorf("entry0 override = %+v", out[0].Override)
	}
	if out[1].AgentName != "diane" || out[1].Override == nil || len(out[1].Override.Tools) != 1 {
		t.Errorf("entry1 = %+v", out[1])
	}
}

func TestListAgentOverridesAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no overrides"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	out, err := m.ListAgentOverrides(context.Background())
	if err != nil {
		t.Fatalf("absent overrides must not error: %v", err)
	}
	if out != nil {
		t.Fatalf("overrides = %+v, want nil", out)
	}
}

func TestSetAgentOverride(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s1"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	prompt := "be terse"
	steps := 5
	in := &AgentOverrideInput{SystemPrompt: &prompt, Tools: []string{"web_search", "memory_lookup"}, MaxSteps: &steps}
	if err := m.SetAgentOverride(context.Background(), "memory", in); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/agent-definitions/overrides/memory" || gotMethod != http.MethodPut {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if gotBody["systemPrompt"] != "be terse" || gotBody["maxSteps"] != float64(5) {
		t.Errorf("body = %v", gotBody)
	}
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Errorf("tools = %v", gotBody["tools"])
	}
}

func TestSetAgentOverrideEscapesName(t *testing.T) {
	var gotEscaped string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscaped = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"success":true,"data":{}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SetAgentOverride(context.Background(), "memory smith", &AgentOverrideInput{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotEscaped, "memory%20smith") {
		t.Errorf("path should escape the agent name: %q", gotEscaped)
	}
}

func TestSetAgentOverrideError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"boom"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SetAgentOverride(context.Background(), "memory", &AgentOverrideInput{}); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestDeleteAgentOverride(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = io.WriteString(w, `{"success":true,"message":"deleted"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.DeleteAgentOverride(context.Background(), "memory"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/agent-definitions/overrides/memory" || gotMethod != http.MethodDelete {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestDeleteAgentOverrideAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no override"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteAgentOverride(context.Background(), "memory"); err != nil {
		t.Fatalf("deleting an absent override must not error: %v", err)
	}
}

// --- generic project settings ---

func TestGetProjectSetting(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s1","projectId":"proj-1","category":"remember_config","key":"agent_name","value":{"name":"memory"},"createdAt":"2026-08-26T10:00:00Z","updatedAt":"2026-08-26T10:00:00Z"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	ps, err := m.GetProjectSetting(context.Background(), "remember_config", "agent_name")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/settings/remember_config/agent_name" || gotMethod != http.MethodGet {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if ps == nil || ps.Category != "remember_config" || ps.Key != "agent_name" {
		t.Fatalf("setting = %+v", ps)
	}
	if v, ok := ps.Value["name"].(string); !ok || v != "memory" {
		t.Errorf("value = %v", ps.Value)
	}
}

func TestGetProjectSettingSessionProject(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s1","projectId":"sess-proj","category":"remember_config","key":"agent_name","value":{"name":"memory"}}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-token", "static-proj")
	ctx := withSessionContext(context.Background(), &sessionContext{Token: "sess-token", ProjectID: "sess-proj"})
	if _, err := m.GetProjectSetting(ctx, "remember_config", "agent_name"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/settings/remember_config/agent_name" {
		t.Errorf("path = %q, want /api/projects/sess-proj/settings/remember_config/agent_name", gotPath)
	}
}

func TestGetProjectSettingAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"setting not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	ps, err := m.GetProjectSetting(context.Background(), "entity_create", "similarity_threshold")
	if err != nil {
		t.Fatalf("absent setting must not error: %v", err)
	}
	if ps != nil {
		t.Fatalf("setting = %+v, want nil", ps)
	}
}

func TestGetProjectSettingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"memory down"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetProjectSetting(context.Background(), "remember_config", "agent_name"); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestSetProjectSetting(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s2","category":"entity_create","key":"similarity_threshold","value":{"value":0.8}}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.SetProjectSetting(context.Background(), "entity_create", "similarity_threshold", map[string]any{"value": 0.8}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/settings/entity_create/similarity_threshold" || gotMethod != http.MethodPut {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if v, ok := gotBody["value"].(float64); !ok || v != 0.8 {
		t.Errorf("body = %v, want raw value map", gotBody)
	}
}

func TestSetProjectSettingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"boom"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SetProjectSetting(context.Background(), "remember_config", "agent_name", map[string]any{"name": "memory"}); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestDeleteProjectSetting(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"deleted":true}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.DeleteProjectSetting(context.Background(), "remember_config", "agent_name"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/settings/remember_config/agent_name" || gotMethod != http.MethodDelete {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestDeleteProjectSettingAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"setting not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteProjectSetting(context.Background(), "remember_config", "agent_name"); err != nil {
		t.Fatalf("deleting an absent setting must not error: %v", err)
	}
}
