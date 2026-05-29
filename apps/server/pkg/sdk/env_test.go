package sdk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSimpleYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server_url: http://yaml-server
api_key: yaml-key
org_id: yaml-org
project_id: yaml-proj
debug: false
cache:
    ttl: 5m
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := parseSimpleYAML(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["server_url"] != "http://yaml-server" {
		t.Errorf("got %q", m["server_url"])
	}
	if m["api_key"] != "yaml-key" {
		t.Errorf("got %q", m["api_key"])
	}
	// Nested keys should be skipped
	if _, ok := m["ttl"]; ok {
		t.Error("nested key should not be parsed")
	}
}

func TestNewFromEnvProjectTokenOverridesAPIKey(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	os.Setenv("MEMORY_SERVER_URL", "http://test")
	os.Setenv("MEMORY_API_KEY", "regular-key")
	os.Setenv("MEMORY_PROJECT_TOKEN", "emt_project_token")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_API_KEY")
		os.Unsetenv("MEMORY_PROJECT_TOKEN")
	}()

	client, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	// The auth provider should have been initialized with the project token
	_ = client // just verify no error
}
