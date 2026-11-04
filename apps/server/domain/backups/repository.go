package backups

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
)

// Repository handles database operations for backups
type Repository struct {
	db  *bun.DB
	log *slog.Logger
}

// NewRepository creates a new backups repository
func NewRepository(db *bun.DB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(slog.String("component", "backups.repository")),
	}
}

// Create creates a new backup record
func (r *Repository) Create(ctx context.Context, backup *Backup) error {
	_, err := r.db.NewInsert().
		Model(backup).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create backup",
			slog.String("project_id", backup.ProjectID),
			slog.Any("error", err),
		)
		return fmt.Errorf("create backup: %w", err)
	}

	r.log.Info("backup created",
		slog.String("id", backup.ID),
		slog.String("project_id", backup.ProjectID),
	)

	return nil
}

// GetByID retrieves a backup by ID
func (r *Repository) GetByID(ctx context.Context, orgID, backupID string) (*Backup, error) {
	var backup Backup
	q := r.db.NewSelect().
		Model(&backup).
		Where("id = ?", backupID)
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	err := q.Scan(ctx)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		r.log.Error("failed to get backup",
			slog.String("id", backupID),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("get backup: %w", err)
	}

	return &backup, nil
}

// List retrieves backups with pagination
func (r *Repository) List(ctx context.Context, params ListParams) (*ListResult, error) {
	query := r.db.NewSelect().
		Model((*Backup)(nil)).
		Where("organization_id = ?", params.OrganizationID).
		Where("deleted_at IS NULL")

	// Optional filters
	if params.ProjectID != nil {
		query = query.Where("project_id = ?", *params.ProjectID)
	}
	if params.Status != nil {
		query = query.Where("status = ?", *params.Status)
	}

	// Cursor pagination
	if params.Cursor != nil {
		query = query.Where("(created_at, id) < (?, ?)", params.Cursor.CreatedAt, params.Cursor.ID)
	}

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count backups: %w", err)
	}

	// Get paginated results
	var backups []Backup
	err = query.
		Order("created_at DESC", "id DESC").
		Limit(params.Limit).
		Scan(ctx, &backups)

	if err != nil {
		r.log.Error("failed to list backups",
			slog.String("org_id", params.OrganizationID),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("list backups: %w", err)
	}

	// Generate next cursor
	var nextCursor *Cursor
	if len(backups) == params.Limit {
		lastBackup := backups[len(backups)-1]
		nextCursor = &Cursor{
			CreatedAt: lastBackup.CreatedAt,
			ID:        lastBackup.ID,
		}
	}

	return &ListResult{
		Backups:    backups,
		Total:      total,
		NextCursor: nextCursor,
	}, nil
}

// Update updates a backup record
func (r *Repository) Update(ctx context.Context, backup *Backup) error {
	_, err := r.db.NewUpdate().
		Model(backup).
		WherePK().
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to update backup",
			slog.String("id", backup.ID),
			slog.Any("error", err),
		)
		return fmt.Errorf("update backup: %w", err)
	}

	return nil
}

// SoftDelete marks a backup as deleted
func (r *Repository) SoftDelete(ctx context.Context, orgID, backupID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*Backup)(nil)).
		Set("deleted_at = ?", now).
		Set("status = ?", BackupStatusDeleted).
		Where("id = ?", backupID).
		Where("organization_id = ?", orgID).
		Where("deleted_at IS NULL").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to soft delete backup",
			slog.String("id", backupID),
			slog.Any("error", err),
		)
		return fmt.Errorf("soft delete backup: %w", err)
	}

	r.log.Info("backup soft deleted",
		slog.String("id", backupID),
	)

	return nil
}

// HardDelete permanently deletes a backup record
func (r *Repository) HardDelete(ctx context.Context, backupID string) error {
	_, err := r.db.NewDelete().
		Model((*Backup)(nil)).
		Where("id = ?", backupID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to hard delete backup",
			slog.String("id", backupID),
			slog.Any("error", err),
		)
		return fmt.Errorf("hard delete backup: %w", err)
	}

	r.log.Info("backup hard deleted",
		slog.String("id", backupID),
	)

	return nil
}

// GetExpiredBackups retrieves backups that have expired
func (r *Repository) GetExpiredBackups(ctx context.Context) ([]Backup, error) {
	var backups []Backup
	err := r.db.NewSelect().
		Model(&backups).
		Where("expires_at < ?", time.Now()).
		Where("deleted_at IS NULL").
		Where("status = ?", BackupStatusReady).
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get expired backups", slog.Any("error", err))
		return nil, fmt.Errorf("get expired backups: %w", err)
	}

	return backups, nil
}

// GetSoftDeletedBackups retrieves backups that were soft deleted before the given date
func (r *Repository) GetSoftDeletedBackups(ctx context.Context, before time.Time) ([]Backup, error) {
	var backups []Backup
	err := r.db.NewSelect().
		Model(&backups).
		Where("deleted_at IS NOT NULL").
		Where("deleted_at < ?", before).
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get soft deleted backups", slog.Any("error", err))
		return nil, fmt.Errorf("get soft deleted backups: %w", err)
	}

	return backups, nil
}

// CreateRestore inserts a new restore job record.
func (r *Repository) CreateRestore(ctx context.Context, restore *Restore) error {
	_, err := r.db.NewInsert().
		Model(restore).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create restore",
			slog.String("backup_id", restore.BackupID),
			slog.Any("error", err),
		)
		return fmt.Errorf("create restore: %w", err)
	}

	r.log.Info("restore created",
		slog.String("id", restore.ID),
		slog.String("backup_id", restore.BackupID),
		slog.String("mode", restore.Mode),
	)

	return nil
}

// GetRestore retrieves a restore job by ID.
func (r *Repository) GetRestore(ctx context.Context, id string) (*Restore, error) {
	var restore Restore
	err := r.db.NewSelect().
		Model(&restore).
		Where("id = ?", id).
		Scan(ctx)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		r.log.Error("failed to get restore",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("get restore: %w", err)
	}

	return &restore, nil
}

// UpdateRestore persists changes to a restore job record.
func (r *Repository) UpdateRestore(ctx context.Context, restore *Restore) error {
	_, err := r.db.NewUpdate().
		Model(restore).
		WherePK().
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to update restore",
			slog.String("id", restore.ID),
			slog.Any("error", err),
		)
		return fmt.Errorf("update restore: %w", err)
	}

	return nil
}

// --- Server-side authorization predicates ---
//
// Every predicate below resolves authority from database membership/ownership,
// never from a caller-supplied value (the :orgId/:projectId path params are the
// addressed resource, not the authorization source).

// OrgMembershipRole returns the caller's role in an organization ("" when not a
// member). It is the org-tier authority source for backup management.
func (r *Repository) OrgMembershipRole(ctx context.Context, orgID, userID string) (string, error) {
	var role string
	err := r.db.NewSelect().
		TableExpr("kb.organization_memberships").
		Column("role").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Limit(1).
		Scan(ctx, &role)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		r.log.Error("failed to get org membership role",
			slog.String("org_id", orgID),
			slog.Any("error", err),
		)
		return "", fmt.Errorf("get org membership role: %w", err)
	}
	return role, nil
}

// ProjectMembershipRole returns the caller's role in a project ("" when not a
// member). It is the project-tier authority source for project backup create and
// overwrite-restore.
func (r *Repository) ProjectMembershipRole(ctx context.Context, projectID, userID string) (string, error) {
	var role string
	err := r.db.NewSelect().
		TableExpr("kb.project_memberships").
		Column("role").
		Where("project_id = ?", projectID).
		Where("user_id = ?", userID).
		Limit(1).
		Scan(ctx, &role)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		r.log.Error("failed to get project membership role",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return "", fmt.Errorf("get project membership role: %w", err)
	}
	return role, nil
}

// ProjectOrgID resolves the owning organization of a project server-side ("" when
// the project does not exist). It is used to derive the org tier from a
// project-scoped address.
func (r *Repository) ProjectOrgID(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Limit(1).
		Scan(ctx, &orgID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		r.log.Error("failed to get project org",
			slog.String("project_id", projectID),
			slog.Any("error", err),
		)
		return "", fmt.Errorf("get project org: %w", err)
	}
	return orgID, nil
}
