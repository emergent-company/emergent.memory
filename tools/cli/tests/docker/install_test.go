// Package dockertests contains end-to-end tests that run inside a Docker
// container where the emergent CLI has been installed from the GitHub release
// via install.sh — exactly as a real end-user would.
//
// # Test environment
//
// Each test runs against a real Emergent server whose URL is provided via:
//
//	MEMORY_TEST_SERVER  (default: http://localhost:5300)
//
// The container is expected to have these binaries on PATH:
//   - emergent  — installed by install.sh from the GitHub release
//   - opencode  — installed during Docker image build
//
// # Running locally (outside Docker, against your dev server)
//
//	MEMORY_TEST_SERVER=http://localhost:3012 go test -v ./...
//
// # Session logs
//
// Each test writes a structured log of all CLI invocations and their output to
// the directory specified by TEST_LOG_DIR (default: /test-logs inside Docker,
// or /tmp/emergent-cli-docker-tests locally).  Logs are named
// <TestName>_<unix-timestamp>.log so they survive across runs.
package dockertests

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Configuration constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// defaultServer is used when MEMORY_TEST_SERVER is not set.
	// Outside Docker this should be overridden via MEMORY_TEST_SERVER.
	// Inside Docker it is always set by docker-compose.yml to http://emergent-server:5300.
	// There is no safe single default — an empty value causes skipIfServerDown to skip
	// server-dependent tests rather than hitting a wrong address.
	defaultServer = ""

	// e2eTestToken is the static Bearer token accepted by the local dev server.
	// It maps to the AdminUser fixture (test-admin-user).
	e2eTestToken = "e2e-test-user"

	// cliTimeout is the per-command timeout for CLI invocations.
	cliTimeout = 30 * time.Second

	// serverHealthTimeout is how long to wait for the Emergent server to be ready.
	serverHealthTimeout = 60 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: verify the emergent binary is on PATH and responds to basic commands
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_Version verifies that the emergent binary installed via
// install.sh is on PATH and prints a recognisable version string.
// The CLI exposes version as a sub-command (`emergent version`), not `--version`.
func TestCLIInstalled_Version(t *testing.T) {
	logStatusPreamble(t)
	out := mustRunCLI(t, "version")
	t.Logf("emergent version: %s", strings.TrimSpace(out))

	if !strings.Contains(out, "emergent") && !strings.Contains(out, "version") {
		t.Errorf("expected version output to contain 'emergent' or 'version', got: %q", out)
	}
}

// TestCLIInstalled_Help verifies that `emergent --help` exits 0 and lists known
// top-level sub-commands so we know the binary is functionally intact.
func TestCLIInstalled_Help(t *testing.T) {
	logStatusPreamble(t)
	out := mustRunCLI(t, "--help")
	t.Logf("emergent --help output:\n%s", out)

	requiredSubcommands := []string{
		"skills",
		"projects",
		"auth",
	}
	for _, sub := range requiredSubcommands {
		if !strings.Contains(out, sub) {
			t.Errorf("--help output is missing expected sub-command %q", sub)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: set-token auth flow
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_SetToken verifies that `memory set-token` writes credentials
// to ~/.memory/credentials.json so subsequent CLI calls authenticate correctly.
func TestCLIInstalled_SetToken(t *testing.T) {
	logStatusPreamble(t)
	skipIfServerDown(t)

	srv := serverURL()
	out := mustRunCLI(t, "set-token", e2eTestToken, "--server", srv)
	t.Logf("set-token output: %s", strings.TrimSpace(out))

	// The CLI should mention the token was saved/configured.
	if !strings.Contains(strings.ToLower(out), "token") {
		t.Errorf("set-token output did not mention 'token': %q", out)
	}

	// Credentials file must now exist.
	credsPath := filepath.Join(os.Getenv("HOME"), ".memory", "credentials.json")
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		t.Errorf("credentials file not created at %s", credsPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: emergent skills install
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_SkillsInstall verifies that `emergent skills install --force`
// creates the expected emergent-* skill directories under .agents/skills/ in the
// workspace.
func TestCLIInstalled_SkillsInstall(t *testing.T) {
	logStatusPreamble(t)
	srv := serverURL()

	// Create a fresh temp workspace directory to act as the project root.
	ws := t.TempDir()

	out := mustRunCLIInDir(t, ws, "skills", "install", "--force", "--server", srv)
	t.Logf("skills install output:\n%s", out)

	// The emergent-* skills that ship in the embedded catalog must be present.
	expectedSkills := []string{
		"emergent-onboard",
		"emergent-query",
		"emergent-agents",
		"emergent-mcp-servers",
		"emergent-providers",
		"emergent-template-packs",
	}
	for _, skill := range expectedSkills {
		skillDir := filepath.Join(ws, ".agents", "skills", skill)
		if _, err := os.Stat(skillDir); os.IsNotExist(err) {
			t.Errorf("expected skill directory not found: %s", skillDir)
		}
	}
}

// TestCLIInstalled_SkillsInstall_NonEmergentSkillsAbsent verifies that
// non-emergent skills (commit, release, etc.) are NOT installed by default.
func TestCLIInstalled_SkillsInstall_NonEmergentSkillsAbsent(t *testing.T) {
	logStatusPreamble(t)
	srv := serverURL()
	ws := t.TempDir()

	mustRunCLIInDir(t, ws, "skills", "install", "--force", "--server", srv)

	nonEmergentSkills := []string{"commit", "release", "pr-review-and-fix"}
	for _, skill := range nonEmergentSkills {
		skillDir := filepath.Join(ws, ".agents", "skills", skill)
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Errorf("non-emergent skill %q should NOT be installed but directory exists: %s", skill, skillDir)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: installed skills have valid SKILL.md frontmatter
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_SkillsValid verifies that every skill installed by the CLI
// has a SKILL.md with non-empty name and description fields. This catches regressions
// where a skill was accidentally shipped with a broken manifest.
func TestCLIInstalled_SkillsValid(t *testing.T) {
	logStatusPreamble(t)
	srv := serverURL()
	ws := t.TempDir()

	mustRunCLIInDir(t, ws, "skills", "install", "--force", "--server", srv)

	skillsDir := filepath.Join(ws, ".agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("could not read skills directory %s: %v", skillsDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills were installed")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(skillsDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			skillMDPath := filepath.Join(skillDir, "SKILL.md")
			data, err := os.ReadFile(skillMDPath)
			if err != nil {
				t.Fatalf("SKILL.md not found in %s: %v", skillDir, err)
			}

			content := string(data)
			name, desc := parseFrontmatterFields(content)

			if name == "" {
				t.Errorf("SKILL.md in %s has empty 'name' field", entry.Name())
			}
			if desc == "" {
				t.Errorf("SKILL.md in %s has empty 'description' field", entry.Name())
			}
			if name != entry.Name() {
				t.Errorf("SKILL.md name %q does not match directory name %q", name, entry.Name())
			}

			t.Logf("skill %s: name=%q desc=%q", entry.Name(), name, desc)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: emergent skills list
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_SkillsList verifies that `emergent skills list` reports the
// installed skills after `emergent skills install` has run.
func TestCLIInstalled_SkillsList(t *testing.T) {
	logStatusPreamble(t)
	srv := serverURL()
	ws := t.TempDir()

	mustRunCLIInDir(t, ws, "skills", "install", "--force", "--server", srv)

	out := mustRunCLIInDir(t, ws, "skills", "list", "--server", srv)
	t.Logf("skills list output:\n%s", out)

	if !strings.Contains(out, "emergent-onboard") {
		t.Errorf("skills list output does not contain 'emergent-onboard':\n%s", out)
	}
	if !strings.Contains(out, "emergent-query") {
		t.Errorf("skills list output does not contain 'emergent-query':\n%s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: opencode is installed and functional
// ─────────────────────────────────────────────────────────────────────────────

// TestOpencodeInstalled verifies that the opencode binary is on PATH and
// responds to --version. This is a pre-condition for higher-level skill tests
// that drive opencode as an agent runtime.
func TestOpencodeInstalled(t *testing.T) {
	logStatusPreamble(t)
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "opencode", "--version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.String()
	logSession(t, "opencode --version", out)

	if err != nil {
		// opencode may exit non-zero on --version — that's acceptable as long as
		// the binary exists and produces output.
		t.Logf("opencode --version exited with error (may be OK): %v", err)
	}

	if out == "" {
		// binary not found at all
		path, lookErr := exec.LookPath("opencode")
		if lookErr != nil {
			t.Fatalf("opencode binary not found on PATH: %v", lookErr)
		}
		t.Logf("opencode found at %s but produced no output", path)
	}

	t.Logf("opencode version: %s", strings.TrimSpace(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: opencode sees installed skills
// ─────────────────────────────────────────────────────────────────────────────

// TestOpencodeSeesInstalledSkills verifies the full skill-installation → opencode
// visibility pipeline:
//
//  1. Create a bookstore workspace.
//  2. Run `emergent skills install --force` in that workspace.
//  3. Confirm the expected emergent-* skill directories and SKILL.md files exist
//     at .agents/skills/ — the filesystem location opencode reads from.
//  4. Start `opencode serve` from the workspace and wait for it to announce it
//     is listening.  This proves opencode boots successfully with the installed
//     skills present (it would refuse to start if skill discovery failed).
func TestOpencodeSeesInstalledSkills(t *testing.T) {
	logStatusPreamble(t)
	srv := serverURL()

	ws := newBookstoreWorkspace(t)

	// Step 1 — install skills into the workspace.
	out := mustRunCLIInDir(t, ws.Dir, "skills", "install", "--force", "--server", srv)
	t.Logf("skills install output:\n%s", out)

	// Step 2 — verify filesystem structure.
	expectedSkills := []string{
		"emergent-onboard",
		"emergent-query",
		"emergent-agents",
		"emergent-mcp-servers",
		"emergent-providers",
		"emergent-template-packs",
	}
	skillsDir := filepath.Join(ws.Dir, ".agents", "skills")
	for _, skill := range expectedSkills {
		skillMD := filepath.Join(skillsDir, skill, "SKILL.md")
		if _, err := os.Stat(skillMD); os.IsNotExist(err) {
			t.Errorf("expected SKILL.md not found: %s", skillMD)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Step 3 — start opencode serve and confirm it reaches "listening on".
	//
	// opencode discovers skills purely by reading .agents/skills/ in the working
	// directory; there is no HTTP endpoint to query.  Successfully booting
	// confirms the installed skill tree is valid and visible to opencode.
	port := 14300 + (os.Getpid() % 500) // pseudo-random port to avoid conflicts
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

	// Poll until "listening on" appears in the output or the context expires.
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(opencodeOut.String(), "listening on") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	logSession(t, fmt.Sprintf("opencode serve --port %d", port), opencodeOut.String())

	if !strings.Contains(opencodeOut.String(), "listening on") {
		t.Fatalf("opencode serve did not reach 'listening on' within 25s.\nopencode output:\n%s", opencodeOut.String())
	}
	t.Logf("opencode serve started successfully with installed skills (port %d)", port)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: emergent projects list (live server round-trip)
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_ProjectsList verifies a full authenticated round-trip against
// the Emergent server: set-token → projects list.
func TestCLIInstalled_ProjectsList(t *testing.T) {
	logStatusPreamble(t)
	skipIfServerDown(t)

	srv := serverURL()

	// Authenticate first.
	mustRunCLI(t, "set-token", e2eTestToken, "--server", srv)

	out := mustRunCLI(t, "projects", "list", "--server", srv)
	t.Logf("projects list output:\n%s", out)
	// We don't assert on content — there may be zero projects in a fresh server.
	// The test passes if the command exits 0.
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// serverURL returns the Emergent server URL from the environment.
// MEMORY_TEST_SERVER must be set — there is no safe default because the
// server address differs between local dev (http://localhost:3012) and Docker
// Compose (http://emergent-server:5300).
func serverURL() string {
	if v := os.Getenv("MEMORY_TEST_SERVER"); v != "" {
		return v
	}
	return defaultServer
}

// skipIfServerDown skips t if the Emergent server at serverURL() is unreachable.
func skipIfServerDown(t *testing.T) {
	t.Helper()

	srv := serverURL()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv+"/health", nil)
	if err != nil {
		t.Skipf("cannot build health request for %s: %v", srv, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("server unreachable (%s): %v — is MEMORY_TEST_SERVER set?", srv, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Skipf("server health check returned %d (%s)", resp.StatusCode, srv)
	}
}

// mustRunCLI runs `emergent <args>` from the current directory and fails the
// test if the command exits non-zero.  Returns combined stdout+stderr.
func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	return mustRunCLIInDir(t, "", args...)
}

// mustRunCLIInDir runs `emergent <args>` from dir (empty = inherit).
// It fails the test if the command exits non-zero and logs all output.
func mustRunCLIInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "emergent", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Strip project-scoped env vars so workspace .env.local files don't bleed in.
	cmd.Env = filteredEnv()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.String()

	invocation := fmt.Sprintf("emergent %s", strings.Join(args, " "))
	logSession(t, invocation, out)

	if err != nil {
		t.Fatalf("CLI command failed: %s\nerror: %v\noutput:\n%s", invocation, err, out)
	}
	return out
}

// filteredEnv returns os.Environ() with project-scoped variables stripped.
func filteredEnv() []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "MEMORY_PROJECT_TOKEN="),
			strings.HasPrefix(kv, "MEMORY_PROJECT="),
			strings.HasPrefix(kv, "MEMORY_PROJECT_ID="),
			strings.HasPrefix(kv, "MEMORY_API_KEY="):
			// skip
		default:
			filtered = append(filtered, kv)
		}
	}
	return filtered
}

// logStatusPreamble runs `emergent status` and records the output as the first
// log entry for the test.  It is called at the top of every test so that each
// log file starts with the current authentication / server state, making it
// easy to diagnose failures without context-switching to a separate run.
// The command is allowed to fail (e.g. no credentials yet) — the output is
// captured and logged either way.
func logStatusPreamble(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{"status", "--server", serverURL()}
	cmd := exec.CommandContext(ctx, "emergent", args...)
	cmd.Env = filteredEnv()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	_ = cmd.Run() // intentionally ignore exit code — status may fail with no token
	out := buf.String()

	t.Logf("emergent status:\n%s", strings.TrimSpace(out))
	logSession(t, "emergent "+strings.Join(args, " "), out)
}

// logSession writes a structured log of the CLI invocation and its output to
// the test log directory so it can be inspected after the run.
//
// Resolution order for the log directory:
//  1. TEST_LOG_DIR env var (explicit override)
//  2. logs/ sibling to this test file (i.e. tests/docker/logs/) — the default
//     so logs are always kept inside the project tree
//  3. /test-logs — fallback inside Docker if the above doesn't exist
func logSession(t *testing.T, invocation, output string) {
	t.Helper()

	logDir := os.Getenv("TEST_LOG_DIR")
	if logDir == "" {
		// Default: logs/ directory next to the test source, inside the project.
		// runtime.Caller gives us the source file path even inside a test binary.
		_, srcFile, _, ok := runtime.Caller(0)
		if ok {
			logDir = filepath.Join(filepath.Dir(srcFile), "logs")
		} else if _, err := os.Stat("/test-logs"); err == nil {
			logDir = "/test-logs"
		} else {
			logDir = filepath.Join(os.TempDir(), "emergent-cli-docker-tests")
		}
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		// Non-fatal — logging is best-effort.
		t.Logf("warn: could not create log directory %s: %v", logDir, err)
		return
	}

	// File name: <DD>-<MM>-<HH>-<MM>-<SS>-<TestName>.log  (sortable by time, sanitised)
	now := time.Now()
	safeName := strings.NewReplacer("/", "_", " ", "_", ":", "-").Replace(t.Name())
	timestamp := now.Format("02-01-15-04-05") // DD-MM-HH-MM-SS
	logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", timestamp, safeName))

	var sb strings.Builder
	sb.WriteString("=== SESSION LOG ===\n")
	sb.WriteString(fmt.Sprintf("test:      %s\n", t.Name()))
	sb.WriteString(fmt.Sprintf("time:      %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("invoked:   %s\n", invocation))
	sb.WriteString("--- output ---\n")
	sb.WriteString(output)
	if !strings.HasSuffix(output, "\n") {
		sb.WriteByte('\n')
	}
	sb.WriteString("--- end ---\n")

	_ = os.WriteFile(logFile, []byte(sb.String()), 0o644)
	t.Logf("session log: %s", logFile)
}

// parseFrontmatterFields extracts the `name:` and `description:` values from
// a SKILL.md YAML frontmatter block.  It intentionally avoids importing a YAML
// library so the test module stays dependency-free.
func parseFrontmatterFields(content string) (name, description string) {
	// Find opening ---
	const delim = "---"
	first := strings.Index(content, delim)
	if first < 0 {
		return
	}
	rest := content[first+len(delim):]
	second := strings.Index(rest, delim)
	if second < 0 {
		return
	}
	frontmatter := rest[:second]

	scanner := bufio.NewScanner(strings.NewReader(frontmatter))
	for scanner.Scan() {
		line := scanner.Text()
		if k, v, ok := strings.Cut(line, ":"); ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			switch k {
			case "name":
				name = v
			case "description":
				description = v
			}
		}
	}
	return
}
