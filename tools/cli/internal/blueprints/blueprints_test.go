package blueprints_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/blueprints"
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

	_, packs, agents, _, _, _, results, err := blueprints.LoadDir(dir, nil)
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

	_, packs, agents, _, _, _, results, err := blueprints.LoadDir(dir, nil)
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

	_, packs, _, _, _, _, _, err := blueprints.LoadDir(dir, nil)
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

	_, packs, _, _, _, _, results, err := blueprints.LoadDir(dir, nil)
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

	_, packs, agents, _, _, _, results, err := blueprints.LoadDir(dir, nil)
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

	_, packs, _, _, _, _, results, err := blueprints.LoadDir(dir, nil)
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
// Seed loader tests
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadDir_SeedObjects_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "seed/objects/Document.jsonl", `{"type":"Document","key":"doc-1","properties":{"title":"Hello"}}
{"type":"Document","key":"doc-2"}
`)

	_, _, _, _, objects, rels, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results, got %d: %v", len(results), results)
	}
	if len(objects) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objects))
	}
	if objects[0].Type != "Document" || objects[0].Key != "doc-1" {
		t.Errorf("unexpected first object: %+v", objects[0])
	}
	if len(rels) != 0 {
		t.Errorf("expected 0 relationships, got %d", len(rels))
	}
}

func TestLoadDir_SeedRelationships_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "seed/relationships/Mentions.jsonl", `{"type":"Mentions","srcKey":"doc-1","dstKey":"doc-2"}
`)

	_, _, _, _, objects, rels, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results, got %d", len(results))
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(rels))
	}
	if rels[0].Type != "Mentions" || rels[0].SrcKey != "doc-1" || rels[0].DstKey != "doc-2" {
		t.Errorf("unexpected relationship: %+v", rels[0])
	}
	if len(objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(objects))
	}
}

func TestLoadDir_SeedMissingDir_NotError(t *testing.T) {
	dir := t.TempDir()
	// No seed/ directory at all

	_, _, _, _, objects, rels, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("expected no error for missing seed dir, got: %v", err)
	}
	if len(objects) != 0 || len(rels) != 0 {
		t.Errorf("expected empty seed results for missing dir")
	}
	if len(results) != 0 {
		t.Errorf("expected no error results, got %d", len(results))
	}
}

func TestLoadDir_SeedObjects_ParseError(t *testing.T) {
	dir := t.TempDir()
	// One valid line, one invalid JSON
	writeFile(t, dir, "seed/objects/Node.jsonl", `{"type":"Node","key":"n1"}
this is not json
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 valid object, got %d", len(objects))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 error result, got %d", len(results))
	}
	if results[0].Action != blueprints.BlueprintsActionError {
		t.Errorf("expected BlueprintsActionError, got %v", results[0].Action)
	}
}

func TestLoadDir_SeedObjects_ValidationError(t *testing.T) {
	dir := t.TempDir()
	// Object missing required 'type' field
	writeFile(t, dir, "seed/objects/Bad.jsonl", `{"key":"x","properties":{}}
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("expected 0 valid objects, got %d", len(objects))
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 error result, got %d", len(results))
	}
	if results[0].Action != blueprints.BlueprintsActionError {
		t.Errorf("expected BlueprintsActionError, got %v", results[0].Action)
	}
}

func TestLoadDir_SeedObjects_SplitFilesLoadedInOrder(t *testing.T) {
	dir := t.TempDir()
	// Split files: 001 comes before 002 lexicographically
	writeFile(t, dir, "seed/objects/Document.001.jsonl", `{"type":"Document","key":"first"}
`)
	writeFile(t, dir, "seed/objects/Document.002.jsonl", `{"type":"Document","key":"second"}
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results, got %d", len(results))
	}
	if len(objects) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objects))
	}
	if objects[0].Key != "first" {
		t.Errorf("expected first object key 'first', got %q", objects[0].Key)
	}
	if objects[1].Key != "second" {
		t.Errorf("expected second object key 'second', got %q", objects[1].Key)
	}
}

func TestLoadDir_SeedObjects_UnsupportedExtensionSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "seed/objects/readme.txt", "not a jsonl file")
	writeFile(t, dir, "seed/objects/Document.jsonl", `{"type":"Document","key":"d1"}
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 error results (txt skipped), got %d", len(results))
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 object (txt skipped), got %d", len(objects))
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
