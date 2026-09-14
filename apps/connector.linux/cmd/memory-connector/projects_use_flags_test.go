package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/memoryapi"
)

// writeUseConfig seeds a config that already carries an instance id and a
// disabled-tools list, so `projects use` preservation/override can be observed.
func writeUseConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "memory-connector.yml")
	body := "server_url: https://old.test\n" +
		"token: old-token\n" +
		"instance_id: stable-id\n" +
		"disabled_tools:\n" +
		"  - notes_create\n" +
		"  - reminders_add\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func useFlagsFixture(t *testing.T) (configPath, serverURL string) {
	t.Helper()
	dir := t.TempDir()
	configPath = writeUseConfig(t, dir)
	serverURL = "https://srv.test"
	seedAccount(t, configPath, serverURL)
	api := &fakeProjectAPI{projects: []memoryapi.Project{{ID: "p2", Name: "Two"}}}
	withFakeProjectManager(t, api)
	return configPath, serverURL
}

func TestProjectsUseInstanceAndToolsFlagsOverride(t *testing.T) {
	configPath, serverURL := useFlagsFixture(t)

	code, _, stderr := runProjectsCapture("use", "p2", "--config", configPath, "--server", serverURL,
		"--instance-id", "new-id", "--disabled-tools", "a, b")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InstanceID != "new-id" {
		t.Errorf("InstanceID = %q, want new-id", cfg.InstanceID)
	}
	if !reflect.DeepEqual(cfg.DisabledTools, []string{"a", "b"}) {
		t.Errorf("DisabledTools = %v, want [a b]", cfg.DisabledTools)
	}
}

func TestProjectsUseOmittedFlagsPreserve(t *testing.T) {
	configPath, serverURL := useFlagsFixture(t)

	code, _, stderr := runProjectsCapture("use", "p2", "--config", configPath, "--server", serverURL)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.InstanceID != "stable-id" {
		t.Errorf("InstanceID = %q, want preserved stable-id", cfg.InstanceID)
	}
	if !reflect.DeepEqual(cfg.DisabledTools, []string{"notes_create", "reminders_add"}) {
		t.Errorf("DisabledTools = %v, want preserved list", cfg.DisabledTools)
	}
}

func TestProjectsUseDisabledToolsEmptyClears(t *testing.T) {
	configPath, serverURL := useFlagsFixture(t)

	code, _, stderr := runProjectsCapture("use", "p2", "--config", configPath, "--server", serverURL, "--disabled-tools", "")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.DisabledTools) != 0 {
		t.Errorf("DisabledTools = %v, want cleared", cfg.DisabledTools)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "disabled_tools") {
		t.Errorf("cleared disabled_tools must be omitted from YAML:\n%s", raw)
	}
}
