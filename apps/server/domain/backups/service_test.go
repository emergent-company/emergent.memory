package backups

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// disabledStorageService returns a storage.Service with no backing client
// (as produced when storage is not configured).
func disabledStorageService() *storage.Service {
	svc, err := storage.NewService(&storage.Config{}, testLogger())
	if err != nil {
		panic(err)
	}
	return svc
}

func TestImportBackupInvalidArchive(t *testing.T) {
	svc := &Service{
		importer: newTestImporter(),
		storage:  disabledStorageService(),
	}

	_, err := svc.ImportBackup(context.Background(), "org-1", "user-1", []byte("not a zip"), 30)
	if err == nil {
		t.Fatal("expected error for invalid archive")
	}
	if !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("errors.Is(err, ErrInvalidArchive) = false, err = %v", err)
	}
}

func TestImportBackupStorageDisabled(t *testing.T) {
	svc := &Service{
		importer: newTestImporter(),
		storage:  disabledStorageService(),
		repo:     nil, // must not be reached
	}

	data := buildTestArchive(t, testProjectID, "Test Project", testProjectID, true, true, nil)
	_, err := svc.ImportBackup(context.Background(), "org-1", "user-1", data, 30)
	if err == nil {
		t.Fatal("expected error when storage is disabled")
	}
}

func TestNewImportedBackupRecord(t *testing.T) {
	data := buildTestArchive(t, testProjectID, "Test Project", testProjectID, true, true, nil)
	archive, err := newTestImporter().Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	manifest := archive.Manifest()

	rec := newImportedBackupRecord("org-1", "user-1", "backup-1", "backups/org-1/backup-1/backup.zip", int64(len(data)), archive, 0)

	if rec.ProjectID != testProjectID {
		t.Errorf("ProjectID = %q, want %q", rec.ProjectID, testProjectID)
	}
	if rec.ProjectName != "Test Project" {
		t.Errorf("ProjectName = %q, want %q", rec.ProjectName, "Test Project")
	}
	if rec.Status != BackupStatusReady {
		t.Errorf("Status = %q, want %q", rec.Status, BackupStatusReady)
	}
	if !rec.Imported {
		t.Error("Imported = false, want true")
	}
	if rec.Progress != 100 {
		t.Errorf("Progress = %d, want 100", rec.Progress)
	}
	if rec.BackupType != BackupTypeFull {
		t.Errorf("BackupType = %q, want %q", rec.BackupType, BackupTypeFull)
	}
	if rec.ManifestChecksum == nil || *rec.ManifestChecksum != manifest.Checksums.Manifest {
		t.Error("ManifestChecksum not copied from manifest")
	}
	if rec.ContentChecksum == nil || *rec.ContentChecksum != manifest.Checksums.Database {
		t.Error("ContentChecksum not copied from manifest")
	}
	if got, _ := rec.Includes["imported"].(bool); !got {
		t.Errorf("Includes[imported] = %v, want true", rec.Includes["imported"])
	}
	if rec.SizeBytes != int64(len(data)) {
		t.Errorf("SizeBytes = %d, want %d", rec.SizeBytes, len(data))
	}
	if rec.CreatedBy == nil || *rec.CreatedBy != "user-1" {
		t.Errorf("CreatedBy = %v, want user-1", rec.CreatedBy)
	}

	days := rec.ExpiresAt.Sub(rec.CreatedAt).Hours() / 24
	if days < 29 || days > 31 {
		t.Errorf("expires in %.1f days, want ~30", days)
	}
}

func TestNewImportedBackupRecordRetentionDays(t *testing.T) {
	data := buildTestArchive(t, testProjectID, "Test Project", testProjectID, true, true, nil)
	archive, err := newTestImporter().Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rec := newImportedBackupRecord("org-1", "user-1", "backup-1", "key", int64(len(data)), archive, 7)
	days := rec.ExpiresAt.Sub(rec.CreatedAt).Hours() / 24
	if days < 6 || days > 8 {
		t.Errorf("expires in %.1f days, want ~7", days)
	}
}
