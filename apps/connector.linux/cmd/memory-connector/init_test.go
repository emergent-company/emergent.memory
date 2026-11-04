package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

// newSessionsHub starts a stub hub for GET /api/mcp-relay/sessions that
// records the Authorization header and returns status.
func newSessionsHub(t *testing.T, status int) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("probe method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/mcp-relay/sessions" {
			t.Errorf("probe path = %s, want /api/mcp-relay/sessions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

func configPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "nested", "config.yml")
}

func runInitCapture(args []string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = runInit(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRunInitSuccess(t *testing.T) {
	srv, gotAuth := newSessionsHub(t, http.StatusOK)
	path := configPath(t)
	code, stdout, stderr := runInitCapture([]string{
		"--config", path,
		"--server-url", srv.URL,
		"--token", "emt_secret",
		"--project-id", "proj-1",
		"--instance-id", "mbp-test-connector",
	})
	if code != 0 {
		t.Fatalf("runInit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if *gotAuth != "Bearer emt_secret" {
		t.Errorf("Authorization = %q, want %q", *gotAuth, "Bearer emt_secret")
	}
	if !strings.Contains(stdout, "ready to relay") {
		t.Errorf("stdout %q should say the connector is ready to relay", stdout)
	}
	if !strings.Contains(stdout, path) {
		t.Errorf("stdout %q should name the config path", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load written config: %v", err)
	}
	if cfg.ServerURL != srv.URL || cfg.Token != "emt_secret" || cfg.ProjectID != "proj-1" || cfg.InstanceID != "mbp-test-connector" {
		t.Errorf("written config = %+v, want server %q token emt_secret project proj-1 instance mbp-test-connector", cfg, srv.URL)
	}
}

func TestRunInitDefaultsInstanceID(t *testing.T) {
	srv, _ := newSessionsHub(t, http.StatusOK)
	path := configPath(t)
	code, _, stderr := runInitCapture([]string{
		"--config", path,
		"--server-url", srv.URL,
		"--token", "emt_secret",
	})
	if code != 0 {
		t.Fatalf("runInit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load written config: %v", err)
	}
	if cfg.InstanceID != config.DefaultInstanceID() {
		t.Errorf("InstanceID = %q, want default %q", cfg.InstanceID, config.DefaultInstanceID())
	}
}

func TestRunInitAuthFailureLeavesNoFile(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newSessionsHub(t, status)
			path := configPath(t)
			code, _, stderr := runInitCapture([]string{
				"--config", path,
				"--server-url", srv.URL,
				"--token", "emt_bad",
			})
			if code != 1 {
				t.Errorf("runInit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "authentication failed") {
				t.Errorf("stderr %q should report an auth failure", stderr)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("config file should be removed after auth failure, stat err = %v", err)
			}
		})
	}
}

func TestRunInitServerErrorLeavesNoFile(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newSessionsHub(t, status)
			path := configPath(t)
			code, _, stderr := runInitCapture([]string{
				"--config", path,
				"--server-url", srv.URL,
				"--token", "emt_x",
			})
			if code != 1 {
				t.Errorf("runInit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "server unreachable") {
				t.Errorf("stderr %q should report the server as unreachable", stderr)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("config file should be removed after server error, stat err = %v", err)
			}
		})
	}
}

func TestRunInitNetworkErrorLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused, deterministic

	path := configPath(t)
	code, _, stderr := runInitCapture([]string{
		"--config", path,
		"--server-url", url,
		"--token", "emt_x",
	})
	if code != 1 {
		t.Errorf("runInit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "server unreachable") {
		t.Errorf("stderr %q should report the server as unreachable", stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("config file should be removed after network error, stat err = %v", err)
	}
}

func TestRunInitMissingRequiredFlags(t *testing.T) {
	srv, _ := newSessionsHub(t, http.StatusOK)
	path := configPath(t)

	cases := []struct {
		name string
		args []string
	}{
		{name: "no token", args: []string{"--config", path, "--server-url", srv.URL}},
		{name: "no server-url", args: []string{"--config", path, "--token", "emt_x"}},
		{name: "nothing", args: []string{"--config", path}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runInitCapture(tc.args)
			if code != 2 {
				t.Errorf("runInit code = %d, want 2", code)
			}
			if !strings.Contains(stderr, "--server-url and --token are required") {
				t.Errorf("stderr %q should explain both flags are required", stderr)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("no config file should be written for missing flags, stat err = %v", err)
			}
		})
	}
}

func TestRunInitFlagParseErrors(t *testing.T) {
	path := configPath(t)

	cases := []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"--config", path, "--nope", "x"}},
		{name: "positional arg", args: []string{"--config", path, "--server-url", "http://x", "--token", "emt_x", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runInitCapture(tc.args)
			if code != 2 {
				t.Errorf("runInit code = %d, want 2", code)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("no config file should be written on parse error, stat err = %v", err)
			}
		})
	}
}
