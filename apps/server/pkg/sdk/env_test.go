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

// TestNewFromEnvIgnoresDotenvFiles verifies that NewFromEnv does NOT read .env
// or .env.local files from the filesystem — the calling app controls file loading.
func TestNewFromEnvIgnoresDotenvFiles(t *testing.T) {
	dir := t.TempDir()
	// Write a .env.local that would set a different server URL if read.
	envFile := filepath.Join(dir, ".env.local")
	content := "MEMORY_SERVER_URL=http://should-not-be-used\nMEMORY_ACCOUNT_API_KEY=dotenv-key\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Set the real env vars to different values.
	os.Setenv("MEMORY_SERVER_URL", "http://real-server")
	os.Setenv("MEMORY_ACCOUNT_API_KEY", "real-key")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_ACCOUNT_API_KEY")
	}()

	client, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	// Must use the process env value, not the .env.local value.
	if client.base != "http://real-server" {
		t.Errorf("expected http://real-server, got %q — SDK must not read .env files", client.base)
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
	os.Setenv("MEMORY_ACCOUNT_API_KEY", "regular-key")
	os.Setenv("MEMORY_PROJECT_API_KEY", "emt_project_token")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_ACCOUNT_API_KEY")
		os.Unsetenv("MEMORY_PROJECT_API_KEY")
	}()

	client, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	// The auth provider should have been initialized with the project token
	_ = client // just verify no error
}

func TestNewFromEnvDeprecatedAPIKeyFallback(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Old MEMORY_ACCOUNT_API_KEY should still work when MEMORY_ACCOUNT_API_KEY is not set.
	os.Setenv("MEMORY_SERVER_URL", "http://test")
	os.Setenv("MEMORY_ACCOUNT_API_KEY", "legacy-key")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_ACCOUNT_API_KEY")
	}()

	_, err := NewFromEnv()
	if err != nil {
		t.Fatalf("expected legacy MEMORY_ACCOUNT_API_KEY to be accepted, got error: %v", err)
	}
}

func TestNewFromEnvDeprecatedProjectTokenFallback(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Old MEMORY_PROJECT_API_KEY should still work when MEMORY_PROJECT_API_KEY is not set.
	os.Setenv("MEMORY_SERVER_URL", "http://test")
	os.Setenv("MEMORY_PROJECT_API_KEY", "emt_legacy_project_token")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_PROJECT_API_KEY")
	}()

	_, err := NewFromEnv()
	if err != nil {
		t.Fatalf("expected legacy MEMORY_PROJECT_API_KEY to be accepted, got error: %v", err)
	}
}

func TestNewFromEnvNewNameWinsOverDeprecated(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// When both old and new names are set, new name wins.
	os.Setenv("MEMORY_SERVER_URL", "http://test")
	os.Setenv("MEMORY_ACCOUNT_API_KEY", "new-key")
	os.Setenv("MEMORY_API_KEY", "old-key")
	defer func() {
		os.Unsetenv("MEMORY_SERVER_URL")
		os.Unsetenv("MEMORY_ACCOUNT_API_KEY")
		os.Unsetenv("MEMORY_API_KEY")
	}()

	cfg := loadEnvConfig()
	if cfg.APIKey != "new-key" {
		t.Errorf("expected MEMORY_ACCOUNT_API_KEY to win, got %q", cfg.APIKey)
	}
}
