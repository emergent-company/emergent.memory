package blueprints

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdkagents "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	sdkruntime "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
)

// preflightTestServer records every request so a test can prove that a failed
// preflight made zero API calls (no partial apply). GETs return empty lists and
// POST /agent-definitions echoes a created definition, capturing the request
// body for model assertions.
type preflightTestServer struct {
	mu      sync.Mutex
	reqs    int
	created []byte // last POST /agent-definitions body
}

func newPreflightTestServer(t *testing.T) (*httptest.Server, *preflightTestServer) {
	t.Helper()
	s := &preflightTestServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs++
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/available"):
			// schemas.GetAvailablePacks decodes a raw array.
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installed"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agent-definitions"):
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/agent-definitions"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.created = body
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"def-1","name":"structured"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *preflightTestServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs
}

func (s *preflightTestServer) createdAgent() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created
}

// newPreflightBlueprinter wires a Blueprinter to the fake server. projects is
// nil because these tests never supply a project file.
func newPreflightBlueprinter(serverURL string) *Blueprinter {
	return NewBlueprintsApplier(
		nil,
		"proj-1",
		sdkschemas.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		sdkagents.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		sdkruntime.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		sdkskills.NewClient(http.DefaultClient, serverURL, noopAuthRuntime{}, "", "proj-1"),
		false, // dryRun
		false, // upgrade
		io.Discard,
	)
}

// TestInstallPreflight_BadModelAbortsBeforeAnyApply is the regression guard for
// #1586: a blueprint whose agent model is not in "provider/model" form must be
// rejected by the preflight BEFORE the applier runs, so it makes zero API calls
// and applies nothing (previously packs were materialized before the agent
// create failed server-side).
//
// Mutation test: if Preflight stopped rejecting the bare model, the install
// guard below would run the applier and requestCount would be non-zero.
func TestInstallPreflight_BadModelAbortsBeforeAnyApply(t *testing.T) {
	srv, fake := newPreflightTestServer(t)
	b := newPreflightBlueprinter(srv.URL)

	agents := []AgentFile{
		{Name: "bad-model", Description: "d", SourceFile: "agents/bad-model.yaml",
			Model: &AgentModel{Name: "gemini-2.5-flash"}},
	}

	report := Preflight(nil, nil, agents, nil, nil, nil, nil)

	// This mirrors runBlueprintsInstall: abort on preflight errors and never
	// reach the applier.
	if report.HasErrors() {
		// expected for a bare model name
	} else {
		if _, err := b.Run(context.Background(), nil, nil, agents, nil); err != nil {
			t.Fatalf("applier run: %v", err)
		}
		t.Fatal("expected preflight to reject a bare model name")
	}

	if got := fake.requestCount(); got != 0 {
		t.Fatalf("expected zero API requests when the model is invalid, got %d (partial apply)", got)
	}
	if len(fake.createdAgent()) != 0 {
		t.Fatal("no agent should have been created")
	}
}

// TestInstallPreflight_ValidStructuredModelNormalizesAndApplies proves a valid
// blueprint still installs as before, and that a structured {name, provider}
// model is folded into the canonical "provider/model" name the server requires
// before it reaches the wire.
func TestInstallPreflight_ValidStructuredModelNormalizesAndApplies(t *testing.T) {
	srv, fake := newPreflightTestServer(t)
	b := newPreflightBlueprinter(srv.URL)

	agents := []AgentFile{
		{Name: "structured", Description: "d", SourceFile: "agents/structured.yaml",
			Model: &AgentModel{Name: "gpt-4o", Provider: "openai"}},
	}

	report := Preflight(nil, nil, agents, nil, nil, nil, nil)
	if report.HasErrors() {
		t.Fatalf("expected valid structured model to pass preflight, got: %v", report.Errors())
	}
	if agents[0].Model.Name != "openai/gpt-4o" || agents[0].Model.Provider != "" {
		t.Fatalf("preflight did not canonicalize model: Name=%q Provider=%q", agents[0].Model.Name, agents[0].Model.Provider)
	}

	results, err := b.Run(context.Background(), nil, nil, agents, nil)
	if err != nil {
		t.Fatalf("applier run: %v", err)
	}
	for _, r := range results {
		if r.Action == BlueprintsActionError {
			t.Fatalf("unexpected apply error for %s: %v", r.Name, r.Error)
		}
	}

	body := fake.createdAgent()
	if len(body) == 0 {
		t.Fatal("expected the agent to be created")
	}

	var req struct {
		Model struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
		} `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode create request: %v", err)
	}
	if req.Model.Name != "openai/gpt-4o" {
		t.Fatalf("expected canonical model name on the wire, got %q", req.Model.Name)
	}
	if req.Model.Provider != "" {
		t.Fatalf("expected provider cleared on the wire, got %q", req.Model.Provider)
	}
}

// TestInstallPreflight_CrossPackRelationshipTarget is the regression guard for
// #1853: a multi-pack blueprint whose relationship endpoint is owned by a
// sibling pack must pass the install preflight (the same-pack rule previously
// rejected it), while an endpoint that exists in no pack still aborts.
func TestInstallPreflight_CrossPackRelationshipTarget(t *testing.T) {
	packs := []PackFile{
		{
			Name:        "core",
			Version:     "1.0.0",
			SourceFile:  "schemas/core.yaml",
			ObjectTypes: []ObjectTypeDef{{Name: "Task", Label: "Task"}},
			RelationshipTypes: []RelationshipTypeDef{
				{Name: "delivers", SourceType: "Task", TargetType: "Note"},
			},
		},
		{
			Name:        "notes",
			Version:     "1.0.0",
			SourceFile:  "schemas/notes.yaml",
			ObjectTypes: []ObjectTypeDef{{Name: "Note", Label: "Note"}},
		},
	}

	if report := Preflight(nil, packs, nil, nil, nil, nil, nil); report.HasErrors() {
		t.Fatalf("expected a cross-pack relationship target to pass the install preflight, got: %v", report.Errors())
	}

	// Without the sibling pack the target exists in no pack and must abort.
	report := Preflight(nil, packs[:1], nil, nil, nil, nil, nil)
	if !report.HasErrors() {
		t.Fatal("expected an unknown relationship target to fail the install preflight")
	}
}

// TestInstallPreflight_ProjectOwnedEndpoint verifies the install gate accepts a
// relationship endpoint owned by the project — an installed pack or a built-in
// type — even when no pack in the blueprint defines it (#1853), while an endpoint
// unknown to both the blueprint and the project still aborts.
func TestInstallPreflight_ProjectOwnedEndpoint(t *testing.T) {
	packs := []PackFile{
		{
			Name:        "board",
			Version:     "1.0.0",
			SourceFile:  "schemas/board.yaml",
			ObjectTypes: []ObjectTypeDef{{Name: "Task", Label: "Task"}},
			RelationshipTypes: []RelationshipTypeDef{
				{Name: "mentions", SourceType: "Task", TargetType: "Message"},
			},
		},
	}

	if report := PreflightWithProjectTypes([]string{"Message"}, nil, packs, nil, nil, nil, nil, nil); report.HasErrors() {
		t.Fatalf("expected a project-owned endpoint to pass the install preflight, got: %v", report.Errors())
	}

	if report := PreflightWithProjectTypes([]string{"Other"}, nil, packs, nil, nil, nil, nil, nil); !report.HasErrors() {
		t.Fatal("expected an endpoint unknown to the blueprint and the project to fail the install preflight")
	}

	// With no project types (read failed / offline), the endpoint owned only by
	// the project reverts to an error — the blueprint-only union.
	if report := PreflightWithProjectTypes(nil, nil, packs, nil, nil, nil, nil, nil); !report.HasErrors() {
		t.Fatal("expected the blueprint-only union to reject a project-owned endpoint")
	}
}
