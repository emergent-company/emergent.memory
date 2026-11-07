package blueprints_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// A pack's migrations block used to be dropped by the CLI's PackFile, so an
// author's type/property renames never reached an upgrade. It must survive
// loading, in the array-of-{from,to} form the server decodes.
func TestLoadDir_CarriesMigrations(t *testing.T) {
	dir := t.TempDir()
	schemas := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(schemas, 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}

	const packYAML = `name: p
version: "3.1.0"
objectTypes:
  - name: Note
    label: Note
    labels: [note]
    ui: {icon: "n", color: "#fff"}
    properties:
      title: {type: string}
relationshipTypes:
  - name: mentions
    label: Mentions
    sourceType: Note
    targetType: Note
migrations:
  from_version: "2.1.0"
  type_renames:
    - from: Memo
      to: Note
  property_renames:
    - type_name: Note
      from: name
      to: title
  removed_properties:
    - type_name: Note
      name: legacy
`
	if err := os.WriteFile(filepath.Join(schemas, "p.yaml"), []byte(packYAML), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	_, packs, _, _, _, _, _, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("expected 1 loaded pack, got %d", len(packs))
	}
	m := packs[0].Migrations
	if m == nil {
		t.Fatal("migrations block was dropped by the loader")
	}
	if m.FromVersion != "2.1.0" {
		t.Errorf("FromVersion = %q, want 2.1.0", m.FromVersion)
	}
	if len(m.TypeRenames) != 1 || m.TypeRenames[0].From != "Memo" || m.TypeRenames[0].To != "Note" {
		t.Errorf("TypeRenames = %+v, want [{Memo Note}]", m.TypeRenames)
	}
	if len(m.PropertyRenames) != 1 {
		t.Fatalf("PropertyRenames = %+v, want exactly one", m.PropertyRenames)
	}
	// TypeName is the field yaml.v3 could not fill while the type carried only
	// json tags (it lowercased TypeName to "typename", not "type_name"), so a
	// real blueprint's property renames decoded with an empty type name. Assert
	// it explicitly: checking only To let that regression through.
	if pr := m.PropertyRenames[0]; pr.TypeName != "Note" || pr.From != "name" || pr.To != "title" {
		t.Errorf("PropertyRenames[0] = %+v, want {Note name title}", pr)
	}
	if len(m.RemovedProperties) != 1 || m.RemovedProperties[0].Name != "legacy" {
		t.Errorf("RemovedProperties = %+v, want one named legacy", m.RemovedProperties)
	}

	// The block is sent to the API as JSON, so assert the wire shape too: the
	// server's decoder wants `type_name`, and an empty one is rejected.
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal hints: %v", err)
	}
	if want := `"type_name":"Note"`; !strings.Contains(string(raw), want) {
		t.Errorf("hints JSON %s does not carry %s", raw, want)
	}
}

// The offline validator rejects a rename that cannot apply, so an author learns
// before the API answers 400.
func TestValidate_MigrationHints(t *testing.T) {
	mk := func(hints *sdkschemas.SchemaMigrationHints) blueprints.PackFile {
		return blueprints.PackFile{
			Name:    "p",
			Version: "3.1.0",
			ObjectTypes: []blueprints.ObjectTypeDef{{
				Name:       "Note",
				Label:      "Note",
				Labels:     []string{"note"},
				UI:         map[string]any{"icon": "n", "color": "#fff"},
				Properties: map[string]any{"title": map[string]any{"type": "string"}},
			}},
			RelationshipTypes: []blueprints.RelationshipTypeDef{{
				Name: "mentions", Label: "Mentions", SourceType: "Note", TargetType: "Note",
			}},
			Migrations: hints,
		}
	}

	t.Run("valid hints pass", func(t *testing.T) {
		pack := mk(&sdkschemas.SchemaMigrationHints{
			FromVersion:     "2.1.0",
			TypeRenames:     []sdkschemas.TypeRename{{From: "Memo", To: "Note"}},
			PropertyRenames: []sdkschemas.PropertyRename{{TypeName: "Note", From: "name", To: "title"}},
		})
		report := blueprints.Validate(nil, []blueprints.PackFile{pack}, nil, nil, nil, nil, nil)
		if report.HasErrors() {
			t.Fatalf("expected no errors, got %v", report.Errors())
		}
	})

	t.Run("rename target must exist", func(t *testing.T) {
		pack := mk(&sdkschemas.SchemaMigrationHints{
			FromVersion: "2.1.0",
			TypeRenames: []sdkschemas.TypeRename{{From: "Memo", To: "Missing"}},
		})
		report := blueprints.Validate(nil, []blueprints.PackFile{pack}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Errors(), "migrations.type_renames[0].to", `type "Missing" does not exist`) {
			t.Fatalf("expected an unknown-target error, got %v", report.Errors())
		}
	})

	t.Run("property rename target must exist on the type", func(t *testing.T) {
		pack := mk(&sdkschemas.SchemaMigrationHints{
			FromVersion:     "2.1.0",
			PropertyRenames: []sdkschemas.PropertyRename{{TypeName: "Note", From: "name", To: "missing"}},
		})
		report := blueprints.Validate(nil, []blueprints.PackFile{pack}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Errors(), "migrations.property_renames[0].to", `property "missing" does not exist`) {
			t.Fatalf("expected an unknown-property error, got %v", report.Errors())
		}
	})

	t.Run("from_version is required", func(t *testing.T) {
		pack := mk(&sdkschemas.SchemaMigrationHints{
			TypeRenames: []sdkschemas.TypeRename{{From: "Memo", To: "Note"}},
		})
		report := blueprints.Validate(nil, []blueprints.PackFile{pack}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Errors(), "migrations.from_version", "from_version is required") {
			t.Fatalf("expected a from_version error, got %v", report.Errors())
		}
	})
}
