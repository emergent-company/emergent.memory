// Package harness manages Emergent project lifecycle for tests:
// create before the test, delete (async) after.
//
// Auth: uses the emergent CLI for both create and delete so that the CLI's
// configured OAuth/API-key flow handles authentication automatically.
// For local dev, override the server via MEMORY_TEST_SERVER env var
// (defaults to http://localhost:5300).
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	defaultServer  = "http://localhost:5300"
	deleteGraceSec = 5 // seconds to wait after async delete
)

// Project holds the created test project metadata.
type Project struct {
	ID   string
	Name string
}

// serverURL returns the Emergent server URL from env or the default.
func serverURL() string {
	if v := os.Getenv("MEMORY_TEST_SERVER"); v != "" {
		return v
	}
	return defaultServer
}

// CreateProject creates a new Emergent project via the emergent CLI and
// registers a t.Cleanup to delete it after the test.
func CreateProject(t *testing.T, name string) *Project {
	t.Helper()

	srv := serverURL()

	// emergent projects create --name <name> --server <url>
	// The CLI prints "Project created successfully!\n  Name: <name> (<id>)"
	// We use --output json if available; fall back to parsing text output.
	out, err := runCLI(t, "projects", "create",
		"--name", name,
		"--server", srv,
	)
	if err != nil {
		t.Fatalf("harness: create project %q: %v\noutput: %s", name, err, out)
	}

	// Parse the project ID from the CLI output.
	// Output format: "Project created successfully!\n  Name: <name> (<id>)\n"
	proj := &Project{Name: name}
	proj.ID = parseProjectID(out)
	if proj.ID == "" {
		t.Fatalf("harness: could not parse project ID from output:\n%s", out)
	}

	t.Logf("harness: created project %s (%s)", proj.Name, proj.ID)

	t.Cleanup(func() {
		deleteProject(t, proj, srv)
	})

	return proj
}

// deleteProject deletes a project via the emergent CLI.
func deleteProject(t *testing.T, p *Project, srv string) {
	t.Helper()

	out, err := runCLI(t, "projects", "delete", p.ID,
		"--server", srv,
	)
	if err != nil {
		t.Logf("harness: delete project %s (%s): %v (ignored)\noutput: %s", p.Name, p.ID, err, out)
		return
	}

	// Delete is async — give the server a moment to cascade.
	time.Sleep(deleteGraceSec * time.Second)
	t.Logf("harness: deleted project %s (%s)", p.Name, p.ID)
}

// SkipIfServerDown skips the test if the Emergent server is not reachable.
func SkipIfServerDown(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv := serverURL()
	req, err := http.NewRequestWithContext(ctx, "GET", srv+"/health", nil)
	if err != nil {
		t.Skipf("harness: build health request for %s: %v", srv, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("harness: server unreachable (%s): %v", srv, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Skipf("harness: server health check returned %d (%s)", resp.StatusCode, srv)
	}
}

// InstallSkills installs the emergent-onboard (and any other test-required) skills
// into the workspace directory. It shells out to `emergent skills install --force`.
func InstallSkills(t *testing.T, workspaceDir string) {
	t.Helper()

	srv := serverURL()
	out, err := runCLIInDir(t, workspaceDir, "skills", "install", "--force", "--server", srv)
	if err != nil {
		t.Fatalf("harness: install skills: %v\noutput: %s", err, out)
	}
	t.Logf("harness: installed skills")
}

// ServerURL returns the server URL the harness is configured to use.
func ServerURL() string {
	return serverURL()
}

// SetupAuth writes a static Bearer token to ~/.emergent/credentials.json by
// running `emergent set-token <token> --server <url>`. This is required
// before any other harness CLI calls when the server uses Zitadel OAuth and
// there are no OAuth credentials cached from a previous `emergent login`.
//
// The token is written with a 24-hour TTL. Call this once at the top of any
// test that needs to make CLI calls.
func SetupAuth(t *testing.T, token string) {
	t.Helper()

	srv := serverURL()
	out, err := runCLI(t, "set-token", token, "--server", srv)
	if err != nil {
		t.Fatalf("harness: auth set-token: %v\noutput: %s", err, out)
	}
	t.Logf("harness: auth configured (set-token)")
}

// CreateProjectToken creates a project-scoped API token for the given project ID
// by running `emergent projects create-token <projectID> --no-env --server <url>`.
// It returns the raw token string (e.g. "emt_...").
//
// The token can then be written to the workspace .env.local as MEMORY_PROJECT_TOKEN
// so that CLI commands (like `emergent query`) running inside the opencode agent
// workspace are authenticated for the project.
func CreateProjectToken(t *testing.T, projectID string) string {
	t.Helper()

	srv := serverURL()
	out, err := runCLI(t, "projects", "create-token", projectID,
		"--name", "test-workspace-token",
		"--no-env",
		"--server", srv,
	)
	if err != nil {
		t.Fatalf("harness: create-token for project %s: %v\noutput: %s", projectID, err, out)
	}

	// Output format: "Token created: <token>\n"
	token := parseToken(out)
	if token == "" {
		t.Fatalf("harness: could not parse token from output:\n%s", out)
	}

	t.Logf("harness: created project token for %s", projectID)
	return token
}

// parseToken extracts the token string from the create-token CLI output.
// It looks for the pattern "Token created: <token>".
func parseToken(output string) string {
	const prefix = "Token created: "
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// runCLI runs `emergent <args>` from the current directory and returns combined output.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runCLIInDir(t, "", args...)
}

// runCLIInDir runs `emergent <args>` from dir and returns combined output.
// When dir is empty the command inherits the test process working directory.
// In both cases, project-scoped environment variables (MEMORY_PROJECT_TOKEN,
// MEMORY_PROJECT, MEMORY_PROJECT_ID) are stripped from the subprocess
// environment to prevent a workspace .env.local in the test process's cwd
// from interfering with org-level harness calls.
func runCLIInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "emergent", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Inherit the full environment but strip project-scoped vars so that a
	// workspace .env.local sitting in the test process's cwd (e.g. /root/emergent)
	// does not override the org-level credentials we set via set-token.
	filtered := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "MEMORY_PROJECT_TOKEN="),
			strings.HasPrefix(kv, "MEMORY_PROJECT="),
			strings.HasPrefix(kv, "MEMORY_PROJECT_ID="),
			strings.HasPrefix(kv, "MEMORY_API_KEY="):
			// skip — these would override our credentials.json auth
		default:
			filtered = append(filtered, kv)
		}
	}
	cmd.Env = filtered

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}

// parseProjectID extracts the UUID from CLI create output.
// It looks for the pattern "  Name: <anything> (<uuid>)".
func parseProjectID(output string) string {
	// Try JSON first (in case --output json is added later)
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err == nil && parsed.ID != "" {
		return parsed.ID
	}

	// Parse text: find "(<uuid>)" — last parenthesised group of correct length
	start := -1
	for i := len(output) - 1; i >= 0; i-- {
		if output[i] == ')' && start == -1 {
			start = i
		}
		if output[i] == '(' && start != -1 {
			candidate := output[i+1 : start]
			if isUUID(candidate) {
				return candidate
			}
			start = -1 // reset and keep searching
		}
	}
	return ""
}

// isUUID returns true if s looks like a lowercase UUID.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
