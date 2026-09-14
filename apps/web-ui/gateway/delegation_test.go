package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}

// 1.3 JSON round-trip: config + delegation keys survive encode/decode with
// equal values; delegation is omitted when nil.
func TestDelegationJSONRoundTrip(t *testing.T) {
	def := AgentDefinition{
		Name:        "bob",
		Tools:       []string{"web"},
		Config:      map[string]any{"spawnPolicy": map[string]any{"allow": []string{"B"}}},
		Model:       &ModelConfig{Name: "deepseek-v4-flash"},
		Skills:      []string{"research"},
		BannedTools: []string{"rm"},
		Delegation:  &Delegation{Enabled: true, Targets: []string{"B", "C"}},
	}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	for _, key := range []string{`"config"`, `"delegation"`} {
		if !strings.Contains(raw, key) {
			t.Errorf("marshaled JSON missing %s: %s", key, raw)
		}
	}

	var back AgentDefinition
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Delegation, def.Delegation) {
		t.Errorf("Delegation round-trip mismatch: %+v != %+v", back.Delegation, def.Delegation)
	}
	// Config contains []any after decode; compare via re-marshal for equality.
	re, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(re) != raw {
		t.Errorf("re-marshaled JSON differs:\n got  %s\n want %s", string(re), raw)
	}

	// delegation omitted when nil.
	b2, err := json.Marshal(AgentDefinition{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), "delegation") {
		t.Errorf("delegation should be omitted when nil: %s", string(b2))
	}
}

// 2.3 enabled with targets: both tools added, spawnPolicy set, Delegation nil.
func TestApplyDelegationEnabled(t *testing.T) {
	def := &AgentDefinition{Tools: []string{"web"}}
	def.Delegation = &Delegation{Enabled: true, Targets: []string{"B", "C"}}
	if err := applyDelegation(def); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"spawn_agents", "list_available_agents"} {
		if !contains(def.Tools, tool) {
			t.Errorf("Tools missing %s: %v", tool, def.Tools)
		}
	}
	sp, ok := def.Config["spawnPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("Config[\"spawnPolicy\"] not set: %v", def.Config)
	}
	allow, ok := sp["allow"].([]string)
	if !ok || !reflect.DeepEqual(allow, []string{"B", "C"}) {
		t.Errorf("spawnPolicy.allow = %#v, want [B C]", sp["allow"])
	}
	if def.Delegation != nil {
		t.Error("Delegation should be nil after applyDelegation")
	}
}

// 2.3 enabled with empty targets: error.
func TestApplyDelegationEnabledEmptyTargets(t *testing.T) {
	def := &AgentDefinition{}
	def.Delegation = &Delegation{Enabled: true}
	err := applyDelegation(def)
	if err == nil {
		t.Fatal("want error for enabled with empty targets")
	}
	if err.Error() != "delegation.targets must not be empty when delegation is enabled" {
		t.Errorf("unexpected error: %v", err)
	}
}

// 2.3 disabled: delegation tools + spawnPolicy removed, everything else kept.
func TestApplyDelegationDisabled(t *testing.T) {
	def := &AgentDefinition{
		Tools: []string{"spawn_agents", "web", "list_available_agents", "spawn_agents"},
		Config: map[string]any{
			"spawnPolicy": map[string]any{"allow": []string{"B"}},
			"other":       "keep",
		},
		Delegation: &Delegation{Enabled: false, Targets: []string{"B"}},
	}
	if err := applyDelegation(def); err != nil {
		t.Fatal(err)
	}
	if contains(def.Tools, "spawn_agents") || contains(def.Tools, "list_available_agents") {
		t.Errorf("delegation tools not removed: %v", def.Tools)
	}
	if !reflect.DeepEqual(def.Tools, []string{"web"}) {
		t.Errorf("Tools = %v, want [web]", def.Tools)
	}
	if _, ok := def.Config["spawnPolicy"]; ok {
		t.Errorf("spawnPolicy not removed: %v", def.Config)
	}
	if def.Config["other"] != "keep" {
		t.Errorf("other config key not preserved: %v", def.Config)
	}
	if def.Delegation != nil {
		t.Error("Delegation should be nil after applyDelegation")
	}
}

// 2.3 nil Delegation: no-op.
func TestApplyDelegationNil(t *testing.T) {
	def := &AgentDefinition{
		Tools:  []string{"spawn_agents", "web"},
		Config: map[string]any{"spawnPolicy": map[string]any{"allow": []string{"B"}}},
	}
	if err := applyDelegation(def); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(def.Tools, []string{"spawn_agents", "web"}) {
		t.Errorf("Tools changed on nil Delegation: %v", def.Tools)
	}
	if _, ok := def.Config["spawnPolicy"]; !ok {
		t.Errorf("Config changed on nil Delegation: %v", def.Config)
	}
	if def.Delegation != nil {
		t.Error("Delegation should stay nil")
	}
}

// 2.3 pre-existing spawn_agents is not duplicated.
func TestApplyDelegationDedupes(t *testing.T) {
	def := &AgentDefinition{Tools: []string{"spawn_agents"}}
	def.Delegation = &Delegation{Enabled: true, Targets: []string{"B"}}
	if err := applyDelegation(def); err != nil {
		t.Fatal(err)
	}
	if len(def.Tools) != 2 || !contains(def.Tools, "list_available_agents") {
		t.Errorf("Tools = %v, want [spawn_agents list_available_agents]", def.Tools)
	}
}

// 3.2 create with delegation: tools + spawnPolicy persisted, no delegation leak.
func TestCreateAgentWithDelegation(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"name":"bob","delegation":{"enabled":true,"targets":["B"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.created) != 1 {
		t.Fatalf("create not forwarded: %+v", f.created)
	}
	got := f.created[0]
	if !contains(got.Tools, "spawn_agents") || !contains(got.Tools, "list_available_agents") {
		t.Errorf("delegation tools missing: %v", got.Tools)
	}
	sp, ok := got.Config["spawnPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("Config[\"spawnPolicy\"] not set: %v", got.Config)
	}
	if !reflect.DeepEqual(sp["allow"], []string{"B"}) {
		t.Errorf("spawnPolicy.allow = %#v, want [B]", sp["allow"])
	}
	if got.Delegation != nil {
		t.Error("Delegation must not be forwarded to memory")
	}
}

// 3.2 create without delegation: neither tool nor spawnPolicy added.
func TestCreateAgentWithoutDelegation(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"name":"bob","tools":["web"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := f.created[0]
	if contains(got.Tools, "spawn_agents") || contains(got.Tools, "list_available_agents") {
		t.Errorf("delegation tools added without delegation: %v", got.Tools)
	}
	if _, ok := got.Config["spawnPolicy"]; ok {
		t.Errorf("spawnPolicy added without delegation: %v", got.Config)
	}
}

// 2.2 enabled with empty targets rejected with 400.
func TestCreateAgentDelegationValidation(t *testing.T) {
	f := &fakeMemory{}
	_, e := newTestServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"name":"bob","delegation":{"enabled":true,"targets":[]}}`))
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

// 2.3 disabled with only delegation tools/config: strips to non-nil empty so
// the empty values serialize (as []/{}) and actually reach memory's partial
// update, removing the tools + spawnPolicy.
func TestApplyDelegationDisabledToEmpty(t *testing.T) {
	def := &AgentDefinition{
		Tools:      []string{"spawn_agents", "list_available_agents"},
		Config:     map[string]any{"spawnPolicy": map[string]any{"allow": []string{"B"}}},
		Delegation: &Delegation{Enabled: false, Targets: []string{"B"}},
	}
	if err := applyDelegation(def); err != nil {
		t.Fatal(err)
	}
	if def.Tools == nil || len(def.Tools) != 0 {
		t.Errorf("Tools should be non-nil empty, got %#v", def.Tools)
	}
	if def.Config == nil || len(def.Config) != 0 {
		t.Errorf("Config should be non-nil empty, got %#v", def.Config)
	}
	if def.Delegation != nil {
		t.Error("Delegation should be nil after applyDelegation")
	}
}

// Empty Tools/Config must serialize as []/{}, not be omitted, so memory's
// partial update can remove them.
func TestDelegationEmptySerialization(t *testing.T) {
	def := AgentDefinition{Name: "x", Tools: []string{}, Config: map[string]any{}}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if !strings.Contains(raw, `"tools":[]`) {
		t.Errorf("empty tools must serialize as [], got %s", raw)
	}
	if !strings.Contains(raw, `"config":{}`) {
		t.Errorf("empty config must serialize as {}, got %s", raw)
	}
}

// deriveDelegation reconstructs the Delegation field from the persisted
// spawn_agents tool + Config.spawnPolicy.allow (JSON-decoded []any shape).
func TestDeriveDelegation(t *testing.T) {
	def := &AgentDefinition{
		Tools:  []string{"web", "spawn_agents", "list_available_agents"},
		Config: map[string]any{"spawnPolicy": map[string]any{"allow": []any{"B", "C"}}},
	}
	deriveDelegation(def)
	if def.Delegation == nil || !def.Delegation.Enabled {
		t.Fatalf("expected enabled delegation, got %+v", def.Delegation)
	}
	if !reflect.DeepEqual(def.Delegation.Targets, []string{"B", "C"}) {
		t.Errorf("targets = %v, want [B C]", def.Delegation.Targets)
	}

	// Disabled: no spawn_agents tool, no spawnPolicy → Delegation stays nil.
	def2 := &AgentDefinition{Tools: []string{"web"}, Config: map[string]any{}}
	deriveDelegation(def2)
	if def2.Delegation != nil {
		t.Errorf("expected nil delegation, got %+v", def2.Delegation)
	}
}
