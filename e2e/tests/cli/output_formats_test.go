// Package cli_test — output_formats_test.go
//
// Tests for --output flag variants (csv, json) across key CLI commands.
// These tests verify that output format flags are accepted without error and
// produce the expected structure where supported.
//
// Notes on current CLI behavior:
//   - `--output csv` is accepted by most commands but produces the same
//     human-readable format as `--output table` (CSV not yet implemented for
//     most commands).
//   - `--output json` / `--json` is implemented for commands like
//     `projects list` and `graph objects list`.
//   - `memory status` does not yet produce JSON output even with --output json.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCLI_ProjectsList_CSV verifies that `memory projects list --output csv`
// is accepted without error and produces non-empty output.
func TestCLI_ProjectsList_CSV(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify projects list --output csv is accepted without error",
		"Authenticate CLI",
		"Run `memory projects list --output csv`",
		"Assert exit 0 and non-empty output",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory projects list --output csv")
	out := mustRunCLIInDirWithHome(t, "", home, "projects", "list", "--output", "csv")
	rl.CLI("memory projects list --output csv", truncate(out, 500))

	rl.Section("Assert non-empty output")
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected non-empty output from projects list --output csv")
	}
	rl.Printf("projects list --output csv: %d bytes", len(out))
}

// TestCLI_ProjectsList_JSON verifies that `memory projects list --output json`
// produces a valid JSON array with id and name fields.
func TestCLI_ProjectsList_JSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify projects list --output json produces a valid JSON array",
		"Authenticate CLI",
		"Run `memory projects list --output json`",
		"Assert exit 0, valid JSON array, each item has id and name",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory projects list --output json")
	out := mustRunCLIInDirWithHome(t, "", home, "projects", "list", "--output", "json")
	rl.CLI("memory projects list --output json", truncate(out, 500))

	rl.Section("Parse JSON array")
	var projects []map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &projects); err != nil {
		t.Fatalf("expected valid JSON array from projects list --output json, got parse error: %v\noutput:\n%s", err, truncate(out, 500))
	}
	rl.Printf("projects list --output json: %d projects parsed", len(projects))

	if len(projects) == 0 {
		t.Skip("no projects found — skipping field assertions")
	}

	rl.Section("Verify id and name fields on first project")
	first := projects[0]
	if _, ok := first["id"]; !ok {
		t.Errorf("expected 'id' field in project JSON, got: %v", first)
	}
	if _, ok := first["name"]; !ok {
		t.Errorf("expected 'name' field in project JSON, got: %v", first)
	}
	rl.Printf("first project: id=%v name=%v", first["id"], first["name"])
}

// TestCLI_Status_OutputFields verifies that `memory status` produces
// human-readable output containing the expected sections: CLI version,
// authentication info, and server health.
//
// Note: `memory status --output json` is not yet implemented — this test
// verifies the default human-readable output contains the key fields.
func TestCLI_Status_OutputFields(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory status output contains expected sections",
		"Authenticate CLI",
		"Run `memory status`",
		"Assert output contains CLI Version, Authentication, and Server sections",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory status")
	out := mustRunCLIInDirWithHome(t, "", home, "status")
	rl.CLI("memory status", truncate(out, 800))

	rl.Section("Verify key sections present")
	checks := []struct {
		label   string
		keyword string
	}{
		{"CLI Version", "CLI Version"},
		{"Authentication section", "Authentication"},
		{"Server section", "Server"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.keyword) {
			t.Errorf("expected status output to contain %q, got:\n%s", c.keyword, truncate(out, 500))
		} else {
			rl.Printf("  found: %s", c.label)
		}
	}
}

// TestCLI_GraphObjectsList_JSON verifies that `memory graph objects list
// --output json` produces a valid JSON array for an existing project.
func TestCLI_GraphObjectsList_JSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects list --output json produces a valid JSON array",
		"Create a temp project",
		"Run `memory graph objects list --output json`",
		"Assert exit 0 and valid JSON array (may be empty)",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create temp project")
	projectName := uniqueProjectName("e2e-graph-json")
	projectID := createProject(t, home, serverURL(), projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project created: id=%s name=%s", projectID, projectName)

	rl.Section("Run memory graph objects list --output json")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list", "--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects list --output json", truncate(out, 500))

	rl.Section("Parse JSON")
	trimmed := strings.TrimSpace(out)
	// API may return a paginated envelope {"items":[...],"total":N} or a plain array.
	var objects []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &objects); err != nil {
		// Try paginated envelope.
		var envelope struct {
			Items []json.RawMessage `json:"items"`
			Total int               `json:"total"`
		}
		if err2 := json.Unmarshal([]byte(trimmed), &envelope); err2 != nil {
			t.Fatalf("expected valid JSON from graph objects list --output json, got: %v\noutput:\n%s", err, truncate(out, 500))
		}
		objects = envelope.Items
	}
	rl.Printf("graph objects list --output json: %d objects (expected empty for new project)", len(objects))
}
