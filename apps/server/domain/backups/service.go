package backups

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Service handles backup business logic
type Service struct {
	repo    *Repository
	creator *Creator
	log     *slog.Logger
}

// NewService creates a new backup service
func NewService(repo *Repository, creator *Creator, log *slog.Logger) *Service {
	return &Service{
		repo:    repo,
		creator: creator,
		log:     log.With(slog.String("component", "backups.service")),
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
		s.markBackupFailed(ctx, backupID, err)
		return
	}
	opts.ProjectName = projectName

	// Execute backup
	if err := s.creator.CreateBackup(ctx, opts); err != nil {
		s.log.Error("backup creation failed",
			slog.String("backup_id", backupID),
			slog.Any("error", err),
		)
		s.markBackupFailed(ctx, backupID, err)
		return
	}

	s.log.Info("backup completed successfully",
		slog.String("backup_id", backupID),
	)
}

// markBackupFailed marks a backup as failed
func (s *Service) markBackupFailed(ctx context.Context, backupID string, err error) {
	backup, getErr := s.repo.GetByID(ctx, "", backupID)
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

// GenerateStorageKey generates a MinIO storage key for a backup
func GenerateStorageKey(orgID, backupID string) string {
	return fmt.Sprintf("backups/%s/%s/backup.zip", orgID, backupID)
}

// GenerateMetadataKey generates a MinIO storage key for backup metadata
func GenerateMetadataKey(orgID, backupID string) string {
	return fmt.Sprintf("backups/%s/%s/metadata.json", orgID, backupID)
}
