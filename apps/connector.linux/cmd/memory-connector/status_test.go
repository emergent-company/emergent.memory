package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/appletools"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
	"github.com/emergent-company/memory.web-ui/connector/internal/tools"
)

const sessionsBody = `{"sessions":[{"instance_id":"mbp-1","version":"0.1.0","tool_count":2,"connected_at":"2026-09-09T10:00:00Z"},{"instance_id":"other-host","tool_count":1}]}`

// sessionsHub serves GET /api/mcp-relay/sessions with the given status/body.
func sessionsHub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp-relay/sessions" {
			t.Errorf("path = %s, want /api/mcp-relay/sessions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeStatusConfig(t *testing.T, serverURL, instanceID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	body := "server_url: " + serverURL + "\n" +
		"token: emt_status\n" +
		"instance_id: " + instanceID + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// fakeStatusTools returns a deterministic local tool set for status tests,
// independent of the host platform.
func fakeStatusTools(names ...string) *tools.Set {
	ts := make([]toolreg.Tool, 0, len(names))
	for _, name := range names {
		ts = append(ts, toolreg.Tool{
			Name:        name,
			Description: "fake " + name,
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     func(context.Context, map[string]any) (map[string]any, error) { return map[string]any{}, nil },
		})
	}
	return &tools.Set{Tools: ts}
}

// fakeStatusProvider adapts a fixed Set to the status provider seam.
func fakeStatusProvider(set *tools.Set) func(*config.Config) (*tools.Set, error) {
	return func(*config.Config) (*tools.Set, error) { return set, nil }
}

func withStatusTools(provider func(*config.Config) (*tools.Set, error)) func() {
	old := statusToolsProvider
	statusToolsProvider = provider
	return func() { statusToolsProvider = old }
}

func runStatusCapture(args []string) (code int, stdout, stderr string) {
	var out, errBuf strings.Builder
	code = runStatus(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestStatusConnectedMatch(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, sessionsBody)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a", "b")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, stderr := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"memory-connector status",
		"instance: mbp-1",
		"version: 0.1.0",
		"tools (2): a, b",
		"hub: connected",
		"matches local set",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "mismatch") {
		t.Errorf("stdout should not flag a mismatch:\n%s", stdout)
	}
}

func TestStatusMismatchFlagged(t *testing.T) {
	body := `{"sessions":[{"instance_id":"mbp-1","tool_count":1}]}`
	srv := sessionsHub(t, http.StatusOK, body)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a", "b")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, _ := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "hub: connected") || !strings.Contains(stdout, "mismatch") {
		t.Errorf("stdout should report connected with mismatch:\n%s", stdout)
	}
}

func TestStatusNotConnected(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a", "b")))()
	path := writeStatusConfig(t, srv.URL, "somewhere-else")

	code, stdout, _ := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (not connected is a valid state)", code)
	}
	if !strings.Contains(stdout, "hub: not connected") {
		t.Errorf("stdout should say not connected:\n%s", stdout)
	}
	if !strings.Contains(stdout, "tools (2): a, b") {
		t.Errorf("stdout should still print local tools:\n%s", stdout)
	}
}

func TestStatusAuthFailed(t *testing.T) {
	srv := sessionsHub(t, http.StatusUnauthorized, "")
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, _ := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "hub: authentication failed") {
		t.Errorf("stdout should report auth failure:\n%s", stdout)
	}
	if !strings.Contains(stdout, "tools (1): a") {
		t.Errorf("stdout should still print local tools:\n%s", stdout)
	}
}

func TestStatusServerErrorAndNetwork(t *testing.T) {
	t.Run("500", func(t *testing.T) {
		srv := sessionsHub(t, http.StatusInternalServerError, "")
		defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()
		path := writeStatusConfig(t, srv.URL, "mbp-1")
		code, stdout, _ := runStatusCapture([]string{"--config", path})
		if code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stdout, "hub: unreachable") {
			t.Errorf("stdout should report unreachable:\n%s", stdout)
		}
	})

	t.Run("network", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()
		path := writeStatusConfig(t, url, "mbp-1")
		code, stdout, _ := runStatusCapture([]string{"--config", path})
		if code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stdout, "hub: unreachable") {
			t.Errorf("stdout should report unreachable:\n%s", stdout)
		}
	})
}

func TestStatusPlatformNote(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[{"instance_id":"mbp-1","tool_count":0}]}`)
	note := appletools.PlatformNote
	defer withStatusTools(fakeStatusProvider(&tools.Set{Note: note}))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, _ := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "tools: none ("+note+")") {
		t.Errorf("stdout should print the platform note:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hub: connected") {
		t.Errorf("stdout should still report hub state:\n%s", stdout)
	}
}

func TestStatusMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yml")
	code, _, stderr := runStatusCapture([]string{"--config", path})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "memory-connector init") {
		t.Errorf("stderr %q should point at init", stderr)
	}
}

func TestStatusFlagErrors(t *testing.T) {
	code, _, _ := runStatusCapture([]string{"--bogus", "x"})
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	code, _, _ = runStatusCapture([]string{"extra"})
	if code != 2 {
		t.Errorf("code = %d, want 2 for positional arg", code)
	}
}

func TestStatusExcludesDisabledTools(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a", "b")))()

	path := filepath.Join(t.TempDir(), "config.yml")
	body := "server_url: " + srv.URL + "\n" +
		"token: emt_status\n" +
		"instance_id: mbp-1\n" +
		"disabled_tools:\n" +
		"  - b\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	code, stdout, stderr := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "tools (1): a") {
		t.Errorf("stdout should list only the enabled tool:\n%s", stdout)
	}
	for _, want := range []string{", b", "tools (2)"} {
		if strings.Contains(stdout, want) {
			t.Errorf("stdout must not include disabled tool b (%q):\n%s", want, stdout)
		}
	}
}

func TestStatusReportsDisabledTools(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[]}`)
	set := fakeStatusTools("linux-host-info")
	set.Note = "filesystem tools disabled: no filesystem config"
	set.Disabled = []tools.DisabledTool{
		{Name: "linux-fs-list", Reason: "no filesystem config"},
		{Name: "linux-fs-read", Reason: "no filesystem config"},
	}
	defer withStatusTools(fakeStatusProvider(set))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, stderr := runStatusCapture([]string{"--config", path})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"tools (1): linux-host-info",
		"tools-disabled: linux-fs-list (no filesystem config), linux-fs-read (no filesystem config)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	code, stdout, stderr = runStatusCapture([]string{"--config", path, "--json"})
	if code != 0 {
		t.Fatalf("json code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		DisabledTools []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"disabled_tools"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", doc.SchemaVersion)
	}
	if len(doc.DisabledTools) != 2 || doc.DisabledTools[0].Name != "linux-fs-list" || doc.DisabledTools[0].Reason != "no filesystem config" {
		t.Errorf("disabled_tools = %+v, want the two fs tools with reasons", doc.DisabledTools)
	}
}
