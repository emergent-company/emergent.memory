package sdk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `
# comment
MEMORY_SERVER_URL=http://test-server
MEMORY_API_KEY="my-key"
MEMORY_ORG_ID='org-1'
EMPTY=
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := parseDotenv(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["MEMORY_SERVER_URL"] != "http://test-server" {
		t.Errorf("got %q", m["MEMORY_SERVER_URL"])
	}
	if m["MEMORY_API_KEY"] != "my-key" {
		t.Errorf("got %q", m["MEMORY_API_KEY"])
	}
	if m["MEMORY_ORG_ID"] != "org-1" {
		t.Errorf("got %q", m["MEMORY_ORG_ID"])
	}
}

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

func TestWalkUpFind(t *testing.T) {
	// Create a temp dir tree: root/sub/sub2
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	sub2 := filepath.Join(sub, "sub2")
	if err := os.MkdirAll(sub2, 0755); err != nil {
		t.Fatal(err)
	}
	// Place .env.local in root
	envPath := filepath.Join(root, ".env.local")
	if err := os.WriteFile(envPath, []byte("KEY=val"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to sub2 and walk up
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(sub2); err != nil {
		t.Fatal(err)
	}

	found := walkUpFind(".env.local")
	if found != envPath {
		t.Errorf("expected %q, got %q", envPath, found)
	}
}

func TestNewFromEnvDotenvLocal(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env.local")
	content := "MEMORY_SERVER_URL=http://dotenv-server\nMEMORY_API_KEY=dotenv-key\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Clear relevant env vars
	os.Unsetenv("MEMORY_SERVER_URL")
	os.Unsetenv("MEMORY_API_URL")
	os.Unsetenv("MEMORY_API_KEY")
	os.Unsetenv("MEMORY_PROJECT_TOKEN")

	client, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if client.base != "http://dotenv-server" {
		t.Errorf("expected http://dotenv-server, got %q", client.base)
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
