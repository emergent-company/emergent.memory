package blueprints_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/tools/emergent-cli/internal/blueprints"
)

// ──────────────────────────────────────────────────────────────────────────────
// LoadDir tests
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadDir_ValidYAMLPack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "packs/core.yaml", `
name: "core-pack"
version: "1.0.0"
objectTypes:
  - name: "Document"
    label: "Document"
`)

	packs, agents, results, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(packs))
	}
	if packs[0].Name != "core-pack" {
		t.Errorf("expected name 'core-pack', got %q", packs[0].Name)
	}
	if packs[0].Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", packs[0].Version)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results, got %d", len(results))
	}
}

func TestLoadDir_ValidJSONAgent(t *testing.T) {
	dir := t.TempDir()
	agent := map[string]any{
		"name":        "my-agent",
		"description": "A test agent",
		"flowType":    "agentic",
	}
	data, _ := json.Marshal(agent)
	writeFile(t, dir, "agents/my-agent.json", string(data))

	packs, agents, results, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "my-agent" {
		t.Errorf("expected name 'my-agent', got %q", agents[0].Name)
	}
	if len(packs) != 0 {
		t.Errorf("expected 0 packs, got %d", len(packs))
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results, got %d", len(results))
	}
}

func TestLoadDir_UnknownExtensionSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "packs/readme.txt", "this is not a pack file")
	writeFile(t, dir, "packs/core.yaml", `
name: real-pack
version: "1.0.0"
objectTypes:
  - name: Doc
`)

	packs, _, _, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 pack (txt skipped), got %d", len(packs))
	}
}

func TestLoadDir_MissingRequiredField_PackSkippedWithError(t *testing.T) {
	dir := t.TempDir()
	// Pack missing required 'name' field
	writeFile(t, dir, "packs/bad.yaml", `
version: "1.0.0"
objectTypes:
  - name: Doc
`)

	packs, _, results, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 0 {
		t.Errorf("expected 0 valid packs, got %d", len(packs))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 error result, got %d", len(results))
	}
	if results[0].Action != blueprints.BlueprintsActionError {
		t.Errorf("expected BlueprintsActionError, got %v", results[0].Action)
	}
}

func TestLoadDir_MissingSubdirsNotError(t *testing.T) {
	dir := t.TempDir()
	// No packs/ or agents/ subdirectory at all

	packs, agents, results, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("expected no error when subdirs are missing, got: %v", err)
	}
	if len(packs) != 0 || len(agents) != 0 || len(results) != 0 {
		t.Errorf("expected empty results for empty dir")
	}
}

func TestLoadDir_ParseError_RecordedAndContinues(t *testing.T) {
	dir := t.TempDir()
	// Invalid YAML
	writeFile(t, dir, "packs/broken.yaml", `{ this is not valid yaml: [`)
	// Valid pack after the broken one
	writeFile(t, dir, "packs/good.yaml", `
name: good-pack
version: "1.0.0"
objectTypes:
  - name: Node
`)

	packs, _, results, err := blueprints.LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 valid pack, got %d", len(packs))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 error result for broken file, got %d", len(results))
	}
	if results[0].Action != blueprints.BlueprintsActionError {
		t.Errorf("expected BlueprintsActionError, got %v", results[0].Action)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GitHub URL parsing tests (no network calls)
// ──────────────────────────────────────────────────────────────────────────────

func TestIsGitHubURL(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"https://github.com/acme/repo", true},
		{"https://github.com/acme/repo#main", true},
		{"./local/dir", false},
		{"/abs/path", false},
		{"https://gitlab.com/acme/repo", false},
	}
	for _, tc := range cases {
		got := blueprints.IsGitHubURL(tc.src)
		if got != tc.want {
			t.Errorf("IsGitHubURL(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// writeFile creates a file at <base>/<relPath> (creating parent dirs).
func writeFile(t *testing.T, base, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
