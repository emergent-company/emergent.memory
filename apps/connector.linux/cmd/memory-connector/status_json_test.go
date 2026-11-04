package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusJSONGolden(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[{"instance_id":"mbp-1","tool_count":2}]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a", "b")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, stderr := runStatusCapture([]string{"--config", path, "--json"})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{
  "schema_version": 1,
  "instance_id": "mbp-1",
  "version": "0.1.0",
  "tools": [
    "a",
    "b"
  ],
  "hub_state": "connected",
  "hub_detail": {
    "hub_tool_count": 2,
    "local_tool_count": 2,
    "session_count": 0
  }
}
`
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant:\n%s", stdout, want)
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON:\n%s", stdout)
	}
}

func TestStatusJSONAuthFailed(t *testing.T) {
	srv := sessionsHub(t, http.StatusUnauthorized, "")
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, _ := runStatusCapture([]string{"--config", path, "--json"})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		HubState      string `json:"hub_state"`
		InstanceID    string `json:"instance_id"`
		Version       string `json:"version"`
		Tools         []string
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", doc.SchemaVersion)
	}
	if doc.HubState != "auth_failed" {
		t.Errorf("hub_state = %q, want auth_failed", doc.HubState)
	}
	if doc.InstanceID != "mbp-1" || doc.Version != version {
		t.Errorf("instance/version = %q/%q", doc.InstanceID, doc.Version)
	}
	if len(doc.Tools) != 1 || doc.Tools[0] != "a" {
		t.Errorf("tools = %v, want [a]", doc.Tools)
	}
}

func TestStatusJSONIncludesProject(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[{"instance_id":"mbp-1","tool_count":1}]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()

	path := filepath.Join(t.TempDir(), "config.yml")
	body := "server_url: " + srv.URL + "\n" +
		"token: emt_status\n" +
		"instance_id: mbp-1\n" +
		"project_id: 9f1c1a4b-9d11-4a2e-9a6d-1234567890ab\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	code, stdout, stderr := runStatusCapture([]string{"--config", path, "--json"})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		Project *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.Project == nil {
		t.Fatalf("project missing from JSON:\n%s", stdout)
	}
	if doc.Project.ID != "9f1c1a4b-9d11-4a2e-9a6d-1234567890ab" {
		t.Errorf("project.id = %q", doc.Project.ID)
	}
}

func TestStatusJSONOmitsProjectWhenUnset(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[{"instance_id":"mbp-1","tool_count":1}]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()
	path := writeStatusConfig(t, srv.URL, "mbp-1")

	code, stdout, stderr := runStatusCapture([]string{"--config", path, "--json"})
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, `"project"`) {
		t.Errorf("project must be omitted when unset:\n%s", stdout)
	}
}

func TestStatusTextUnchangedWithProject(t *testing.T) {
	srv := sessionsHub(t, http.StatusOK, `{"sessions":[{"instance_id":"mbp-1","tool_count":1}]}`)
	defer withStatusTools(fakeStatusProvider(fakeStatusTools("a")))()

	basePath := writeStatusConfig(t, srv.URL, "mbp-1")
	projPath := filepath.Join(t.TempDir(), "config.yml")
	body := "server_url: " + srv.URL + "\n" +
		"token: emt_status\n" +
		"instance_id: mbp-1\n" +
		"project_id: p-1\n"
	if err := os.WriteFile(projPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, baseText, _ := runStatusCapture([]string{"--config", basePath})
	_, projText, _ := runStatusCapture([]string{"--config", projPath})
	if baseText != projText {
		t.Errorf("text output changed with project_id set:\nbase:\n%s\nwith project:\n%s", baseText, projText)
	}
	if strings.Contains(projText, "project") {
		t.Errorf("text output must not mention project:\n%s", projText)
	}
}

func TestStatusJSONMissingConfigExitsAndPrintsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yml")
	code, stdout, stderr := runStatusCapture([]string{"--config", path, "--json"})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on missing config (no partial JSON)", stdout)
	}
	if !strings.Contains(stderr, "memory-connector init") {
		t.Errorf("stderr = %q, want a pointer to init", stderr)
	}
}
