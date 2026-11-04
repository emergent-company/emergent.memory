package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// modelMapServer is a fake install server tailored to the model-mapping flow.
// It returns a project provider list (raw JSON array, as ListProjectConfigs
// expects), an empty stored-overrides map, and records the PUT body so a test
// can prove the resolved agent→model choices are persisted per-project.
type modelMapServer struct {
	mu        sync.Mutex
	putBodies [][]byte
	created   []byte // last POST /agent-definitions body
}

func newModelMapServer(t *testing.T, providersJSON string) (*httptest.Server, *modelMapServer) {
	t.Helper()
	s := &modelMapServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/providers"):
			// GET /api/v1/projects/{id}/providers — raw JSON array.
			_, _ = io.WriteString(w, providersJSON)
		case r.Method == http.MethodGet && strings.Contains(path, "/api/blueprints/model-overrides"):
			_, _ = io.WriteString(w, `{"modelOverrides":{}}`)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/api/blueprints/model-overrides"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.putBodies = append(s.putBodies, body)
			s.mu.Unlock()
			_, _ = io.WriteString(w, `{"modelOverrides":{}}`)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/available"):
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/installed"):
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/agent-definitions"):
			_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/agent-definitions"):
			body, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.created = body
			s.mu.Unlock()
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"def-1","name":"operator"}}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
		default:
			t.Errorf("unexpected mutating request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *modelMapServer) putBody(t *testing.T, i int) []byte {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.putBodies) {
		t.Fatalf("expected at least %d PUT bodies, got %d", i+1, len(s.putBodies))
	}
	return s.putBodies[i]
}

func (s *modelMapServer) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.putBodies)
}

// writeMappingBlueprintDir writes a blueprint dir with a project.yaml carrying a
// name (so the override store is keyed deterministically) and one agent whose
// model is pinned to the supplied ref.
func writeMappingBlueprintDir(t *testing.T, agentModel string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte("name: operator\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatalf("write project.yaml: %v", err)
	}
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	agentYAML := "name: operator\ndescription: d\nmodel:\n  name: " + agentModel + "\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	return dir
}

// runInstallWithMapFlags drives the real runBlueprintsInstall with the supplied
// --map-model flags, isolating and restoring the package-level CLI state.
func runInstallWithMapFlags(t *testing.T, serverURL, source string, mapModel []string, noMap bool) (string, error) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_test_project_token")

	prevServer := viper.GetString("server")
	viper.Set("server", serverURL)
	t.Cleanup(func() { viper.Set("server", prevServer) })

	prevProject := blueprintsProjectFlag
	prevDryRun := blueprintsDryRunFlag
	prevUpgrade := blueprintsUpgradeFlag
	prevMapModel := blueprintsMapModelFlag
	prevNoMap := blueprintsNoMapFlag
	blueprintsProjectFlag = "00000000-0000-0000-0000-000000000001"
	blueprintsDryRunFlag = false
	blueprintsUpgradeFlag = false
	blueprintsMapModelFlag = mapModel
	blueprintsNoMapFlag = noMap
	t.Cleanup(func() {
		blueprintsProjectFlag = prevProject
		blueprintsDryRunFlag = prevDryRun
		blueprintsUpgradeFlag = prevUpgrade
		blueprintsMapModelFlag = prevMapModel
		blueprintsNoMapFlag = prevNoMap
	})

	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runBlueprintsInstall(cmd, []string{source})
	return out.String(), err
}

// TestRunBlueprintsInstall_MapModelPersistsOverrides proves a non-interactive
// --map-model install rewrites the agent's model, sends the mapped reference on
// the wire, and persists the resolved choices via PUT keyed by the blueprint's
// project name.
func TestRunBlueprintsInstall_MapModelPersistsOverrides(t *testing.T) {
	srv, fake := newModelMapServer(t, `[{"provider":"google","generativeModel":"gemini-2.5-flash"},{"provider":"deepseek","generativeModel":"deepseek-v4-flash"}]`)
	// The blueprint pins google, but the project operator overrides it to
	// deepseek — a distinct, servable choice that proves mapping (not the pin)
	// lands on the wire and in the PUT body.
	dir := writeMappingBlueprintDir(t, "google/gemini-2.5-flash")

	out, err := runInstallWithMapFlags(t, srv.URL, dir, []string{"operator=deepseek/deepseek-v4-flash"}, false)
	if err != nil {
		t.Fatalf("install failed: %v\noutput:\n%s", err, out)
	}

	// The applier must have sent the mapped model, not the blueprint pin.
	if len(fake.created) == 0 {
		t.Fatal("expected an agent-definitions create request")
	}
	var req struct {
		Model struct {
			Name string `json:"name"`
		} `json:"model"`
	}
	if err := json.Unmarshal(fake.created, &req); err != nil {
		t.Fatalf("decode create request: %v", err)
	}
	if req.Model.Name != "deepseek/deepseek-v4-flash" {
		t.Fatalf("expected mapped model on the wire, got %q", req.Model.Name)
	}

	// The resolved choices must be persisted under the blueprint's project name.
	if fake.putCount() != 1 {
		t.Fatalf("expected exactly one PUT /model-overrides, got %d", fake.putCount())
	}
	var put struct {
		Name           string            `json:"name"`
		ModelOverrides map[string]string `json:"modelOverrides"`
	}
	if err := json.Unmarshal(fake.putBody(t, 0), &put); err != nil {
		t.Fatalf("decode PUT body: %v", err)
	}
	if put.Name != "operator" {
		t.Fatalf("expected PUT name %q (from project.yaml), got %q", "operator", put.Name)
	}
	if got := put.ModelOverrides["operator"]; got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("expected override operator=deepseek/deepseek-v4-flash, got %q (full map: %v)", got, put.ModelOverrides)
	}
}

// TestRunBlueprintsInstall_MapModelBadModelErrors proves a malformed --map-model
// model aborts before the applier runs (no agent-definitions create, no PUT).
func TestRunBlueprintsInstall_MapModelBadModelErrors(t *testing.T) {
	srv, fake := newModelMapServer(t, `[{"provider":"google","generativeModel":"gemini-2.5-flash"}]`)
	dir := writeMappingBlueprintDir(t, "google/gemini-2.5-flash")

	_, err := runInstallWithMapFlags(t, srv.URL, dir, []string{"operator=baremodel"}, false)
	if err == nil {
		t.Fatal("expected a malformed --map-model to fail, got nil")
	}
	if !strings.Contains(err.Error(), "--map-model") && !strings.Contains(err.Error(), "map-model") {
		t.Fatalf("expected an error mentioning --map-model, got: %v", err)
	}
	if len(fake.created) != 0 {
		t.Fatal("expected no agent-definitions create for a malformed --map-model")
	}
	if fake.putCount() != 0 {
		t.Fatalf("expected no PUT for a malformed --map-model, got %d", fake.putCount())
	}
}

// TestRunBlueprintsInstall_NoMapSkipsMapping proves --no-map applies the
// blueprint's pinned model verbatim and persists nothing.
func TestRunBlueprintsInstall_NoMapSkipsMapping(t *testing.T) {
	srv, fake := newModelMapServer(t, `[{"provider":"google","generativeModel":"gemini-2.5-flash"}]`)
	dir := writeMappingBlueprintDir(t, "google/gemini-2.5-flash")

	out, err := runInstallWithMapFlags(t, srv.URL, dir, nil, true)
	if err != nil {
		t.Fatalf("install failed: %v\noutput:\n%s", err, out)
	}

	if len(fake.created) == 0 {
		t.Fatal("expected an agent-definitions create request")
	}
	var req struct {
		Model struct {
			Name string `json:"name"`
		} `json:"model"`
	}
	if err := json.Unmarshal(fake.created, &req); err != nil {
		t.Fatalf("decode create request: %v", err)
	}
	if req.Model.Name != "google/gemini-2.5-flash" {
		t.Fatalf("expected the pinned model on the wire with --no-map, got %q", req.Model.Name)
	}
	if fake.putCount() != 0 {
		t.Fatalf("expected no PUT with --no-map, got %d", fake.putCount())
	}
}
