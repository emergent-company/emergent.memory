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

// TestComposeArgsPassEnvLocalExplicitly locks the #2004 env-file contract: every
// compose invocation must name the installation's single secrets file
// (config/.env.local) explicitly, for install and upgrade alike, and must never
// fall back to a second copy under docker/.
func TestComposeArgsPassEnvLocalExplicitly(t *testing.T) {
	dm := NewDockerManager("/home/user/.memory", &mockOutput{})

	got := dm.composeArgs("up", "-d")
	want := []string{
		"compose",
		"-f", filepath.Join("/home/user/.memory", "docker", "docker-compose.yml"),
		"--env-file", filepath.Join("/home/user/.memory", "config", ".env.local"),
		"up", "-d",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("composeArgs() =\n  %v\nwant\n  %v", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "docker/.env") {
		t.Errorf("compose args must not reference docker/.env: %v", got)
	}
}

// TestUpArgsRemoveOrphans guards the orphan cleanup: the MinIO/Kreuzberg
// containers from older installs are only dropped when compose is told to.
func TestUpArgsRemoveOrphans(t *testing.T) {
	dm := NewDockerManager("/home/user/.memory", &mockOutput{})

	args := dm.composeArgs(dm.upArgs()...)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--remove-orphans") {
		t.Errorf("`compose up` args missing --remove-orphans: %v", args)
	}
	if !strings.Contains(joined, "up -d") {
		t.Errorf("`compose up` args malformed: %v", args)
	}
}

// TestDownArgsRemoveOrphans keeps `memory server uninstall` from leaving legacy
// containers running after the install directory is deleted.
func TestDownArgsRemoveOrphans(t *testing.T) {
	dm := NewDockerManager("/home/user/.memory", &mockOutput{})

	args := dm.composeArgs("down", "--remove-orphans", "-v")
	if !strings.Contains(strings.Join(args, " "), "--remove-orphans") {
		t.Errorf("compose down args missing --remove-orphans: %v", args)
	}
}

// TestFilterLegacyOrphans covers the warning that names what is being removed.
func TestFilterLegacyOrphans(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{
			name:  "no legacy containers",
			names: []string{"memory-db", "memory-server", "memory-seaweedfs", ""},
			want:  nil,
		},
		{
			name:  "full legacy stack after the transition",
			names: []string{"memory-server", "memory-minio", "memory-minio-init", "memory-kreuzberg"},
			want:  []string{"memory-minio", "memory-minio-init", "memory-kreuzberg"},
		},
		{
			name:  "order is stable regardless of docker output order",
			names: []string{"memory-kreuzberg", "memory-minio-init", "memory-minio"},
			want:  []string{"memory-minio", "memory-minio-init", "memory-kreuzberg"},
		},
		{
			name:  "prefix matches are not treated as legacy services",
			names: []string{"memory-minio-backup", "memory-kreuzberg2"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterLegacyOrphans(tt.names)
			if len(got) != len(tt.want) {
				t.Fatalf("filterLegacyOrphans(%v) = %v, want %v", tt.names, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("filterLegacyOrphans(%v) = %v, want %v", tt.names, got, tt.want)
				}
			}
		})
	}
}

// TestSeaweedFSHealthcheckRequiresS3Listener guards #2004: the master endpoint
// is green (`{}`) before raft election and before the S3 gateway listens on
// 8333, and anonymous S3 GETs answer 403 (so `wget --spider` cannot be used).
// The probe must therefore check leader election AND that 8333 accepts.
func TestSeaweedFSHealthcheckRequiresS3Listener(t *testing.T) {
	template := GetDockerComposeTemplate()

	start := strings.Index(template, "\n  seaweedfs:\n")
	if start < 0 {
		t.Fatal("seaweedfs service not found in docker-compose template")
	}
	next := strings.Index(template[start+1:], "\n\n  ")
	if next < 0 {
		t.Fatal("could not delimit the seaweedfs service block")
	}
	block := template[start : start+1+next]

	var testLine string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "test:") {
			testLine = line
			break
		}
	}
	if testLine == "" {
		t.Fatalf("seaweedfs healthcheck has no test line:\n%s", block)
	}

	if !strings.Contains(testLine, "nc -z 127.0.0.1 8333") {
		t.Errorf("seaweedfs healthcheck must prove the S3 port is accepting: %s", testLine)
	}
	if !strings.Contains(testLine, "cluster/status") || !strings.Contains(testLine, "IsLeader") {
		t.Errorf("seaweedfs healthcheck must prove raft leadership: %s", testLine)
	}
	if strings.Contains(testLine, "--spider") {
		t.Errorf("seaweedfs healthcheck must not rely on a 2xx-only spider probe: %s", testLine)
	}
	if strings.Contains(block, "interval: 30s") {
		t.Errorf("seaweedfs healthcheck interval must be short so storage-init starts promptly:\n%s", block)
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
		PostgresImage,
		"ghcr.io/xberg-io/xberg:1.3.0",
		ObjectStoreImage,
		"seaweedfs:",
		"storage-init:",
		StorageInitEntrypoint,
		"http://seaweedfs:8333",
		"http://127.0.0.1:9333/cluster/status",
		"object_store_data",
		"xberg:",
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

// TestDockerComposeDbShmSize guards #2003: Docker's default /dev/shm is 64 MB,
// but PostgreSQL allocates ~265 MB of shared memory when building the HNSW index
// in migration 00164 with parallel workers, and fails with SQLSTATE 53100.
func TestDockerComposeDbShmSize(t *testing.T) {
	template := GetDockerComposeTemplate()

	dbStart := strings.Index(template, "\n  db:\n")
	if dbStart < 0 {
		t.Fatal("db service not found in docker-compose template")
	}
	nextService := strings.Index(template[dbStart+1:], "\n\n  ")
	if nextService < 0 {
		t.Fatal("could not delimit the db service block")
	}
	block := template[dbStart : dbStart+1+nextService]

	if !containsString(block, "shm_size: 1gb") {
		t.Errorf("db service must set shm_size: 1gb for the HNSW index migration, got:\n%s", block)
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
