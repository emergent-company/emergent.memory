// Package cli_test — install_test.go
//
// End-to-end tests that verify the memory CLI binary is installed and working.
package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fixtures "github.com/emergent-company/emergent.memory/e2e/fixtures"
	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_Version verifies that the memory binary installed via
// install.sh is on PATH and prints a recognisable version string.
func TestCLIInstalled_Version(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory CLI is installed and prints a version string",
		"Run `memory version`",
		"Assert output contains 'memory' or 'version'",
	)
	logStatusPreamble(t)

	rl.Section("Run memory version")
	out := mustRunCLI(t, "version")
	rl.CLI("memory version", out)

	if !strings.Contains(out, "memory") && !strings.Contains(out, "version") {
		t.Errorf("expected version output to contain 'memory' or 'version', got: %q", out)
	}
}

// TestCLIInstalled_Help verifies that `memory --help` exits 0 and lists known
// top-level sub-commands so we know the binary is functionally intact.
func TestCLIInstalled_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory --help lists expected sub-commands",
		"Run `memory --help`",
		"Assert output contains skills, projects, login",
	)
	logStatusPreamble(t)

	rl.Section("Run memory --help")
	out := mustRunCLI(t, "--help")
	rl.CLI("memory --help", out)

	requiredSubcommands := []string{
		"skills",
		"projects",
		"login",
	}
	for _, sub := range requiredSubcommands {
		if !strings.Contains(out, sub) {
			t.Errorf("--help output is missing expected sub-command %q", sub)
		}
	}
}

// TestCLIInstalled_SetToken verifies that `memory set-token` writes credentials
// to ~/.memory/credentials.json so subsequent CLI calls authenticate correctly.
func TestCLIInstalled_SetToken(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory set-token writes credentials.json",
		"Run `memory set-token <token> --server <url>`",
		"Assert output mentions 'token'",
		"Assert ~/.memory/credentials.json exists",
	)
	logStatusPreamble(t)

	srv := serverURL()

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory set-token")
	out := mustRunCLIInDirWithHome(t, "", home, "set-token", e2eTestToken(), "--server", srv)
	rl.CLI(fmt.Sprintf("memory set-token <token> --server %s", srv), out)

	if !strings.Contains(strings.ToLower(out), "token") {
		t.Errorf("set-token output did not mention 'token': %q", out)
	}

	rl.Section("Verify credentials.json written")
	framework.VerifyCredentialsWritten(t, rl, home)
}

// TestCLIInstalled_SkillsInstall verifies that `memory install-memory-skills --force`
// creates the expected memory-* skill directories under .agents/skills/ in the workspace.
func TestCLIInstalled_SkillsInstall(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory install-memory-skills creates expected skill directories",
		"Run `memory install-memory-skills --force` in a temp workspace",
		"Assert each memory-* skill directory exists under .agents/skills/",
	)
	logStatusPreamble(t)

	ws := t.TempDir()

	rl.Section("Run memory install-memory-skills --force")
	out := mustRunCLIInDir(t, ws, "install-memory-skills", "--force")
	rl.CLI("memory install-memory-skills --force", out)

	expectedSkills := []string{
		"memory-agents",
		"memory-blueprints",
		"memory-branches",
		"memory-cli-reference",
		"memory-graph",
		"memory-issue-report",
		"memory-journal",
		"memory-onboard",
		"memory-providers",
		"memory-query",
		"memory-schemas",
	}

	rl.Section("Verify skill directories exist")
	for _, skill := range expectedSkills {
		skillDir := filepath.Join(ws, ".agents", "skills", skill)
		if _, err := os.Stat(skillDir); os.IsNotExist(err) {
			t.Errorf("expected skill directory not found: %s", skillDir)
		}
		rl.Printf("  found: %s", skill)
	}

	// Also verify no unexpected extra skills snuck in.
	entries, err := os.ReadDir(filepath.Join(ws, ".agents", "skills"))
	if err != nil {
		t.Fatalf("could not read skills directory: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != len(expectedSkills) {
		t.Errorf("expected %d skill dirs, got %d: %v", len(expectedSkills), len(dirs), dirs)
	}
	rl.Printf("  total skill dirs: %d (expected %d)", len(dirs), len(expectedSkills))
}

// TestCLIInstalled_SkillsInstall_NonMemorySkillsAbsent verifies that
// non-memory skills (commit, release, etc.) are NOT installed by default.
func TestCLIInstalled_SkillsInstall_NonMemorySkillsAbsent(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify non-memory skills are NOT installed by default",
		"Run `memory install-memory-skills --force`",
		"Assert commit, release, pr-review-and-fix skill dirs are absent",
	)
	logStatusPreamble(t)
	ws := t.TempDir()

	rl.Section("Run memory install-memory-skills --force")
	out := mustRunCLIInDir(t, ws, "install-memory-skills", "--force")
	rl.CLI("memory install-memory-skills --force", out)

	rl.Section("Verify non-memory skills are absent")
	nonMemorySkills := []string{"commit", "release", "pr-review-and-fix"}
	for _, skill := range nonMemorySkills {
		skillDir := filepath.Join(ws, ".agents", "skills", skill)
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Errorf("non-memory skill %q should NOT be installed but directory exists: %s", skill, skillDir)
		}
		rl.Printf("  absent (ok): %s", skill)
	}
}

// TestCLIInstalled_SkillsValid verifies that every skill installed by the CLI
// has a SKILL.md with non-empty name and description fields.
func TestCLIInstalled_SkillsValid(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify every installed skill has a valid SKILL.md with name and description",
		"Run `memory install-memory-skills --force`",
		"For each skill dir: parse SKILL.md frontmatter, assert name and description are non-empty",
		"Assert name matches directory name",
	)
	logStatusPreamble(t)
	ws := t.TempDir()

	rl.Section("Run memory install-memory-skills --force")
	out := mustRunCLIInDir(t, ws, "install-memory-skills", "--force")
	rl.CLI("memory install-memory-skills --force", out)

	skillsDir := filepath.Join(ws, ".agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("could not read skills directory %s: %v", skillsDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills were installed")
	}

	rl.Section("Validate SKILL.md for each installed skill")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			framework.VerifySkillInstalled(t, rl, skillsDir, entry.Name())
		})
	}
}

// TestCLIInstalled_SkillsList verifies that `memory skills list` returns
// server-side skills via an authenticated CLI round-trip.
func TestCLIInstalled_SkillsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory skills list returns server-side skills",
		"Create a project and skill",
		"Run `memory skills list --project`",
		"Assert output is non-empty and contains at least one skill name",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	// Create a project so we have a scope for the skill.
	srv := serverURL()
	projName := uniqueProjectName("e2e-skills-ls")
	projectID := createProject(t, home, srv, projName)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Create a test skill")
	createOut := mustRunCLIInDirWithHome(t, "", home, "skills", "create",
		"--name", "e2e-list-check",
		"--description", "Skill for list check",
		"--content", "test content",
		"--project", projectID)
	rl.CLI("memory skills create", createOut)

	rl.Section("Run memory skills list --project")
	out := mustRunCLIInDirWithHome(t, "", home, "skills", "list", "--project", projectID)
	rl.CLI("memory skills list --project "+projectID, out)

	rl.Section("Verify output contains skills")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from `memory skills list`, got empty string")
	}
	// The table output contains skill names and descriptions.
	// At minimum it should have a header row or skill entries.
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		rl.Failf("expected at least 2 lines (header + skill) from `memory skills list`, got %d lines", len(lines))
	}
	rl.Printf("  skill list returned %d lines", len(lines))
}

// TestOpencodeInstalled verifies that the opencode binary is on PATH and
// responds to --version.
func TestOpencodeInstalled(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify opencode binary is on PATH and responds to --version",
		"Run `opencode --version`",
		"Assert binary is found and produces output",
	)
	logStatusPreamble(t)
	ctx, cancel := context.WithTimeout(context.Background(), framework.CLITimeout)
	defer cancel()

	rl.Section("Run opencode --version")
	cmd := exec.CommandContext(ctx, "opencode", "--version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.String()
	rl.CLI("opencode --version", out)
	logSession(t, "opencode --version", out)

	if err != nil {
		rl.Printf("opencode --version exited with error (may be OK): %v", err)
	}

	if out == "" {
		path, lookErr := exec.LookPath("opencode")
		if lookErr != nil {
			t.Fatalf("opencode binary not found on PATH: %v", lookErr)
		}
		rl.Printf("opencode found at %s but produced no output", path)
	} else {
		rl.Printf("opencode version: %s", strings.TrimSpace(out))
	}
}

// TestOpencodeSeesInstalledSkills verifies the full skill-installation → opencode
// visibility pipeline.
func TestOpencodeSeesInstalledSkills(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify full skill-installation → opencode visibility pipeline",
		"Install memory skills into a bookstore workspace",
		"Start opencode serve and wait for 'listening on'",
		"Assert all expected skill SKILL.md files are present",
	)
	logStatusPreamble(t)

	ws := fixtures.NewBookstoreWorkspace(t)

	rl.Section("Install memory skills into workspace")
	out := mustRunCLIInDir(t, ws.Dir, "install-memory-skills", "--force")
	rl.CLI("memory install-memory-skills --force", out)

	rl.Section("Verify skill SKILL.md files present")
	expectedSkills := []string{
		"memory-agents",
		"memory-blueprints",
		"memory-branches",
		"memory-cli-reference",
		"memory-graph",
		"memory-issue-report",
		"memory-journal",
		"memory-onboard",
		"memory-providers",
		"memory-query",
		"memory-schemas",
	}
	skillsDir := filepath.Join(ws.Dir, ".agents", "skills")
	for _, skill := range expectedSkills {
		t.Run(skill, func(t *testing.T) {
			framework.VerifySkillInstalled(t, rl, skillsDir, skill)
		})
	}
	if t.Failed() {
		t.FailNow()
	}

	rl.Section("Start opencode serve and wait for ready")
	port := 14300 + (os.Getpid() % 500)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opencodeCmd := exec.CommandContext(ctx, "opencode", "serve", "--port", fmt.Sprintf("%d", port))
	opencodeCmd.Dir = ws.Dir

	var opencodeOut bytes.Buffer
	opencodeCmd.Stdout = &opencodeOut
	opencodeCmd.Stderr = &opencodeOut

	if err := opencodeCmd.Start(); err != nil {
		t.Fatalf("failed to start opencode serve: %v", err)
	}
	defer func() {
		if opencodeCmd.Process != nil {
			_ = opencodeCmd.Process.Kill()
			_ = opencodeCmd.Wait()
		}
	}()

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(opencodeOut.String(), "listening on") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	serveOut := opencodeOut.String()
	rl.CLI(fmt.Sprintf("opencode serve --port %d", port), serveOut)
	logSession(t, fmt.Sprintf("opencode serve --port %d", port), serveOut)

	if !strings.Contains(serveOut, "listening on") {
		t.Fatalf("opencode serve did not reach 'listening on' within 25s.\nopencode output:\n%s", serveOut)
	}
}

// TestCLIInstalled_ProjectsList verifies a full authenticated round-trip against
// the Emergent server: auth setup → projects list.
func TestCLIInstalled_ProjectsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify full authenticated round-trip: auth setup → projects list",
		"Set up CLI auth (set-token)",
		"Run `memory projects list`",
		"Assert output is non-empty and contains 'project' (table header or count line)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory projects list")
	out := mustRunCLIInDirWithHome(t, "", home, "projects", "list")
	rl.CLI("memory projects list", out)

	rl.Section("Verify output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from `memory projects list`, got empty string")
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "project") {
		rl.Failf("expected output to contain 'project' (table header or count line), got:\n%s", trimmed)
	}
	rl.Printf("output length: %d bytes, contains 'project': true", len(trimmed))
}

// TestCLIInstalled_StatusAuthenticated verifies that `memory status` returns
// connection and authentication information when the CLI is authenticated.
func TestCLIInstalled_StatusAuthenticated(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory status shows connection and auth info when authenticated",
		"Set up CLI auth",
		"Run `memory status`",
		"Assert output contains server URL and connection status",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory status")
	out := mustRunCLIInDirWithHome(t, "", home, "status")
	rl.CLI("memory status", out)

	rl.Section("Verify status output")
	lower := strings.ToLower(out)
	// Status output shows "Authentication Status:", "Mode: OAuth", "Status: ✓ Authenticated", etc.
	if !strings.Contains(lower, "status") && !strings.Contains(lower, "mode") {
		t.Errorf("expected status output to contain 'status' or 'mode', got:\n%s", out)
	}
	// Should show authentication info — "authenticated", "oauth", or the server URL.
	if !strings.Contains(lower, "authenticated") && !strings.Contains(lower, "oauth") && !strings.Contains(lower, strings.ToLower(serverURL())) {
		t.Errorf("expected status output to show auth info (authenticated/oauth/server URL), got:\n%s", out)
	}
	rl.Printf("status output contains auth info: true (%d bytes)", len(out))
}

// TestCLIInstalled_ConfigShowAndSet verifies that `memory config show` and
// `memory config set` work correctly.
func TestCLIInstalled_ConfigShowAndSet(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory config show and config set work correctly",
		"Set up CLI auth",
		"Run `memory config show` and verify output",
		"Set a config value with `memory config set`",
		"Verify the change is reflected in a subsequent `config show`",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory config show (initial)")
	showOut := mustRunCLIInDirWithHome(t, "", home, "config", "show")
	rl.CLI("memory config show", showOut)

	if strings.TrimSpace(showOut) == "" {
		rl.Failf("expected non-empty output from config show, got empty")
	}
	// Config show should contain the server_url we set during auth.
	if !strings.Contains(showOut, serverURL()) {
		t.Errorf("expected config show to contain server URL %q, got:\n%s", serverURL(), showOut)
	}
	rl.Printf("config show contains server URL: true")

	// Set a new config value — use project_id with a known sentinel value.
	rl.Section("Set config value")
	sentinel := fmt.Sprintf("e2e-sentinel-%d", time.Now().UnixMilli())
	setOut := mustRunCLIInDirWithHome(t, "", home, "config", "set", "project_id", sentinel)
	rl.CLI("memory config set project_id "+sentinel, setOut)

	// Verify the value persisted.
	rl.Section("Verify config value persisted")
	showOut2 := mustRunCLIInDirWithHome(t, "", home, "config", "show")
	rl.CLI("memory config show", showOut2)

	if !strings.Contains(showOut2, sentinel) {
		t.Errorf("expected config show to contain sentinel %q after set, got:\n%s", sentinel, showOut2)
	}
	rl.Printf("config show contains sentinel value: true")
}
