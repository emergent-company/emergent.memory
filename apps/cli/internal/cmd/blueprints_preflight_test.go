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
	mu           sync.Mutex
	total        int
	mutating     int
	compiledHits int
	compiledBody string // GET .../compiled-types response; empty means no object types
	createdRaw   []byte // last POST body to an agent-definitions endpoint
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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/compiled-types"):
			s.mu.Lock()
			s.compiledHits++
			body := s.compiledBody
			s.mu.Unlock()
			if body == "" {
				body = `{"objectTypes":[]}`
			}
			_, _ = w.Write([]byte(body))
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

func (s *cmdPreflightServer) compiledTypesHits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compiledHits
}

// setCompiledTypes sets the body the fake returns for GET .../compiled-types,
// so a test can give the project the object and relationship types it needs.
func (s *cmdPreflightServer) setCompiledTypes(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compiledBody = body
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

// writePackBlueprintDir writes a blueprint directory whose only resource is a
// single pack file under schemas/ with the supplied YAML content.
func writePackBlueprintDir(t *testing.T, packYAML string) string {
	t.Helper()
	dir := t.TempDir()
	schemasDir := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(schemasDir, 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}
	if err := os.WriteFile(filepath.Join(schemasDir, "pack.yaml"), []byte(packYAML), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}
	return dir
}

// runRealInstall drives the actual runBlueprintsInstall guard against the fake
// server. It isolates the process-global CLI state (config discovery, viper
// server override, package-level blueprint flags) and restores it afterwards.
func runRealInstall(t *testing.T, serverURL, source string) (string, error) {
	t.Helper()
	return runRealInstallMode(t, serverURL, source, false)
}

// runRealInstallMode is runRealInstall with --dry-run selectable. A dry-run
// still runs the whole preflight — including its read of the project's compiled
// types — but issues no mutating request, so a test can assert on the warnings
// the gate prints without standing up the apply endpoints.
func runRealInstallMode(t *testing.T, serverURL, source string, dryRun bool) (string, error) {
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
	blueprintsDryRunFlag = dryRun
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

// TestRunBlueprintsInstall_ConsultsProjectTypesForUnknownEndpoint proves the
// install preflight widens its relationship-endpoint check to the project when a
// blueprint endpoint is not defined by any of its packs (#1853): it issues a
// read-only compiled-types request, and when the project does not own the type
// it aborts before any mutation.
func TestRunBlueprintsInstall_ConsultsProjectTypesForUnknownEndpoint(t *testing.T) {
	srv, fake := newCmdPreflightServer(t)
	dir := writePackBlueprintDir(t, `name: board
version: "1.0.0"
objectTypes:
  - name: Task
    label: Task
relationshipTypes:
  - name: mentions
    sourceType: Task
    targetType: Message
`)

	out, err := runRealInstall(t, srv.URL, dir)
	if err == nil {
		t.Fatalf("expected install to abort for an endpoint the project does not own, got nil (output: %s)", out)
	}

	if _, mutating := fake.counts(); mutating != 0 {
		t.Fatalf("expected zero mutating requests before abort, got %d", mutating)
	}
	if fake.compiledTypesHits() == 0 {
		t.Fatal("expected the preflight to consult the project's compiled types")
	}
}

// TestRunBlueprintsInstall_PassesProjectRelationshipTypesToViewCheck pins #667.6
// at the command level, the layer the blueprint validator cannot see: a ui.view
// section may present a relationship type a companion pack already installed
// owns, so the install gate must (a) read the project's compiled types when the
// blueprint's own packs cannot answer for a view relation and (b) hand the
// project's relationship types to the validator.
//
// Mutation checks: dropping `|| HasUnknownViewRelationshipTypes(packs)` from the
// preflight condition stops the read (compiledTypesHits == 0, and the second
// subtest's "or the project" wording disappears); dropping the
// compiled.RelationshipTypes loop in projectKnownTypes leaves the known set
// blueprint-only, so the project-owned relation is reported as unknown again.
func TestRunBlueprintsInstall_PassesProjectRelationshipTypesToViewCheck(t *testing.T) {
	dir := writePackBlueprintDir(t, `name: board
version: "1.0.0"
objectTypes:
  - name: Task
    label: Task
    ui:
      view:
        sections:
          - title: Services
            relation: serves
relationshipTypes:
  - name: fixes
    sourceType: Task
    targetType: Task
`)

	t.Run("a relationship type the project owns satisfies the view", func(t *testing.T) {
		srv, fake := newCmdPreflightServer(t)
		fake.setCompiledTypes(`{"objectTypes":[{"name":"Task"}],"relationshipTypes":[{"name":"serves"}]}`)

		out, err := runRealInstallMode(t, srv.URL, dir, true)
		if err != nil {
			t.Fatalf("expected a view naming a project-owned relationship type to install, got: %v\noutput:\n%s", err, out)
		}
		if fake.compiledTypesHits() == 0 {
			t.Fatal("expected the preflight to consult the project's compiled types for a view relation")
		}
		if _, mutating := fake.counts(); mutating != 0 {
			t.Fatalf("expected a dry-run install to mutate nothing, got %d mutating requests", mutating)
		}
		if strings.Contains(out, "is not a relationshipType") {
			t.Fatalf("a project-owned relationship type must satisfy the view check, got:\n%s", out)
		}
	})

	t.Run("one neither the blueprint nor the project owns is still reported", func(t *testing.T) {
		srv, fake := newCmdPreflightServer(t)
		fake.setCompiledTypes(`{"objectTypes":[{"name":"Task"}],"relationshipTypes":[{"name":"mentions"}]}`)

		out, err := runRealInstallMode(t, srv.URL, dir, true)
		if err != nil {
			t.Fatalf("expected dry-run install to succeed, got: %v\noutput:\n%s", err, out)
		}
		// The "or the project" scope is the proof the gate had the project's
		// relationship types in hand rather than falling back to blueprint-only.
		if !strings.Contains(out, `relation "serves" is not a relationshipType declared by this blueprint or the project`) {
			t.Fatalf("expected the gate to report the unknown relation against the project too, got:\n%s", out)
		}
	})
}
