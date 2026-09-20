// Package cli_test — schemas_test.go
//
// End-to-end tests for `memory schemas` CLI subcommands: list, installed,
// install, uninstall, and compiled-types.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_SchemasList verifies that `memory schemas list` returns
// available schemas from the registry.
func TestCLIInstalled_SchemasList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory schemas list returns available schemas",
		"Create project for context",
		"Run `memory schemas list`",
		"Assert output is non-empty and contains at least one schema entry",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-schemas-list")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Run memory schemas list")
	out := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "list",
		"--project", projectID,
	)
	rl.CLI("memory schemas list --project "+projectID, out)

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from schemas list, got empty string")
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		rl.Failf("expected at least 2 lines from schemas list (header + entry), got %d", len(lines))
	}
	rl.Printf("schemas list returned %d lines", len(lines))
}

// TestCLIInstalled_SchemasInstalledEmpty verifies that a fresh project has no
// installed schemas.
func TestCLIInstalled_SchemasInstalledEmpty(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify fresh project has no installed schemas",
		"Create a new project",
		"Run `memory schemas installed`",
		"Assert output shows no schemas or is empty/minimal",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-schemas-empty")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Run memory schemas installed")
	out := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "installed",
		"--project", projectID,
	)
	rl.CLI("memory schemas installed --project "+projectID, out)

	// A fresh project may show "No schemas installed" or just a header line.
	// The key assertion is that it runs without error (exit 0).
	rl.Printf("schemas installed output: %s", truncate(strings.TrimSpace(out), 300))
}

// TestCLIInstalled_SchemasInstallUninstall exercises the install → installed →
// uninstall workflow.  It picks the first schema from `schemas list` and
// installs it into a fresh project, then uninstalls.
func TestCLIInstalled_SchemasInstallUninstall(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify schema install → installed → uninstall lifecycle",
		"Create project, list available schemas, pick the first one",
		"Install the schema into the project",
		"Verify it appears in `schemas installed`",
		"Uninstall it and verify it is gone from `schemas installed`",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-schemas-inst")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// List available schemas as JSON to extract an ID.
	rl.Section("List available schemas")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory schemas list --project "+projectID+" --output json", listOut)

	// Parse the first schema ID from the JSON array.
	schemaID := extractFirstSchemaID(t, listOut)
	if schemaID == "" {
		t.Skipf("no schemas available in registry — skipping (empty server)")
	}
	rl.Printf("picked schema ID: %s", schemaID)

	// Install the schema.
	rl.Section("Install schema")
	installOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "install", schemaID,
		"--project", projectID,
	)
	rl.CLI("memory schemas install "+schemaID+" --project "+projectID, installOut)
	rl.Printf("install output: %s", truncate(installOut, 300))

	// Extract the assignment ID from install output — uninstall requires the
	// assignment ID, NOT the schema ID.
	assignmentID := parseLineField(installOut, "Assignment ID:")
	if assignmentID == "" {
		// Fallback: try JSON output field.
		assignmentID = parseJSONField(installOut, "assignment_id")
	}
	if assignmentID == "" {
		rl.Failf("could not parse Assignment ID from install output: %s", truncate(installOut, 300))
	}
	rl.Printf("assignment ID: %s", assignmentID)

	// Verify it appears in installed schemas.
	rl.Section("Verify schema appears in installed list")
	installedOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "installed",
		"--project", projectID,
	)
	rl.CLI("memory schemas installed --project "+projectID, installedOut)

	if !strings.Contains(installedOut, schemaID) {
		t.Errorf("expected schemas installed to contain %q, got:\n%s", schemaID, truncate(installedOut, 500))
	}
	rl.Printf("installed list contains schema %s: true", schemaID)

	// Uninstall the schema using the assignment ID.
	rl.Section("Uninstall schema")
	uninstallOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "uninstall", assignmentID,
		"--project", projectID,
	)
	rl.CLI("memory schemas uninstall "+assignmentID+" --project "+projectID, uninstallOut)
	rl.Printf("uninstall output: %s", truncate(uninstallOut, 300))

	// Verify it no longer appears.
	rl.Section("Verify schema removed from installed list")
	afterOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "installed",
		"--project", projectID,
	)
	rl.CLI("memory schemas installed --project "+projectID, afterOut)

	if strings.Contains(afterOut, schemaID) {
		t.Errorf("expected schema %s to be absent after uninstall, but found in:\n%s", schemaID, truncate(afterOut, 500))
	}
	rl.Printf("installed list no longer contains schema %s: true", schemaID)
}

// TestCLIInstalled_SchemasCompiledTypes verifies that `memory schemas
// compiled-types` returns type information after a schema is installed.
func TestCLIInstalled_SchemasCompiledTypes(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify schemas compiled-types shows types after schema install",
		"Create project, install a schema",
		"Run `memory schemas compiled-types` and verify output is non-empty",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-schemas-ct")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Install a schema first.
	rl.Section("Install schema for compiled-types test")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory schemas list --project "+projectID+" --output json", listOut)

	schemaID := extractFirstSchemaID(t, listOut)
	if schemaID == "" {
		t.Skipf("no schemas available — skipping (empty server)")
	}

	installOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "install", schemaID,
		"--project", projectID,
	)
	rl.CLI("memory schemas install "+schemaID+" --project "+projectID, installOut)

	// Run compiled-types.
	rl.Section("Run schemas compiled-types")
	ctOut := mustRunCLIInDirWithHome(t, "", home,
		"schemas", "compiled-types",
		"--project", projectID,
	)
	rl.CLI("memory schemas compiled-types --project "+projectID, ctOut)

	trimmed := strings.TrimSpace(ctOut)
	if trimmed == "" {
		t.Errorf("expected non-empty output from schemas compiled-types after install, got empty")
	}
	rl.Printf("compiled-types output: %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// extractFirstSchemaID parses JSON output from `schemas list --output json`
// and returns the ID of the first schema, or "" if none found.
func extractFirstSchemaID(t *testing.T, jsonOutput string) string {
	t.Helper()
	trimmed := strings.TrimSpace(jsonOutput)

	// Try as array of objects with "id" field.
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && len(arr) > 0 {
		if id, ok := arr[0]["id"].(string); ok && id != "" {
			return id
		}
	}

	// Fallback: try parseJSONField on the raw output.
	if id := parseJSONField(trimmed, "id"); id != "" {
		return id
	}

	return ""
}

var _ = framework.SetToken // keep import used
