package blueprints_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestLoadDir_YMLLiteralUIConfigs proves a real YAML pack with a native
// uiConfigs mapping loads (json.RawMessage could not decode !!map via yaml.v3)
// and that both raw blobs are JSON-decodable. It also runs the validator over
// the loaded pack to catch the original round-trip failure.
func TestLoadDir_YMLLiteralUIConfigs(t *testing.T) {
	dir := t.TempDir()
	schemas := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(schemas, 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}

	const packYAML = `name: p
version: "1.0.0"
objectTypes:
  - name: Doc
    label: Document
    labels: [doc]
    ui:
      icon: user
      color: "#fff"
    properties:
      title:
        type: string
    required: [title]
uiConfigs:
  Doc:
    icon: user
    color: "#fff"
extractionPrompts:
  Doc:
    prompt: extract documents
`
	path := filepath.Join(schemas, "p.yaml")
	if err := os.WriteFile(path, []byte(packYAML), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	_, packs, _, _, _, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	for _, r := range results {
		if r.Action == blueprints.BlueprintsActionError {
			t.Fatalf("unexpected load error: %v", r.Error)
		}
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 loaded pack, got %d", len(packs))
	}

	uiRaw := packs[0].UIConfigs.Raw()
	if len(uiRaw) == 0 {
		t.Fatal("UIConfigs is empty after YAML decode")
	}
	var ui map[string]map[string]any
	if err := json.Unmarshal(uiRaw, &ui); err != nil {
		t.Fatalf("UIConfigs is not JSON-decodable: %v (%s)", err, uiRaw)
	}
	if ui["Doc"]["icon"] != "user" || ui["Doc"]["color"] != "#fff" {
		t.Fatalf("UIConfigs round-trip = %v", ui)
	}

	exRaw := packs[0].ExtractionPrompts.Raw()
	if len(exRaw) == 0 {
		t.Fatal("ExtractionPrompts is empty after YAML decode")
	}
	var ex map[string]map[string]any
	if err := json.Unmarshal(exRaw, &ex); err != nil {
		t.Fatalf("ExtractionPrompts is not JSON-decodable: %v (%s)", err, exRaw)
	}
	if ex["Doc"]["prompt"] != "extract documents" {
		t.Fatalf("ExtractionPrompts round-trip = %v", ex)
	}

	report := blueprints.Validate(nil, packs, nil, nil, nil, nil, nil)
	if len(report.Issues) != 0 {
		t.Fatalf("expected the loaded pack to validate cleanly, got: %v", report.Issues)
	}
}
