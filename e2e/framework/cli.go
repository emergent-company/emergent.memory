// Package e2eframework — cli.go
//
// Redacting CLI runners. runlog's MustRun* helpers log the raw argv when a
// command fails; these wrappers keep the same behaviour (execute, record a
// "cli" event on failure, fail the test with the output) but build the
// invocation through FormatInvocation so secret-bearing flags are masked in
// BOTH the recorded event and the printed failure message.
package e2eframework

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	runlog "github.com/emergent-company/runlog"
)

// runBinary executes binary with args, returning combined output, the command
// error, and the redacted invocation string used for recording and printing.
func runBinary(t *testing.T, binary, dir, home string, args ...string) (string, error, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), CLITimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	env := FilteredEnv()
	env = append(env, "HOME="+home)
	env = append(env, "PATH="+home+"/"+runlog.DefaultBinaryPath+":"+os.Getenv("PATH"))
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	// One redaction pass, used by both the stored event and the fatal message.
	return buf.String(), err, FormatInvocation(binary, args...)
}

// cliFailureMessage is the text passed to t.Fatalf when a Must* CLI helper
// fails. It receives the already-redacted invocation.
func cliFailureMessage(invocation string, err error, out string) string {
	return RedactText(fmt.Sprintf("CLI command failed: %s\nerror: %v\noutput:\n%s", invocation, err, out))
}

// recordCLI stores a CLI event in the active RunLog for t, redacting both the
// description and the invocation/output. No-op when no RunLog is active,
// matching runlog's behaviour.
func recordCLI(t *testing.T, desc, invocation, output string, err error) {
	t.Helper()
	v, ok := ActiveRunLogs.Load(t.Name())
	if !ok {
		return
	}
	rl, ok := v.(*RunLog)
	if !ok || rl == nil {
		return
	}
	rl.CLIStepErr(RedactText(desc), RedactText(invocation), RedactText(output), err, 0)
}

// recordCLIFailure stores the failure of a Must* helper. It mirrors runlog's
// logCLIFailureIfActive: the event is written before t.Fatalf exits the test.
func recordCLIFailure(t *testing.T, invocation, output string, err error) {
	t.Helper()
	recordCLI(t, "$ "+invocation, invocation, output, err)
}

// logCLISuccess stores a successful CLI step (used by multi-command helpers so
// commands they run on the caller's behalf still leave a trace).
func logCLISuccess(t *testing.T, invocation, output string) {
	t.Helper()
	recordCLI(t, "$ "+invocation, invocation, output, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// CLI runners (replace runlog's re-exported MustRun*/Run* helpers)
// ─────────────────────────────────────────────────────────────────────────────

// MustRunCLI runs `memory <args>` from the current directory and fails the
// test if the command exits non-zero. Returns combined stdout+stderr.
func MustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	return MustRunCLIInDir(t, "", args...)
}

// MustRunCLIInDir runs `memory <args>` from dir with a per-test isolated HOME.
func MustRunCLIInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return MustRunCLIInDirWithHome(t, dir, t.TempDir(), args...)
}

// MustRunCLIInDirWithHome runs `memory <args>` from dir with the given home
// directory injected as HOME in the subprocess environment.
func MustRunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) string {
	t.Helper()
	return MustRunBinaryInDirWithHome(t, runlog.DefaultBinaryName, dir, home, args...)
}

// MustRunBinaryInDirWithHome runs `<binary> <args>` from dir with the given
// home directory and fails the test if the command exits non-zero.
func MustRunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) string {
	t.Helper()
	out, err, invocation := runBinary(t, binary, dir, home, args...)
	if err != nil {
		// Scrub the captured output too: a CLI may echo the credential
		// without its flag, and recording must not leak it.
		safeOut := RedactCapturedOutput(out, args...)
		recordCLIFailure(t, invocation, safeOut, err)
		t.Fatalf("%s", cliFailureMessage(invocation, err, safeOut))
	}
	return out
}

// RunCLIInDirWithHome is like MustRunCLIInDirWithHome but returns an error
// instead of failing the test — used for polling where transient failures are OK.
func RunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error) {
	t.Helper()
	return RunBinaryInDirWithHome(t, runlog.DefaultBinaryName, dir, home, args...)
}

// RunBinaryInDirWithHome is the generic non-fatal runner. It never records by
// itself; callers that record the invocation must build it with
// FormatInvocation so it stays redacted.
func RunBinaryInDirWithHome(t *testing.T, binary, dir, home string, args ...string) (string, error) {
	t.Helper()
	out, err, _ := runBinary(t, binary, dir, home, args...)
	return out, err
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers re-implemented here because runlog's versions log raw argv on failure
// ─────────────────────────────────────────────────────────────────────────────

// SetupCLIAuth configures authentication in the isolated home directory for a
// CLI test. Mirrors runlog.SetupCLIAuth, but every invocation it records goes
// through the redacting recorder (`memory set-token <token>` would otherwise
// leak the token if the command failed).
func SetupCLIAuth(t *testing.T, home string) {
	t.Helper()

	srv := ServerURL()

	if AuthMode() == "account" {
		// Write credentials.json so the CLI considers itself authenticated.
		// Do NOT set api_key — that would make the CLI skip credentials.json.
		token := SetToken()
		out := MustRunCLIInDirWithHome(t, "", home, "set-token", token, "--server", srv)
		logCLISuccess(t, FormatInvocation(runlog.DefaultBinaryName, "set-token", token, "--server", srv), out)
		out = MustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
		logCLISuccess(t, FormatInvocation(runlog.DefaultBinaryName, "config", "set", "server_url", srv), out)
		// Some servers don't expose the org-list endpoint for synthetic tokens,
		// so auto-detection fails. Set org_id explicitly when provided.
		if id := OrgID(); id != "" {
			out = MustRunCLIInDirWithHome(t, "", home, "config", "set", "org_id", id)
			logCLISuccess(t, FormatInvocation(runlog.DefaultBinaryName, "config", "set", "org_id", id), out)
		}
	} else {
		// Standalone: server accepts a plain API key via X-API-Key header.
		out := MustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
		logCLISuccess(t, FormatInvocation(runlog.DefaultBinaryName, "config", "set", "server_url", srv), out)
		out = MustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", E2ETestToken())
		logCLISuccess(t, FormatInvocation(runlog.DefaultBinaryName, "config", "set", "api_key", E2ETestToken()), out)
	}
}

// ConfigureProvider configures an LLM provider at the project level via
// `memory provider configure-project`. Mirrors runlog.ConfigureProvider (which
// is not used because it logs the raw `--api-key` value on failure).
func ConfigureProvider(t *testing.T, home, projectID, provider, apiKey, model string) {
	t.Helper()
	args := []string{"provider", "configure-project", provider, "--project", projectID, "--api-key", apiKey}
	if baseURL := runlog.ProviderFromEnvBaseURL(); baseURL != "" {
		args = append(args, "--base-url", baseURL)
	}
	if model != "" {
		args = append(args, "--generative-model", model)
	}
	out := MustRunCLIInDirWithHome(t, "", home, args...)
	t.Logf("provider configure-project %s:\n%s", provider, out)
}

// ConfigureGoogleProvider configures the Google AI provider at the project level.
// Deprecated: use ConfigureProvider(t, home, projectID, "google", apiKey, model).
func ConfigureGoogleProvider(t *testing.T, home, projectID, apiKey, model string) {
	t.Helper()
	ConfigureProvider(t, home, projectID, "google", apiKey, model)
}

// LogSession writes a structured log of the CLI invocation and its output to
// the session log directory, with secret values masked first.
func LogSession(t *testing.T, invocation, output string) {
	t.Helper()
	runlog.LogSession(t, RedactText(invocation), RedactText(output))
}
