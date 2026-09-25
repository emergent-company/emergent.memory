package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
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
	dm := NewDockerManager("/home/user/.memory", mock)

	expectedCompose := filepath.Join("/home/user/.memory", "docker", "docker-compose.yml")
	if got := dm.composePath(); got != expectedCompose {
		t.Errorf("composePath: expected %s, got %s", expectedCompose, got)
	}

	expectedEnv := filepath.Join("/home/user/.memory", "config", ".env.local")
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
		"ghcr.io/kreuzberg-dev/kreuzberg-full:4.10.3",
		"ghcr.io/emergent-company/minio:RELEASE.2025-09-07T16-13-09Z",
		"ghcr.io/emergent-company/minio-mc:RELEASE.2025-08-13T08-35-41Z",
		"kreuzberg:",
		"minio:",
		"minio-init:",
		"server:",
		"memory-server",
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

// TestMinioImagesUseOwnedRegistry guards against the regression that broke every
// fresh install and every `memory server upgrade`: MinIO withdrew the
// minio/minio and minio/mc repositories from Docker Hub, and later the
// quay.io/minio images too, so any compose file generated with those image
// references fails to pull. We now serve the pinned images from our own GHCR
// mirror.
func TestMinioImagesUseOwnedRegistry(t *testing.T) {
	images := map[string]string{
		"MinioImage":       MinioImage,
		"MinioClientImage": MinioClientImage,
	}

	for name, image := range images {
		if !strings.HasPrefix(image, "ghcr.io/emergent-company/") {
			t.Errorf("%s = %q: must use the owned ghcr.io/emergent-company registry", name, image)
		}
		if strings.HasSuffix(image, ":latest") {
			t.Errorf("%s = %q: must be pinned to an explicit RELEASE tag, not :latest", name, image)
		}
		if !strings.Contains(image, ":RELEASE.") {
			t.Errorf("%s = %q: expected a MinIO RELEASE.* tag", name, image)
		}
	}

	template := GetDockerComposeTemplate()
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image: minio/") ||
			strings.HasPrefix(trimmed, "image: quay.io/minio/") {
			t.Errorf("docker-compose template still references a withdrawn registry image: %q", trimmed)
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

func TestDockerfileContentHash(t *testing.T) {
	h1 := dockerfileContentHash("a")
	h2 := dockerfileContentHash("b")

	// Both must be non-empty 64-char lowercase hex strings.
	for name, h := range map[string]string{"a": h1, "b": h2} {
		if len(h) != 64 {
			t.Errorf("hash of %q: expected 64 chars, got %d", name, len(h))
		}
		if _, err := hex.DecodeString(h); err != nil {
			t.Errorf("hash of %q is not valid hex: %v", name, err)
		}
	}

	// Different inputs must produce different hashes.
	if h1 == h2 {
		t.Errorf("expected different hashes for different inputs, got %s", h1)
	}

	// Empty input must equal sha256 of empty input and be deterministic.
	empty := dockerfileContentHash("")
	want := sha256.Sum256([]byte(""))
	if empty != hex.EncodeToString(want[:]) {
		t.Errorf("hash of empty input: expected %s, got %s", hex.EncodeToString(want[:]), empty)
	}
	if dockerfileContentHash("") != empty {
		t.Errorf("hash of empty input not deterministic")
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
