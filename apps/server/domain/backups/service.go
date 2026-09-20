package backups

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/google/uuid"
)

// Service handles backup business logic.
type Service struct {
	repo     *Repository
	creator  *Creator
	restorer *Restorer
	importer *Importer
	storage  *storage.Service
	log      *slog.Logger
}

// NewService creates a new backup service
func NewService(repo *Repository, creator *Creator, restorer *Restorer, importer *Importer, storageSvc *storage.Service, log *slog.Logger) *Service {
	return &Service{
		repo:     repo,
		creator:  creator,
		restorer: restorer,
		importer: importer,
		storage:  storageSvc,
		log:      log.With(slog.String("component", "backups.service")),
	}
}

// CreateBackup creates a new backup and starts the backup process asynchronously
func (s *Service) CreateBackup(ctx context.Context, req CreateBackupRequest) (*Backup, error) {
	backupID := uuid.New().String()

	// Generate storage key: backups/{orgId}/{backupId}/backup.zip
	storageKey := GenerateStorageKey(req.OrganizationID, backupID)

	// Calculate expiration date
	expiresAt := time.Now().AddDate(0, 0, req.RetentionDays)
	if req.RetentionDays == 0 {
		expiresAt = time.Now().AddDate(0, 0, 30) // Default: 30 days
	}

	// Get project name for the backup
	var projectName string
	err := s.repo.db.NewSelect().
		Table("kb.projects").
		Column("name").
		Where("id = ?", req.ProjectID).
		Scan(ctx, &projectName)
	if err != nil {
		return nil, fmt.Errorf("get project name: %w", err)
	}

	backup := &Backup{
		ID:             backupID,
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
		ProjectName:    projectName,
		StorageKey:     storageKey,
		Status:         BackupStatusCreating,
		Progress:       0,
		BackupType:     BackupTypeFull,
		Includes: map[string]any{
			"documents": true,
			"chunks":    true,
			"graph":     true,
			"chat":      req.IncludeChat,
			"journal":   req.IncludeJournal,
			"deleted":   req.IncludeDeleted,
		},
		CreatedAt: time.Now(),
		CreatedBy: &req.CreatedBy,
		ExpiresAt: &expiresAt,
	}

	if err := s.repo.Create(ctx, backup); err != nil {
		return nil, fmt.Errorf("create backup: %w", err)
	}

	s.log.Info("backup creation initiated",
		slog.String("backup_id", backupID),
		slog.String("project_id", req.ProjectID),
	)

	// Start backup creation asynchronously
	go s.executeBackup(context.Background(), backupID, req)

	return backup, nil
}

// executeBackup runs the actual backup creation process
func (s *Service) executeBackup(ctx context.Context, backupID string, req CreateBackupRequest) {
	opts := CreateBackupOptions{
		BackupID:       backupID,
		ProjectID:      req.ProjectID,
		ProjectName:    "", // Will be fetched from database
		OrganizationID: req.OrganizationID,
		IncludeChat:    req.IncludeChat,
		IncludeJournal: req.IncludeJournal,
		IncludeDeleted: req.IncludeDeleted,
	}

	// Get project name
	var projectName string
	err := s.repo.db.NewSelect().
		Table("kb.projects").
		Column("name").
		Where("id = ?", req.ProjectID).
		Scan(ctx, &projectName)
	if err != nil {
		s.log.Error("failed to get project name",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		s.markBackupFailed(ctx, req.OrganizationID, backupID, err)
		return
	}
	opts.ProjectName = projectName

	// Execute backup
	if err := s.creator.CreateBackup(ctx, opts); err != nil {
		s.log.Error("backup creation failed",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		s.markBackupFailed(ctx, req.OrganizationID, backupID, err)
		return
	}

	s.log.Info("backup completed successfully",
		slog.String("backup_id", backupID),
	)
}

// markBackupFailed marks a backup as failed
func (s *Service) markBackupFailed(ctx context.Context, orgID, backupID string, err error) {
	backup, getErr := s.repo.GetByID(ctx, orgID, backupID)
	if getErr != nil {
		s.log.Error("failed to get backup for failure update",
			slog.String("backup_id", backupID),
			slog.Any("error", getErr),
		)
		return
	}
	if backup == nil {
		return
	}

	backup.Status = BackupStatusFailed
	errMsg := err.Error()
	backup.ErrorMessage = &errMsg

	if updateErr := s.repo.Update(ctx, backup); updateErr != nil {
		s.log.Error("failed to update backup status",
			slog.String("backup_id", backupID),
			slog.Any("error", updateErr),
		)
	}
}

// GetBackup retrieves a backup by ID
func (s *Service) GetBackup(ctx context.Context, orgID, backupID string) (*Backup, error) {
	backup, err := s.repo.GetByID(ctx, orgID, backupID)
	if err != nil {
		return nil, fmt.Errorf("get backup: %w", err)
	}
	if backup == nil {
		return nil, fmt.Errorf("backup not found")
	}
	return backup, nil
}

// ListBackups retrieves a list of backups
func (s *Service) ListBackups(ctx context.Context, params ListParams) (*ListResult, error) {
	return s.repo.List(ctx, params)
}

// UpdateBackupStatus updates the status of a backup
func (s *Service) UpdateBackupStatus(ctx context.Context, backupID, status string, progress int) error {
	backup, err := s.repo.GetByID(ctx, "", backupID)
	if err != nil {
		return fmt.Errorf("get backup: %w", err)
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}

	backup.Status = status
	backup.Progress = progress

	if status == BackupStatusReady {
		now := time.Now()
		backup.CompletedAt = &now
	}

	return s.repo.Update(ctx, backup)
}

// DeleteBackup soft deletes a backup
func (s *Service) DeleteBackup(ctx context.Context, orgID, backupID string) error {
	return s.repo.SoftDelete(ctx, orgID, backupID)
}

// ImportBackup validates an uploaded archive, stores it under a fresh backup
// key, and registers a `ready` backup so the clone-restore path can consume it.
// The archive's source project is foreign (see design D2), so the backup is
// marked imported and its project_id records the manifest's source project id.
func (s *Service) ImportBackup(ctx context.Context, orgID, userID string, data []byte, retentionDays int) (*Backup, error) {
	archive, err := s.importer.Parse(data)
	if err != nil {
		return nil, err
	}

	if !s.storage.Enabled() {
		return nil, fmt.Errorf("storage service not enabled")
	}

	backupID := uuid.New().String()
	storageKey := GenerateStorageKey(orgID, backupID)

	if _, err := s.storage.Upload(ctx, storageKey, bytes.NewReader(data), int64(len(data)), storage.UploadOptions{
		ContentType: "application/zip",
	}); err != nil {
		return nil, fmt.Errorf("upload imported backup: %w", err)
	}

	backup := newImportedBackupRecord(orgID, userID, backupID, storageKey, int64(len(data)), archive, retentionDays)

	if err := s.repo.Create(ctx, backup); err != nil {
		// Best-effort cleanup of the just-uploaded object so a failed insert
		// does not leave an orphaned archive behind.
		_ = s.storage.Delete(ctx, storageKey)
		return nil, fmt.Errorf("create imported backup: %w", err)
	}

	s.log.Info("imported backup registered",
		slog.String("backup_id", backupID),
		slog.String("org_id", orgID),
		slog.String("source_project_id", backup.ProjectID),
	)

	return backup, nil
}

// newImportedBackupRecord builds the `ready` backup record for an imported
// archive. retentionDays <= 0 defaults to 30. The record's project_id /
// project_name record the manifest's foreign source project; it is flagged
// imported and supports clone restore only.
func newImportedBackupRecord(orgID, userID, backupID, storageKey string, dataLen int64, archive *Archive, retentionDays int) *Backup {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	manifest := archive.Manifest()
	backupType := manifest.BackupType
	if backupType == "" {
		backupType = BackupTypeFull
	}

	now := time.Now()
	expiresAt := now.AddDate(0, 0, retentionDays)

	return &Backup{
		ID:             backupID,
		OrganizationID: orgID,
		ProjectID:      manifest.Project.ID,
		ProjectName:    manifest.Project.Name,
		StorageKey:     storageKey,
		SizeBytes:      dataLen,
		Status:         BackupStatusReady,
		Progress:       100,
		BackupType:     backupType,
		Includes: map[string]any{
			"documents": true,
			"chunks":    true,
			"graph":     true,
			"chat":      manifest.Metadata["include_chat"],
			"journal":   manifest.Metadata["include_journal"],
			"imported":  true,
		},
		Stats:            backupStatsToMap(&manifest.Contents),
		CreatedAt:        now,
		CreatedBy:        &userID,
		CompletedAt:      &now,
		ExpiresAt:        &expiresAt,
		Imported:         true,
		ManifestChecksum: &manifest.Checksums.Manifest,
		ContentChecksum:  &manifest.Checksums.Database,
	}
}

// CreateRestore creates a restore job and dispatches it asynchronously.
func (s *Service) CreateRestore(ctx context.Context, req RestoreRequest) (*Restore, error) {
	if req.BackupID == "" {
		return nil, fmt.Errorf("backup_id is required")
	}
	if req.Mode != RestoreModeOverwrite && req.Mode != RestoreModeClone {
		return nil, fmt.Errorf("invalid restore mode %q", req.Mode)
	}

	restoreID := uuid.New().String()
	job := &Restore{
		ID:             restoreID,
		OrganizationID: req.OrganizationID,
		BackupID:       req.BackupID,
		Mode:           req.Mode,
		Status:         RestoreStatusPending,
		Progress:       0,
		CreatedAt:      time.Now(),
		Stats:          map[string]any{},
	}
	if req.SourceProjectID != "" {
		job.SourceProjectID = &req.SourceProjectID
	}
	if req.TargetProjectID != "" {
		job.TargetProjectID = &req.TargetProjectID
	}
	if req.CreatedBy != "" {
		job.CreatedBy = &req.CreatedBy
	}

	if err := s.repo.CreateRestore(ctx, job); err != nil {
		return nil, err
	}

	s.log.Info("restore job created",
		slog.String("restore_id", restoreID),
		slog.String("backup_id", req.BackupID),
		slog.String("mode", req.Mode),
	)

	// Run on its own copy so the caller's snapshot is not mutated concurrently.
	dispatch := *job
	go s.restorer.Restore(context.Background(), &dispatch, req)

	return job, nil
}

// GetRestore retrieves a restore job by ID.
func (s *Service) GetRestore(ctx context.Context, id string) (*Restore, error) {
	job, err := s.repo.GetRestore(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get restore: %w", err)
	}
	if job == nil {
		return nil, nil
	}
	return job, nil
}

// GenerateStorageKey generates a MinIO storage key for a backup
func GenerateStorageKey(orgID, backupID string) string {
	return fmt.Sprintf("backups/%s/%s/backup.zip", orgID, backupID)
}

// GenerateMetadataKey generates a MinIO storage key for backup metadata
func GenerateMetadataKey(orgID, backupID string) string {
	return fmt.Sprintf("backups/%s/%s/metadata.json", orgID, backupID)
}

// ListDatabaseBackups returns all database backup records ordered by created_at DESC
func (s *Service) ListDatabaseBackups(ctx context.Context) ([]*DatabaseBackup, error) {
	var backups []*DatabaseBackup
	err := s.repo.db.NewSelect().
		Model(&backups).
		OrderExpr("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list database backups: %w", err)
	}
	return backups, nil
}

// GetDatabaseBackupDownloadURL generates a presigned URL for downloading a database backup
func (s *Service) GetDatabaseBackupDownloadURL(ctx context.Context, id string) (string, error) {
	var backup DatabaseBackup
	err := s.repo.db.NewSelect().
		Model(&backup).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return "", fmt.Errorf("get database backup: %w", err)
	}
	if backup.StorageKey == nil {
		return "", fmt.Errorf("backup has no storage key")
	}

	url, err := s.storage.GetSignedDownloadURLFromBucket(
		ctx,
		"database-backups",
		*backup.StorageKey,
		storage.GetSignedDownloadURLOptions{
			ExpiresIn: 15 * time.Minute,
			ResponseContentDisposition: fmt.Sprintf(
				`attachment; filename="db-backup-%s.dump"`,
				backup.CreatedAt.UTC().Format("2006-01-02_15-04-05"),
			),
		},
	)
	if err != nil {
		return "", fmt.Errorf("generate presigned URL: %w", err)
	}
	return url, nil
}
