package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDaemonCapture(args []string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = runDaemon(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRunDaemonMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.yml")
	code, _, stderr := runDaemonCapture([]string{"--config", path})
	if code != 1 {
		t.Fatalf("runDaemon code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "memory-connector daemon:") || !strings.Contains(stderr, "no config at") {
		t.Errorf("stderr = %q, want a daemon-prefixed missing-config error", stderr)
	}
}

func TestRunDaemonInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("server_url: https://x.test\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code, _, stderr := runDaemonCapture([]string{"--config", path})
	if code != 1 {
		t.Fatalf("runDaemon code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "memory-connector daemon:") || !strings.Contains(stderr, "token") {
		t.Errorf("stderr = %q, want a daemon-prefixed token validation error", stderr)
	}
}

func TestRunDaemonFlagAndArgErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"--bogus"}},
		{name: "positional arg", args: []string{"extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runDaemonCapture(tc.args)
			if code != 2 {
				t.Errorf("runDaemon code = %d, want 2", code)
			}
		})
	}
}

func TestRunDaemonAlreadyRunning(t *testing.T) {
	// Hold the lock the daemon will try to take, then confirm it refuses to run.
	path := filepath.Join(t.TempDir(), "config.yml")
	release, err := acquireLock(path + ".lock")
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer release()

	code, _, stderr := runDaemonCapture([]string{"--config", path})
	if code != 1 {
		t.Fatalf("runDaemon code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "already running") || !strings.Contains(stderr, "lock held") {
		t.Errorf("stderr = %q, want an already-running message naming the lock", stderr)
	}
}

func TestRunDaemonReleasesLockOnFailure(t *testing.T) {
	// A daemon that exits on a bad config must drop its lock so a later attempt
	// can acquire it.
	path := filepath.Join(t.TempDir(), "config.yml")
	if code, _, _ := runDaemonCapture([]string{"--config", path}); code != 1 {
		t.Fatalf("runDaemon code = %d, want 1", code)
	}
	release, err := acquireLock(path + ".lock")
	if err != nil {
		t.Fatalf("lock was not released after daemon exit: %v", err)
	}
	release()
}
