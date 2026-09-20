package backups

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

const testProjectID = "11111111-1111-1111-1111-111111111111"

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newTestImporter() *Importer {
	return &Importer{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// buildTestArchive assembles a synthetic backup archive in memory, mirroring
// the Creator's checksum contract. `mutate`, when set, runs on the manifest
// before the self-checksum is computed, so it can corrupt a checksum or the
// project identity while keeping the manifest self-checksum consistent.
func buildTestArchive(t *testing.T, projectID, projectName, configID string, includeConfig, includeManifest bool, mutate func(*Manifest)) []byte {
	t.Helper()

	dbPayload := []byte(`{"id":"doc-1","project_id":"` + projectID + `"}` + "\n")
	filePayload := []byte("hello file")

	manifest := Manifest{
		Version:       "1.0.0",
		SchemaVersion: "20260211_000000",
		CreatedAt:     time.Now(),
		BackupType:    BackupTypeFull,
		Project: ProjectInfo{
			ID:             projectID,
			Name:           projectName,
			OrganizationID: "99999999-9999-9999-9999-999999999999",
		},
		Contents: BackupStats{Documents: 1},
		Files: map[string]FileEntry{
			"myfile.txt": {Filename: "myfile.txt"},
		},
		Checksums: Checksums{
			Database: sha256Hex(dbPayload),
			Files:    sha256Hex(filePayload),
		},
		Metadata: map[string]any{
			"include_chat":    true,
			"include_journal": false,
		},
	}

	if mutate != nil {
		mutate(&manifest)
	}

	// Manifest self-checksum: compact marshal with Checksums.Manifest cleared.
	payload := manifest
	payload.Checksums.Manifest = ""
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	sum := sha256.Sum256(payloadBytes)
	manifest.Checksums.Manifest = hex.EncodeToString(sum[:])

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if includeManifest {
		w, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatalf("create manifest entry: %v", err)
		}
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Fatalf("encode manifest: %v", err)
		}
	}

	if includeConfig {
		w, err := zw.Create("project/config.json")
		if err != nil {
			t.Fatalf("create config entry: %v", err)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": configID, "name": projectName}); err != nil {
			t.Fatalf("encode config: %v", err)
		}
	}

	dw, err := zw.Create("database/foo.ndjson")
	if err != nil {
		t.Fatalf("create database entry: %v", err)
	}
	if _, err := dw.Write(dbPayload); err != nil {
		t.Fatalf("write database entry: %v", err)
	}

	fw, err := zw.Create("files/myfile.txt")
	if err != nil {
		t.Fatalf("create files entry: %v", err)
	}
	if _, err := fw.Write(filePayload); err != nil {
		t.Fatalf("write files entry: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestParseValidArchive(t *testing.T) {
	data := buildTestArchive(t, testProjectID, "Test Project", testProjectID, true, true, nil)

	archive, err := newTestImporter().Parse(data)
	if err != nil {
		t.Fatalf("Parse valid archive: %v", err)
	}
	if got := archive.Manifest().Project.ID; got != testProjectID {
		t.Errorf("project id = %q, want %q", got, testProjectID)
	}
	if !archive.HasTable("foo") {
		t.Error("expected database table foo")
	}
	if got := len(archive.Files()); got != 1 {
		t.Errorf("files = %d, want 1", got)
	}
}

func TestParseRejections(t *testing.T) {
	tests := []struct {
		name            string
		projectID       string
		configID        string
		includeConfig   bool
		includeManifest bool
		mutate          func(*Manifest)
	}{
		{
			name:            "bad database checksum",
			projectID:       testProjectID,
			configID:        testProjectID,
			includeConfig:   true,
			includeManifest: true,
			mutate: func(m *Manifest) {
				m.Checksums.Database = "deadbeef"
			},
		},
		{
			name:            "missing manifest",
			projectID:       testProjectID,
			configID:        testProjectID,
			includeManifest: false,
			includeConfig:   true,
		},
		{
			name:            "empty project id",
			projectID:       "",
			configID:        "",
			includeConfig:   true,
			includeManifest: true,
			mutate: func(m *Manifest) {
				m.Project.ID = ""
			},
		},
		{
			name:            "invalid project id",
			projectID:       "not-a-uuid",
			configID:        "not-a-uuid",
			includeConfig:   true,
			includeManifest: true,
			mutate: func(m *Manifest) {
				m.Project.ID = "not-a-uuid"
			},
		},
		{
			name:            "project id disagrees with config",
			projectID:       testProjectID,
			configID:        "22222222-2222-2222-2222-222222222222",
			includeConfig:   true,
			includeManifest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildTestArchive(t, tt.projectID, "Test Project", tt.configID, tt.includeConfig, tt.includeManifest, tt.mutate)

			_, err := newTestImporter().Parse(data)
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !errors.Is(err, ErrInvalidArchive) {
				t.Errorf("errors.Is(err, ErrInvalidArchive) = false, err = %v", err)
			}
		})
	}
}

func TestParseNotZip(t *testing.T) {
	_, err := newTestImporter().Parse([]byte("this is not a zip archive"))
	if err == nil {
		t.Fatal("expected Parse to fail on non-ZIP input")
	}
	if !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("errors.Is(err, ErrInvalidArchive) = false, err = %v", err)
	}
}
