package cmd

import (
	"bytes"
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

// cmdPreflightServer records every request the real install command issues and
// distinguishes mutating verbs from reads, so a test can prove a failed
// preflight mutated nothing and a valid blueprint did.
type cmdPreflightServer struct {
	mu         sync.Mutex
	total      int
	mutating   int
	createdRaw []byte // last POST body to an agent-definitions endpoint
}

func newCmdPreflightServer(t *testing.T) (*httptest.Server, *cmdPreflightServer) {
	t.Helper()
	s := &cmdPreflightServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.total++
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			s.mutating++
		}
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
			s.createdRaw = body
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"def-1","name":"structured"}}`))
		case r.Method == http.MethodGet:
			// Any other read: empty successful list. Unknown writes are a bug.
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			t.Errorf("unexpected mutating request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *cmdPreflightServer) counts() (total, mutating int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total, s.mutating
}

func (s *cmdPreflightServer) createdBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createdRaw
}

// writeBlueprintDir writes a minimal blueprint directory whose only resource is
// a single agent file with the supplied YAML content.
func writeBlueprintDir(t *testing.T, agentYAML string) string {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return dir
}

// runRealInstall drives the actual runBlueprintsInstall guard against the fake
// server. It isolates the process-global CLI state (config discovery, viper
// server override, package-level blueprint flags) and restores it afterwards.
func runRealInstall(t *testing.T, serverURL, source string) (string, error) {
	t.Helper()

	// Keep config discovery out of any real ~/.memory/config.yaml.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", serverURL)
	// A project token makes resolveProjectContext skip the projects:read call
	// (and the interactive picker), so before-preflight traffic stays at zero.
	t.Setenv("MEMORY_PROJECT_API_KEY", "emt_test_project_token")

	// getClient() overrides the config server URL from the global viper key.
	prevServer := viper.GetString("server")
	viper.Set("server", serverURL)
	t.Cleanup(func() { viper.Set("server", prevServer) })

	prevProject := blueprintsProjectFlag
	prevDryRun := blueprintsDryRunFlag
	prevUpgrade := blueprintsUpgradeFlag
	blueprintsProjectFlag = "00000000-0000-0000-0000-000000000001"
	blueprintsDryRunFlag = false
	blueprintsUpgradeFlag = false
	t.Cleanup(func() {
		blueprintsProjectFlag = prevProject
		blueprintsDryRunFlag = prevDryRun
		blueprintsUpgradeFlag = prevUpgrade
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runBlueprintsInstall(cmd, []string{source})
	return out.String(), err
}

// TestRunBlueprintsInstall_BadModelAbortsBeforeAnyApply is the cmd-level
// regression guard for #1586. Unlike a re-implemented guard, it drives the real
// runBlueprintsInstall function and proves a malformed agent model aborts with
// an error and issues ZERO mutating requests (nothing partially applied).
//
// Mutation check: flipping the real `if report.HasErrors()` guard to `false`
// lets the applier run, generates mutating requests, and fails this test.
func TestRunBlueprintsInstall_BadModelAbortsBeforeAnyApply(t *testing.T) {
	srv, fake := newCmdPreflightServer(t)
	dir := writeBlueprintDir(t, "name: bad-model\ndescription: d\nmodel:\n  name: gemini-2.5-flash\n")

	out, err := runRealInstall(t, srv.URL, dir)
	if err == nil {
		t.Fatalf("expected install to fail for a bare model name, got nil (output: %s)", out)
	}

	total, mutating := fake.counts()
	if mutating != 0 {
		t.Fatalf("expected zero mutating requests before abort, got %d (of %d total) — partial apply", mutating, total)
	}
	if total != 0 {
		t.Fatalf("expected zero API requests at all for a preflight failure, got %d", total)
	}
}

// TestRunBlueprintsInstall_ValidStructuredModelApplies proves the real install
// path still applies a valid blueprint, folding a structured {name, provider}
// model into the canonical "provider/model" name with a cleared provider on the
// wire.
func TestRunBlueprintsInstall_ValidStructuredModelApplies(t *testing.T) {
	srv, fake := newCmdPreflightServer(t)
	dir := writeBlueprintDir(t, "name: structured\ndescription: d\nmodel:\n  name: gpt-4o\n  provider: openai\n")

	out, err := runRealInstall(t, srv.URL, dir)
	if err != nil {
		t.Fatalf("expected valid structured model to install, got: %v\noutput:\n%s", err, out)
	}

	if _, mutating := fake.counts(); mutating == 0 {
		t.Fatal("expected the valid blueprint to be applied with a mutating request")
	}

	body := fake.createdBody()
	if len(body) == 0 {
		t.Fatal("expected an agent-definitions create request")
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
