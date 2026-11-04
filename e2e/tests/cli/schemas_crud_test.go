// Package cli_test — schemas_crud_test.go
//
// End-to-end tests for custom schema CRUD: `memory schemas create`, `schemas
// get`, and `schemas delete` (from the global registry).
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_SchemasCreateGetDelete exercises the full custom schema
// lifecycle: create from JSON → get by ID → delete from registry.
func TestCLIInstalled_SchemasCreateGetDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify custom schema create → get → delete lifecycle",
		"Create a project for context",
		"Write a schema JSON file and create it via `schemas create`",
		"Get the schema by ID and verify fields",
		"Delete the schema from the registry",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-schema-crud")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Write schema JSON file.
	rl.Section("Write schema JSON file")
	schemaFile := filepath.Join(home, "test_schema.json")
	schemaJSON := `{
  "name": "e2e-widget-schema",
  "version": "1.0.0",
  "description": "Test schema for e2e CRUD",
  "objectTypeSchemas": [
    {
      "name": "Widget",
      "description": "A test widget",
      "properties": {
        "color": {"type": "string", "description": "Widget color"},
        "size": {"type": "number", "description": "Widget size"}
      }
    }
  ],
  "relationshipTypeSchemas": [
    {
      "name": "contains",
      "description": "Container relationship",
      "sourceType": "Widget",
      "targetType": "Widget"
    }
  ]
}`
	if err := os.WriteFile(schemaFile, []byte(schemaJSON), 0644); err != nil {
		rl.Failf("failed to write schema file: %v", err)
	}
	rl.Printf("wrote schema file: %s", schemaFile)

	// Create schema.
	rl.Section("Create schema from JSON file")
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "create",
		"--file", schemaFile,
		"--project", projectID,
	)
	rl.CLI("memory schemas create --file "+schemaFile+" --project "+projectID, createOut)

	if !strings.Contains(createOut, "e2e-widget-schema") {
		t.Errorf("expected create output to mention schema name, got:\n%s", truncate(createOut, 500))
	}
	// Parse the schema ID from the output.
	schemaID := parseLineField(createOut, "ID:")
	if schemaID == "" {
		schemaID = parseJSONField(createOut, "id")
	}
	if schemaID == "" {
		rl.Failf("could not parse schema ID from create output: %s", truncate(createOut, 300))
	}
	rl.Printf("created schema ID: %s", schemaID)

	// Clean up the schema at the end (delete from global registry).
	t.Cleanup(func() {
		runCLIInDirWithHome(t, "", home,
			"schemas", "delete", schemaID,
			"--project", projectID,
		)
	})

	// Get schema by ID (table format).
	rl.Section("Get schema by ID (table format)")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "get", schemaID,
		"--project", projectID,
	)
	rl.CLI("memory schemas get "+schemaID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, "e2e-widget-schema") {
		t.Errorf("expected get output to contain schema name, got:\n%s", truncate(getOut, 500))
	}
	if !strings.Contains(getOut, "1.0.0") {
		t.Errorf("expected get output to contain version 1.0.0, got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("table output contains name and version")

	// Get schema by ID (JSON format).
	rl.Section("Get schema by ID (JSON format)")
	jsonOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "get", schemaID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory schemas get "+schemaID+" --project "+projectID+" --output json", jsonOut)

	trimmed := strings.TrimSpace(jsonOut)
	if !json.Valid([]byte(trimmed)) {
		t.Errorf("expected valid JSON from schemas get --output json, got:\n%s", truncate(trimmed, 500))
	}
	gotName := parseJSONField(jsonOut, "name")
	if gotName != "e2e-widget-schema" {
		t.Errorf("expected name 'e2e-widget-schema', got %q", gotName)
	}
	rl.Printf("JSON: name=%s, valid JSON=true", gotName)

	// Delete schema.
	rl.Section("Delete schema from registry")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "delete", schemaID,
		"--project", projectID,
	)
	rl.CLI("memory schemas delete "+schemaID+" --project "+projectID, delOut)
	rl.Printf("delete output: %s", truncate(delOut, 300))

	// Verify it's gone — get should error.
	rl.Section("Verify schema deleted")
	verifyOut, err := runCLIInDirWithHome(t, "", home,
		"schemas", "get", schemaID,
		"--project", projectID,
	)
	if err == nil && strings.Contains(verifyOut, "e2e-widget-schema") {
		t.Errorf("expected schema to be deleted, but get still returned it")
	}
	rl.Printf("schema no longer accessible after delete: true")
}

var _ = framework.SetToken // keep import used
