package blueprints

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkruntime "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// noopAuthRuntime satisfies the SDK auth.Provider interface for tests.
type noopAuthRuntime struct{}

func (noopAuthRuntime) Authenticate(r *http.Request) error { return nil }
func (noopAuthRuntime) Refresh(ctx context.Context) error  { return nil }

func TestAgentFileToCreateRequest_MapsWorkConfig(t *testing.T) {
	ag := AgentFile{
		Name: "board-agent",
		WorkConfig: map[string]any{
			"status":         map[string]any{"ready": "todo"},
			"requiresReview": true,
		},
	}
	req := agentFileToCreateRequest(ag)
	if req.WorkConfig == nil {
		t.Fatal("expected workConfig to be mapped on create")
	}
	var wc map[string]any
	if err := json.Unmarshal(req.WorkConfig, &wc); err != nil {
		t.Fatalf("unmarshal workConfig: %v", err)
	}
	if wc["requiresReview"] != true {
		t.Fatalf("expected requiresReview=true, got %v", wc["requiresReview"])
	}
	if _, ok := wc["status"]; !ok {
		t.Fatalf("expected nested status block, got %v", wc)
	}
}

func TestAgentFileToUpdateRequest_MapsWorkConfig(t *testing.T) {
	ag := AgentFile{Name: "board-agent", WorkConfig: map[string]any{"failureLimit": float64(5)}}
	req := agentFileToUpdateRequest(ag)
	if req.WorkConfig == nil {
		t.Fatal("expected workConfig to be mapped on update")
	}
	var wc map[string]any
	if err := json.Unmarshal(req.WorkConfig, &wc); err != nil {
		t.Fatalf("unmarshal workConfig: %v", err)
	}
	if wc["failureLimit"] != float64(5) {
		t.Fatalf("expected failureLimit=5, got %v", wc["failureLimit"])
	}
}

func TestAgentFileToCreateRequest_NoWorkConfigIsNil(t *testing.T) {
	req := agentFileToCreateRequest(AgentFile{Name: "plain"})
	if req.WorkConfig != nil {
		t.Fatalf("expected nil workConfig when absent, got %s", req.WorkConfig)
	}
}

// newRuntimeAgentBlueprinter builds a Blueprinter wired to a fake runtime-agent
// server so ensureRuntimeAgent can be exercised end-to-end.
func newRuntimeAgentBlueprinter(serverURL string) *Blueprinter {
	return &Blueprinter{
		projectID:     "proj-1",
		runtimeAgents: sdkruntime.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
	}
}

func TestEnsureRuntimeAgent_Create(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/proj-1/agents":
			// No existing runtime agent.
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/proj-1/agents":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"ra-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newRuntimeAgentBlueprinter(srv.URL)
	ag := AgentFile{
		Name:           "board-agent",
		ReactionConfig: &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []string{"created"}},
	}
	if err := b.ensureRuntimeAgent(context.Background(), ag, "def-1"); err != nil {
		t.Fatalf("ensureRuntimeAgent: %v", err)
	}
	if createBody == nil {
		t.Fatal("expected a create call")
	}
	if createBody["name"] != "board-agent" {
		t.Fatalf("expected name=board-agent, got %v", createBody["name"])
	}
	if createBody["strategyType"] != "agent-def:def-1" {
		t.Fatalf("expected strategyType agent-def:def-1, got %v", createBody["strategyType"])
	}
	if createBody["triggerType"] != "reaction" {
		t.Fatalf("expected triggerType reaction (defaulted from reactionConfig), got %v", createBody["triggerType"])
	}
	if createBody["cronSchedule"] != "0 0 * * *" {
		t.Fatalf("expected default cronSchedule, got %v", createBody["cronSchedule"])
	}
	if createBody["agentDefinitionId"] != "def-1" {
		t.Fatalf("expected agentDefinitionId def-1, got %v", createBody["agentDefinitionId"])
	}
	rc, ok := createBody["reactionConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected reactionConfig object, got %v", createBody["reactionConfig"])
	}
	if len(rc["objectTypes"].([]any)) != 1 || rc["objectTypes"].([]any)[0] != "Task" {
		t.Fatalf("expected reactionConfig.objectTypes [Task], got %v", rc["objectTypes"])
	}
	cfg, ok := createBody["config"].(map[string]any)
	if !ok || cfg["source"] != "blueprints-cli" {
		t.Fatalf("expected config source stamp, got %v", createBody["config"])
	}
}

func TestEnsureRuntimeAgent_Update(t *testing.T) {
	var updateBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/proj-1/agents":
			// Existing runtime agent owned by this CLI (source stamp + matching
			// strategy type), so the ownership gate admits it for update.
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"ra-1","name":"board-agent","config":{"source":"blueprints-cli"},"strategyType":"agent-def:def-2"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/projects/proj-1/agents/ra-1":
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"ra-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newRuntimeAgentBlueprinter(srv.URL)
	ag := AgentFile{
		Name:           "board-agent",
		TriggerType:    "reaction",
		CronSchedule:   "0 8 * * *",
		ReactionConfig: &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []string{"updated"}},
	}
	if err := b.ensureRuntimeAgent(context.Background(), ag, "def-2"); err != nil {
		t.Fatalf("ensureRuntimeAgent: %v", err)
	}
	if updateBody == nil {
		t.Fatal("expected an update call")
	}
	if updateBody["agentDefinitionId"] != "def-2" {
		t.Fatalf("expected agentDefinitionId def-2 on update, got %v", updateBody["agentDefinitionId"])
	}
	if updateBody["triggerType"] != "reaction" {
		t.Fatalf("expected triggerType reaction, got %v", updateBody["triggerType"])
	}
	if updateBody["cronSchedule"] != "0 8 * * *" {
		t.Fatalf("expected cronSchedule 0 8 * * *, got %v", updateBody["cronSchedule"])
	}
	// M2: Config must be omitted on update so the server does not wipe existing keys.
	if _, ok := updateBody["config"]; ok {
		t.Fatalf("update must omit config to preserve existing keys, got %v", updateBody["config"])
	}
}

// TestEnsureRuntimeAgent_ForeignAgentNoHijack verifies a pre-existing runtime
// agent that is not stamped "blueprints-cli" is left untouched — no PATCH
// request is issued — even when its strategy type happens to match the target
// definition (the ownership stamp is the sole admission criterion).
func TestEnsureRuntimeAgent_ForeignAgentNoHijack(t *testing.T) {
	var patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/proj-1/agents":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"ra-1","name":"board-agent","config":{"source":"other"},"strategyType":"agent-def:def-3"}]}`))
		case r.Method == http.MethodPatch:
			patched = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"ra-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newRuntimeAgentBlueprinter(srv.URL)
	ag := AgentFile{
		Name:           "board-agent",
		TriggerType:    "reaction",
		ReactionConfig: &ReactionConfig{ObjectTypes: []string{"Task"}},
	}
	if err := b.ensureRuntimeAgent(context.Background(), ag, "def-3"); err != nil {
		t.Fatalf("ensureRuntimeAgent: %v", err)
	}
	if patched {
		t.Fatal("foreign runtime agent must not be repurposed")
	}
}

func TestEnsureRuntimeAgent_NoTriggerConfigIsNoOp(t *testing.T) {
	b := newRuntimeAgentBlueprinter("http://127.0.0.1:1") // unreachable — must not be called
	if err := b.ensureRuntimeAgent(context.Background(), AgentFile{Name: "plain"}, "def-1"); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

// TestBlueprintAgent_SkipPathReconcilesRuntimeAgent verifies that when the
// definition already exists and upgrade is off, blueprintAgent still calls
// ensureRuntimeAgent so a retry after a definition-created/runtime-failed run
// recovers — the runtime agent gets created, but the result stays skipped.
func TestBlueprintAgent_SkipPathReconcilesRuntimeAgent(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/proj-1/agents":
			// No existing runtime agent.
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/proj-1/agents":
			created = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"ra-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newRuntimeAgentBlueprinter(srv.URL)
	ag := AgentFile{
		Name:           "board-agent",
		ReactionConfig: &ReactionConfig{ObjectTypes: []string{"Task"}},
	}

	res := b.blueprintAgent(context.Background(), ag, map[string]sdkagents.AgentDefinitionSummary{
		"board-agent": {ID: "def-1", Name: "board-agent"},
	})

	if res.Action != BlueprintsActionSkipped {
		t.Fatalf("expected skipped result, got %v (err=%v)", res.Action, res.Error)
	}
	if !created {
		t.Fatal("expected runtime agent to be created on the skip path")
	}
}
