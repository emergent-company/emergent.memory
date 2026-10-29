package sources

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles database operations for data sources.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new sources repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("sources.repo")),
	}
}

// Create inserts a new source and returns it. The encrypted_credentials column
// is written but never selected back into the returned row.
func (r *Repository) Create(ctx context.Context, src *Source) (*Source, error) {
	_, err := r.db.NewInsert().
		Model(src).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to create source", logger.Error(err))
		return nil, apperror.NewDatabase("failed to create source", err)
	}
	return src, nil
}

// GetByID retrieves a source by id, scoped to projectID. encrypted_credentials
// is never selected. A missing row (or a row belonging to another project)
// resolves to not-found.
func (r *Repository) GetByID(ctx context.Context, id, projectID uuid.UUID) (*Source, error) {
	src := &Source{}
	err := r.db.NewSelect().
		Model(src).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		ExcludeColumn("encrypted_credentials").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get source", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get source", err)
	}
	return src, nil
}

// GetByIDWithCredentials retrieves the full source row (including
// encrypted_credentials) for internal use by update and connection testing.
func (r *Repository) GetByIDWithCredentials(ctx context.Context, id, projectID uuid.UUID) (*Source, error) {
	src := &Source{}
	err := r.db.NewSelect().
		Model(src).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get source with credentials", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get source", err)
	}
	return src, nil
}

// List retrieves all sources for a project, excluding encrypted_credentials.
func (r *Repository) List(ctx context.Context, projectID uuid.UUID) ([]*Source, error) {
	var srcs []*Source
	err := r.db.NewSelect().
		Model(&srcs).
		Where("project_id = ?", projectID).
		ExcludeColumn("encrypted_credentials").
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list sources", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list sources", err)
	}
	if srcs == nil {
		srcs = []*Source{}
	}
	return srcs, nil
}

// Update updates the mutable fields of an existing source scoped by project.
// encrypted_credentials is only updated when a non-empty value is supplied.
func (r *Repository) Update(ctx context.Context, src *Source) (*Source, error) {
	q := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("display_name = ?", src.DisplayName).
		Set("config = ?", src.Config).
		Set("sync_policy = ?", src.SyncPolicy).
		Set("sync_interval_minutes = ?", src.SyncIntervalMinutes).
		Set("status = ?", src.Status).
		Set("credential_id = ?", src.CredentialID).
		Set("updated_at = now()").
		Where("id = ?", src.ID).
		Where("project_id = ?", src.ProjectID)

	if len(src.EncryptedCredentials) > 0 {
		q = q.Set("encrypted_credentials = ?", src.EncryptedCredentials)
	} else if src.CredentialID != nil {
		// A referenced source carries no inline credential copy.
		q = q.Set("encrypted_credentials = NULL")
	}

	res, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to update source", logger.Error(err))
		return nil, apperror.NewDatabase("failed to update source", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperror.ErrNotFound
	}
	return src, nil
}

// UpdateCredentials replaces one source's encrypted credential blob. It sets a
// single column so a write-back that happens mid-sync cannot clobber a
// concurrent edit to the source's config or status — Update would rewrite the
// whole row from a snapshot taken before the sync started.
func (r *Repository) UpdateCredentials(ctx context.Context, id, projectID uuid.UUID, blob []byte) error {
	if len(blob) == 0 {
		return apperror.NewValidation("credentials blob is empty")
	}
	res, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("encrypted_credentials = ?", blob).
		Set("updated_at = now()").
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to persist source credentials", logger.Error(err))
		return apperror.NewDatabase("failed to persist source credentials", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// ReplaceOAuthGrant installs a freshly granted OAuth credential on an existing
// source: the credential blob and the connect-time config metadata are replaced
// and the recorded error is cleared, because a dead grant is the failure a
// reconnect resolves. Every other column — name, sync policy, status, cursor,
// the source's own config keys — is left alone, so a reconnect cannot reset the
// sync scope or undo a pause (#2107).
func (r *Repository) ReplaceOAuthGrant(ctx context.Context, id, projectID uuid.UUID, blob []byte, config map[string]any) error {
	if len(blob) == 0 {
		return apperror.NewValidation("credentials blob is empty")
	}
	res, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("encrypted_credentials = ?", blob).
		Set("config = ?", config).
		Set("last_error = NULL").
		Set("last_error_at = NULL").
		Set("error_count = 0").
		Set("updated_at = now()").
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to replace source oauth grant", logger.Error(err))
		return apperror.NewDatabase("failed to replace source oauth grant", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// Delete deletes a source scoped by project. A missing row (or a cross-project
// id) resolves to not-found.
func (r *Repository) Delete(ctx context.Context, id, projectID uuid.UUID) error {
	res, err := r.db.NewDelete().
		Model((*Source)(nil)).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete source", logger.Error(err))
		return apperror.NewDatabase("failed to delete source", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// ListSourceTypes returns document counts grouped by source type for a project.
// Only HEAD rows are counted so versioned documents are not double-counted.
func (r *Repository) ListSourceTypes(ctx context.Context, projectID uuid.UUID) ([]SourceType, error) {
	type row struct {
		SourceType    string `bun:"source_type"`
		DocumentCount int    `bun:"document_count"`
	}
	var rows []row
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		ColumnExpr("source_type, COUNT(*)::int AS document_count").
		Where("project_id = ?", projectID).
		Where("source_type IS NOT NULL").
		Where("supersedes_id IS NULL").
		GroupExpr("source_type").
		OrderExpr("document_count DESC").
		Scan(ctx, &rows)
	if err != nil {
		r.log.Error("failed to list source types", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list source types", err)
	}

	out := make([]SourceType, len(rows))
	for i, r := range rows {
		out[i] = SourceType(r)
	}
	return out, nil
}
