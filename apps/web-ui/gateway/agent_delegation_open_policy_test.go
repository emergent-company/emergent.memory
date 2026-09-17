package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// openPolicySettingsServer wires the six per-section POST handlers onto a bare
// echo instance backed by f, mirroring the harness in agent_ui_test.go so these
// regression tests don't depend on that file's (separately owned) helpers.
func openPolicySettingsServer(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/agents/:id/settings/general", s.uiAgentUpdateGeneral)
	e.POST("/agents/:id/settings/tools", s.uiAgentUpdateTools)
	e.POST("/agents/:id/settings/delegation", s.uiAgentUpdateDelegation)
	return e
}

func openPolicyPost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// An agent with spawn_agents whitelisted but no spawnPolicy.allow key is the
// "open policy" state: deriveDelegation yields Enabled=true with no targets.
// Saving an unrelated section must not 400 on that state.
func TestOpenPolicyAgentSurvivesGeneralSave(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{"a1": {
			ID:     "a1",
			Name:   "diane",
			Tools:  []string{"spawn_agents"},
			Config: map[string]any{"language": "Spanish"},
		}},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
	}
	e := openPolicySettingsServer(f)
	rec := openPolicyPost(e, "/agents/a1/settings/general", "name=diane")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?updated=1") || strings.Contains(loc, "err=") {
		t.Errorf("redirect = %q, want ?updated=1 (no err)", loc)
	}
	u := f.updatedAgent
	if u == nil {
		t.Fatal("UpdateAgentDefinition not called")
	}
	for _, tool := range []string{"spawn_agents", "list_available_agents"} {
		if !containsString(u.Tools, tool) {
			t.Errorf("open-policy tools missing %s: %v", tool, u.Tools)
		}
	}
	if _, ok := u.Config["spawnPolicy"]; ok {
		t.Errorf("open-policy save must not set spawnPolicy: %v", u.Config)
	}
}

// Saving the Tools section with a list that omits spawn_agents must still
// re-add the delegation tools, so the open policy isn't silently dropped.
func TestOpenPolicyAgentSurvivesToolsSave(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{"a1": {
			ID:    "a1",
			Name:  "diane",
			Tools: []string{"spawn_agents"},
		}},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
	}
	e := openPolicySettingsServer(f)
	rec := openPolicyPost(e, "/agents/a1/settings/tools", "tool=web_search")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?updated=1") || strings.Contains(loc, "err=") {
		t.Errorf("redirect = %q, want ?updated=1 (no err)", loc)
	}
	u := f.updatedAgent
	if u == nil {
		t.Fatal("UpdateAgentDefinition not called")
	}
	if !containsString(u.Tools, "web_search") {
		t.Errorf("submitted tool missing: %v", u.Tools)
	}
	for _, tool := range []string{"spawn_agents", "list_available_agents"} {
		if !containsString(u.Tools, tool) {
			t.Errorf("open-policy tools should be re-added on a tools save, missing %s: %v", tool, u.Tools)
		}
	}
}

// Submitting the delegation form without delegationEnabled still strips the
// delegation tools and spawnPolicy.
func TestDisabledDelegationStillStrips(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{"a1": {
			ID:     "a1",
			Name:   "diane",
			Tools:  []string{"spawn_agents", "list_available_agents", "web_search"},
			Config: map[string]any{"spawnPolicy": map[string]any{"allow": []any{"milo"}}},
		}},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
	}
	e := openPolicySettingsServer(f)
	rec := openPolicyPost(e, "/agents/a1/settings/delegation", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303, body=%s", rec.Code, rec.Body.String())
	}
	u := f.updatedAgent
	if u == nil {
		t.Fatal("UpdateAgentDefinition not called")
	}
	if containsString(u.Tools, "spawn_agents") || containsString(u.Tools, "list_available_agents") {
		t.Errorf("delegation tools should be stripped on disable: %v", u.Tools)
	}
	if _, ok := u.Config["spawnPolicy"]; ok {
		t.Errorf("spawnPolicy should be removed on disable: %v", u.Config)
	}
}

// The JSON API still rejects an explicit enabled delegation with zero targets
// (the guard in createAgent preserves the old 400 even though applyDelegation
// now treats empty targets as open policy).
func TestCreateAgentDelegationEmptyTargetsStill400(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"name":"x","delegation":{"enabled":true}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.created) != 0 {
		t.Errorf("invalid request must not reach memory backend")
	}
}
