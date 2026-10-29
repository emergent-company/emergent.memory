package sources

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// ListCredentials returns a project's saved credentials, optionally filtered to
// one provider, plus the number of sources referencing each credential.
func (r *Repository) ListCredentials(ctx context.Context, projectID uuid.UUID, provider string) ([]*SourceCredential, map[uuid.UUID]int, error) {
	var creds []*SourceCredential
	q := r.db.NewSelect().
		Model(&creds).
		Where("project_id = ?", projectID).
		Order("created_at DESC")
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list source credentials", logger.Error(err))
		return nil, nil, apperror.NewDatabase("failed to list source credentials", err)
	}
	if creds == nil {
		creds = []*SourceCredential{}
	}

	type countRow struct {
		CredentialID uuid.UUID `bun:"credential_id"`
		UsedCount    int       `bun:"used_count"`
	}
	var rows []countRow
	if err := r.db.NewSelect().
		TableExpr("kb.sources").
		ColumnExpr("credential_id, COUNT(*)::int AS used_count").
		Where("project_id = ?", projectID).
		Where("credential_id IS NOT NULL").
		GroupExpr("credential_id").
		Scan(ctx, &rows); err != nil {
		r.log.Error("failed to count source credential usage", logger.Error(err))
		return nil, nil, apperror.NewDatabase("failed to count source credential usage", err)
	}
	counts := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		counts[row.CredentialID] = row.UsedCount
	}
	return creds, counts, nil
}

// GetCredentialByID retrieves one saved credential scoped to projectID. A
// missing row, including a row belonging to another project, is not-found.
func (r *Repository) GetCredentialByID(ctx context.Context, id, projectID uuid.UUID) (*SourceCredential, error) {
	cred := &SourceCredential{}
	err := r.db.NewSelect().
		Model(cred).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get source credential", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get source credential", err)
	}
	return cred, nil
}

// CreateCredential inserts a saved credential and returns it.
func (r *Repository) CreateCredential(ctx context.Context, cred *SourceCredential) (*SourceCredential, error) {
	if _, err := r.db.NewInsert().Model(cred).Exec(ctx); err != nil {
		r.log.Error("failed to create source credential", logger.Error(err))
		return nil, apperror.NewDatabase("failed to create source credential", err)
	}
	return cred, nil
}

// UpdateCredential updates a saved credential's name and encrypted payload,
// scoped to its project.
func (r *Repository) UpdateCredential(ctx context.Context, cred *SourceCredential) (*SourceCredential, error) {
	res, err := r.db.NewUpdate().
		Model((*SourceCredential)(nil)).
		Set("name = ?", cred.Name).
		Set("encrypted_credentials = ?", cred.EncryptedCredentials).
		Set("updated_at = now()").
		Where("id = ?", cred.ID).
		Where("project_id = ?", cred.ProjectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update source credential", logger.Error(err))
		return nil, apperror.NewDatabase("failed to update source credential", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperror.ErrNotFound
	}
	return cred, nil
}

// DeleteCredential deletes one saved credential scoped to its project.
func (r *Repository) DeleteCredential(ctx context.Context, id, projectID uuid.UUID) error {
	res, err := r.db.NewDelete().
		Model((*SourceCredential)(nil)).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		if pgutils.IsForeignKeyViolation(err) {
			// Lost the race against a concurrent source-create/update that
			// referenced this credential: same conflict as the service precheck.
			return apperror.NewConflict("credential is in use by a data source")
		}
		r.log.Error("failed to delete source credential", logger.Error(err))
		return apperror.NewDatabase("failed to delete source credential", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// CountSourcesUsingCredential reports how many sources in a project reference a
// credential.
func (r *Repository) CountSourcesUsingCredential(ctx context.Context, projectID, credentialID uuid.UUID) (int, error) {
	count, err := r.db.NewSelect().
		Model((*Source)(nil)).
		Where("project_id = ?", projectID).
		Where("credential_id = ?", credentialID).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count sources using credential", logger.Error(err))
		return 0, apperror.NewDatabase("failed to count sources using credential", err)
	}
	return count, nil
}
