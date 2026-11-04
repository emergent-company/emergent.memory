package e2eframework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunlogRedactsSecretFlags is the non-vacuous guard for #2428: a command
// carrying every secret-bearing shape is recorded through the harness and the
// sentinel must be absent from both the stored record (run_events in runs.db)
// and the rendered step output (run.log), while a non-secret flag value
// survives in both.
func TestRunlogRedactsSecretFlags(t *testing.T) {
	// Isolate the harness side effects: flat log files and SQLite.
	dir := t.TempDir()
	t.Setenv("TEST_LOG_DIR", filepath.Join(dir, "logs"))
	t.Setenv("_RUNLOG_DAEMON_DB", filepath.Join(dir, "runs.db"))

	const sentinel = "sk-TEST-REDACT-ME"
	const safeValue = "openai/gpt-5-non-secret"

	rl := NewRunLog(t)
	defer rl.Close()

	// Every shape the mask must cover: `--flag value`, `--flag=value`, the
	// `-p` short form, `--client-secret`, and an Authorization header value.
	args := []string{
		"provider", "configure", "openai",
		"--api-key", sentinel,
		"--token=" + sentinel,
		"-p", sentinel,
		"--password=" + sentinel,
		"--client-secret", sentinel,
		"-H", "Authorization: Bearer " + sentinel,
		"--model", safeValue,
	}

	// Execute through the same runner the Must* helpers use. The stub echoes
	// its argv *and* the raw secret on its own, so both the flag-shape mask
	// and the captured-output scrub are exercised. `exit 3` drives the
	// failure-recording path.
	home := t.TempDir()
	const script = `echo "$@"; printf '%s\n' "$4"; exit 3`
	out, runErr, invocation := runBinary(t, "sh", "", home, append([]string{"-c", script}, args...)...)
	if runErr == nil {
		t.Fatal("expected the stub command to fail")
	}
	if !strings.Contains(out, sentinel) {
		t.Fatalf("stub output should contain the raw sentinel to make the scrub non-vacuous, got:\n%s", out)
	}
	safeOut := RedactCapturedOutput(out, args...)
	if strings.Contains(safeOut, sentinel) {
		t.Errorf("captured output scrub leaked sentinel %q:\n%s", sentinel, safeOut)
	}
	if strings.Contains(invocation, sentinel) {
		t.Errorf("redacted invocation still contains sentinel %q: %s", sentinel, invocation)
	}
	if !strings.Contains(invocation, safeValue) {
		t.Errorf("redacted invocation dropped non-secret value %q: %s", safeValue, invocation)
	}
	for _, want := range []string{"--api-key " + RedactedValue, "--token=" + RedactedValue, "--client-secret " + RedactedValue} {
		if !strings.Contains(invocation, want) {
			t.Errorf("redacted invocation missing %q: %s", want, invocation)
		}
	}
	if strings.Contains(invocation, "Bearer") {
		t.Errorf("Authorization header value was not masked: %s", invocation)
	}

	// The failure path records the event and builds the t.Fatalf message from
	// the same redacted invocation and scrubbed output.
	recordCLIFailure(t, invocation, safeOut, runErr)

	failureMsg := cliFailureMessage(invocation, runErr, safeOut)
	if strings.Contains(failureMsg, sentinel) {
		t.Errorf("rendered failure message leaked sentinel %q:\n%s", sentinel, failureMsg)
	}

	// Rendered step output: run.log written by RunLog.CLIStepErr.
	logBytes, err := os.ReadFile(filepath.Join(rl.Dir(), "run.log"))
	if err != nil {
		t.Fatalf("read run.log: %v", err)
	}
	logText := string(logBytes)
	if strings.Contains(logText, sentinel) {
		t.Errorf("rendered run.log leaked sentinel %q", sentinel)
	}
	if !strings.Contains(logText, safeValue) {
		t.Errorf("rendered run.log dropped non-secret value %q", safeValue)
	}

	// Stored record: run_events rows in runs.db.
	db, err := SharedDB()
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	rows, err := db.RawDB().Query(`SELECT COALESCE(message, '') || ' ' || COALESCE(details, '') FROM run_events`)
	if err != nil {
		t.Fatalf("query run_events: %v", err)
	}
	defer rows.Close()

	sawSafeValue := false
	for rows.Next() {
		var stored string
		if err := rows.Scan(&stored); err != nil {
			t.Fatalf("scan run_events: %v", err)
		}
		if strings.Contains(stored, sentinel) {
			t.Errorf("stored run_events row leaked sentinel %q: %s", sentinel, stored)
		}
		if strings.Contains(stored, safeValue) {
			sawSafeValue = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate run_events: %v", err)
	}
	if !sawSafeValue {
		t.Errorf("stored run_events dropped non-secret value %q", safeValue)
	}

	// Shape-level table: non-secret flags must be left alone.
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "flag value",
			in:   []string{"--api-key", sentinel},
			want: []string{"--api-key", RedactedValue},
		},
		{
			name: "flag equals",
			in:   []string{"--token=" + sentinel},
			want: []string{"--token=" + RedactedValue},
		},
		{
			name: "short password",
			in:   []string{"-p", sentinel},
			want: []string{"-p", RedactedValue},
		},
		{
			name: "client secret",
			in:   []string{"--client-secret", sentinel},
			want: []string{"--client-secret", RedactedValue},
		},
		{
			name: "authorization header",
			in:   []string{"-H", "Authorization: Bearer " + sentinel},
			want: []string{"-H", "Authorization: ***"},
		},
		{
			name: "non-secret untouched",
			in:   []string{"--model", safeValue},
			want: []string{"--model", safeValue},
		},
		{
			name: "token id is not a secret flag",
			in:   []string{"--token-id", "abc"},
			want: []string{"--token-id", "abc"},
		},
	} {
		got := RedactArgs(tc.in...)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: RedactArgs(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
