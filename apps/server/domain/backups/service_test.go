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
