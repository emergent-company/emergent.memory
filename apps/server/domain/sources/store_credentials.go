package sources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

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

// ReplaceSavedCredentialGrant installs a freshly granted credential on a saved
// credential and returns every source that reads it to active.
//
// A source created from a saved credential reads that credential's blob at
// runtime (Service.credentialsForSource) and never its own row, so replacing only
// the source's inline blob — which is what a reconnect used to do — left the sync
// failing on the old grant and re-parked the source on its next run, making the
// user's one repair action a no-op loop (#2418). A saved credential is one
// account's grant, so one consent repairs every source on it.
//
// Those sources have to be un-parked here as well: a parked source is never
// admitted by the scheduler sweep (ListDueSources), so a sibling left parked could
// not notice the repaired credential on its own. The recorded failure is cleared
// only on the rows that were parked — an active source keeps the error its last
// sync recorded, because that one is live, which is the same rule
// Repository.Update follows when an explicit enable reactivates a source (#2421).
//
// grantMeta carries the grant's display metadata (strategy, scopes, account email)
// and replaces it on every referencing source: after a reconnect the card must
// describe the account that consented now, not the one the dead grant belonged to
// (#2186). Keys absent from grantMeta are removed, not left behind.
//
// It returns the sources it returned to active, so the caller can resolve their
// notifications; a disabled sibling is left alone and is not returned.
//
// The credential write and the source updates run in one transaction (#2457): a
// half-applied reconnect would leave a fresh credential with every source on it
// parked and an in-flight job still able to park it again.
func (r *Repository) ReplaceSavedCredentialGrant(ctx context.Context, credentialID, projectID uuid.UUID, blob []byte, grantMeta map[string]any) ([]uuid.UUID, error) {
	if len(blob) == 0 {
		return nil, apperror.NewValidation("credentials blob is empty")
	}
	// A nil map marshals to `null`, and `config || 'null'::jsonb` turns the object
	// into an array ([{…}, null]) — every read of that source then fails to
	// unmarshal. The column is NOT NULL, so it would need SQL to repair. Callers
	// pass the grant metadata they could resolve, which is legitimately empty when
	// the exchange supplies none.
	if grantMeta == nil {
		grantMeta = map[string]any{}
	}
	// Marshal before the first write: a value the metadata cannot serialise is a
	// caller bug, and failing after the blob had been rotated would leave the
	// credential replaced with the sources still parked.
	metaJSON, err := json.Marshal(grantMeta)
	if err != nil {
		return nil, apperror.NewValidation("grant metadata is not serialisable")
	}

	// The grant write and the source un-park are one unit (#2457). A death
	// between them would leave the credential fresh and every source on it parked
	// with the old epoch — a state nothing retries, because the scheduler never
	// admits a parked source, so the reconnect would appear to have failed and
	// cost the user a second one. The repository owns the boundary here rather
	// than the caller: the metadata merge below is not expressible in the same
	// statement as the credential write.
	var reactivated []uuid.UUID
	if err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Model((*SourceCredential)(nil)).
			Set("encrypted_credentials = ?", blob).
			Set("updated_at = now()").
			Where("id = ?", credentialID).
			Where("project_id = ?", projectID).
			Exec(ctx)
		if err != nil {
			r.log.Error("failed to replace saved credential grant", logger.Error(err))
			return apperror.NewDatabase("failed to replace saved credential grant", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return apperror.ErrNotFound
		}

		// Every source on the credential learns the grant's display metadata, and
		// the grant it reads has just been replaced, so each of their epochs moves:
		// a sync job that started before this write and fails afterwards must not
		// park the source the user has just repaired (#2425). This is the
		// saved-credential half of ReplaceOAuthGrant's bump.
		if _, err := tx.NewUpdate().
			Model((*Source)(nil)).
			Set("config = (config - ? - ? - ?) || ?::jsonb",
				oauthGrantedStrategyKey, oauthGrantedScopesKey, oauthAccountEmailKey, string(metaJSON)).
			Set("credentials_updated_at = now()").
			Set("updated_at = now()").
			Where("credential_id = ?", credentialID).
			Where("project_id = ?", projectID).
			Exec(ctx); err != nil {
			r.log.Error("failed to refresh sources on the saved credential", logger.Error(err))
			return apperror.NewDatabase("failed to refresh sources on the saved credential", err)
		}

		// Reactivation is its own statement so the returned ids are exactly the
		// sources this reconnect repaired. A RETURNING on the statement above would
		// report every matched row — a disabled sibling matches credential_id and
		// project_id too — and the caller would clear an item nobody fixed (#2414).
		if err := tx.NewUpdate().
			Model((*Source)(nil)).
			Set("status = ?", SourceStatusActive).
			Set("last_error = NULL").
			Set("last_error_at = NULL").
			Set("error_count = 0").
			Set("updated_at = now()").
			Where("credential_id = ?", credentialID).
			Where("project_id = ?", projectID).
			Where("status = ?", SourceStatusNeedsReconnect).
			Returning("id").
			Scan(ctx, &reactivated); err != nil {
			r.log.Error("failed to reactivate sources on the saved credential", logger.Error(err))
			return apperror.NewDatabase("failed to reactivate sources on the saved credential", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reactivated, nil
}

// UpdateCredential updates a saved credential's name and encrypted payload,
// scoped to its project.
//
// When the secret is replaced (replaceSecret), it also moves the grant epoch of
// every source that reads the credential: each of them now reads a different
// grant, so a sync job that started before this write and fails afterwards must
// not park the source the user has just repaired (#2425). A rename leaves the
// epochs alone — the grants did not change.
//
// The two writes share one transaction, so a crash cannot leave a fresh secret
// with stale epochs, which would re-open the park race for every source on the
// credential (#2457 is the same class on the reconnect path).
func (r *Repository) UpdateCredential(ctx context.Context, cred *SourceCredential, replaceSecret bool) (*SourceCredential, error) {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Model((*SourceCredential)(nil)).
			Set("name = ?", cred.Name).
			Set("encrypted_credentials = ?", cred.EncryptedCredentials).
			Set("updated_at = now()").
			Where("id = ?", cred.ID).
			Where("project_id = ?", cred.ProjectID).
			Exec(ctx)
		if err != nil {
			r.log.Error("failed to update source credential", logger.Error(err))
			return apperror.NewDatabase("failed to update source credential", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return apperror.ErrNotFound
		}
		if !replaceSecret {
			return nil
		}
		if _, err := tx.NewUpdate().
			Model((*Source)(nil)).
			Set("credentials_updated_at = now()").
			Where("credential_id = ?", cred.ID).
			Where("project_id = ?", cred.ProjectID).
			Exec(ctx); err != nil {
			r.log.Error("failed to move the grant epoch for a saved credential", logger.Error(err))
			return apperror.NewDatabase("failed to move the grant epoch for a saved credential", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
