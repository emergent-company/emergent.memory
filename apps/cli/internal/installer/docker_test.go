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
		ObjectStoreImage,
		"seaweedfs:",
		"storage-init:",
		StorageInitEntrypoint,
		"http://seaweedfs:8333",
		"http://127.0.0.1:9333/cluster/status",
		"object_store_data",
		"kreuzberg:",
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

// TestObjectStoreImageIsPinnedByDigest guards the replacement of the archived
// MinIO images (issue #23) with a maintained, freely pullable backend. The
// object-store image must be pinned by digest and the bucket bootstrap must run
// from the server image, not a MinIO client image.
func TestObjectStoreImageIsPinnedByDigest(t *testing.T) {
	if !strings.HasPrefix(ObjectStoreImage, "chrislusf/seaweedfs@sha256:") {
		t.Fatalf("ObjectStoreImage = %q: must be a digest-pinned chrislusf/seaweedfs image", ObjectStoreImage)
	}
	digest := strings.TrimPrefix(ObjectStoreImage, "chrislusf/seaweedfs@sha256:")
	if len(digest) != 64 {
		t.Errorf("ObjectStoreImage digest length = %d, want 64", len(digest))
	}
	if _, err := hex.DecodeString(digest); err != nil {
		t.Errorf("ObjectStoreImage digest is not valid hex: %v", err)
	}

	if StorageInitImage != ServerImageRepo {
		t.Errorf("StorageInitImage = %q, want the server image repo %q", StorageInitImage, ServerImageRepo)
	}
	if StorageInitEntrypoint != "/usr/local/bin/emergent-storage-init" {
		t.Errorf("StorageInitEntrypoint = %q, want /usr/local/bin/emergent-storage-init", StorageInitEntrypoint)
	}

	template := GetDockerComposeTemplate()
	if !strings.Contains(template, ObjectStoreImage) {
		t.Errorf("docker-compose template does not pin ObjectStoreImage %q", ObjectStoreImage)
	}
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image: ghcr.io/emergent-company/minio") ||
			strings.HasPrefix(trimmed, "image: minio/") {
			t.Errorf("docker-compose template still references a MinIO image: %q", trimmed)
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
