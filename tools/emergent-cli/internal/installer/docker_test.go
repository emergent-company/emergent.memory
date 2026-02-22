package installer

import (
	"path/filepath"
	"testing"
)

func TestNewDockerManager(t *testing.T) {
	mock := &mockOutput{}
	dm := NewDockerManager("/tmp/test", mock)

	if dm == nil {
		t.Fatal("NewDockerManager returned nil")
	}
	if dm.installDir != "/tmp/test" {
		t.Errorf("expected installDir /tmp/test, got %s", dm.installDir)
	}
}

func TestDockerManagerPaths(t *testing.T) {
	mock := &mockOutput{}
	dm := NewDockerManager("/home/user/.emergent", mock)

	expectedCompose := filepath.Join("/home/user/.emergent", "docker", "docker-compose.yml")
	if got := dm.composePath(); got != expectedCompose {
		t.Errorf("composePath: expected %s, got %s", expectedCompose, got)
	}

	expectedEnv := filepath.Join("/home/user/.emergent", "config", ".env.local")
	if got := dm.envPath(); got != expectedEnv {
		t.Errorf("envPath: expected %s, got %s", expectedEnv, got)
	}
}

func TestGetDockerComposeTemplate(t *testing.T) {
	template := GetDockerComposeTemplate()

	if template == "" {
		t.Fatal("GetDockerComposeTemplate returned empty string")
	}

	requiredStrings := []string{
		"services:",
		"db:",
		"pgvector/pgvector:pg17",
		"kreuzberg:",
		"minio:",
		"minio-init:",
		"server:",
		"emergent-server",
		"volumes:",
		"networks:",
		"STANDALONE_MODE",
		"POSTGRES_HOST: db",
	}

	for _, s := range requiredStrings {
		if !containsString(template, s) {
			t.Errorf("docker-compose template missing: %s", s)
		}
	}
}

func TestGetInitSQLTemplate(t *testing.T) {
	template := GetInitSQLTemplate()

	if template == "" {
		t.Fatal("GetInitSQLTemplate returned empty string")
	}

	requiredStrings := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		"app_rls",
		"NOLOGIN",
	}

	for _, s := range requiredStrings {
		if !containsString(template, s) {
			t.Errorf("init.sql template missing: %s", s)
		}
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 &&
		(haystack == needle || len(haystack) > len(needle) &&
			(haystack[:len(needle)] == needle ||
				containsStringHelper(haystack, needle)))
}

func containsStringHelper(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
