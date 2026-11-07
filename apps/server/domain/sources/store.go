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

// InsertSourceGrant records that the concrete credential (credentialKind,
// credentialID) may act as sourceID in projectID. It is written from the
// source-create path so the credential that created a source can push as it: a
// source created after migration 00265 carries no wildcard, so without this
// exact grant its creator could never push as it.
//
// Only a concrete credential (an api_key with a non-null id) is ever granted;
// a session creator gets no row — its source is pushed by the system sync
// worker, which is exempt from the grant check — and a ('session', NULL) row is
// deliberately never written because it would read back as a hidden wildcard.
func (r *Repository) InsertSourceGrant(ctx context.Context, projectID, sourceID uuid.UUID, credentialKind string, credentialID uuid.UUID) error {
	_, err := r.db.NewRaw(`
		INSERT INTO kb.source_grants (project_id, source_id, credential_kind, credential_id, granted_by_type)
		VALUES (?, ?, ?, ?, 'system')
		ON CONFLICT DO NOTHING`,
		projectID, sourceID, credentialKind, credentialID,
	).Exec(ctx)
	if err != nil {
		r.log.Error("failed to record source grant", logger.Error(err))
		return apperror.NewDatabase("failed to record source grant", err)
	}
	return nil
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
//
// `status` is deliberately not written here (#2437). It is a lifecycle decision
// owned by the statements that make it — MarkSourceNeedsReconnect,
// ReplaceOAuthGrant, ReplaceSavedCredentialGrant, UpdateStatus, and the sync
// success path — while this method writes a snapshot the caller read earlier. A
// failing sync that parks the source inside the caller's read→write window would
// otherwise have its `active` overwritten by the snapshot, silently returning a
// dead-grant source to the scheduler; the error columns would be cleared with it.
// An explicit enable/disable is a transition and goes through UpdateStatus.
func (r *Repository) Update(ctx context.Context, src *Source) (*Source, error) {
	q := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("display_name = ?", src.DisplayName).
		Set("config = ?", src.Config).
		Set("sync_policy = ?", src.SyncPolicy).
		Set("sync_interval_minutes = ?", src.SyncIntervalMinutes).
		Set("credential_id = ?", src.CredentialID).
		Set("updated_at = now()").
		Where("id = ?", src.ID).
		Where("project_id = ?", src.ProjectID)

	// The epoch moves only when the credential the source reads changes (#2425).
	//
	// Comparing the *value* is what makes that true, not testing whether a blob is
	// present: Service.Update re-reads the row with GetByIDWithCredentials, so every
	// settings save carries the stored blob and would otherwise look like a
	// credential write — moving the epoch on a rename would let a rename suppress a
	// legitimate park and put back the every-cadence failing sync #2406 removed.
	// The comparison is against the row, so it stays correct under a concurrent
	// write.
	//
	// Two cases are deliberately conservative and can move the epoch for a change
	// this source does not actually read: an inline blob written on a source that
	// also references a credential is dropped rather than read, and re-selecting
	// the same credential drops a stale rotation-written blob. Either suppresses at
	// most one park (a source admits one active job), so it costs one ordinary
	// failure report rather than a loop.
	switch {
	case len(src.EncryptedCredentials) > 0:
		// A blob is being written: it only changes the grant if it differs from
		// the stored one (a save that re-submits the same value changes nothing).
		q = q.Set("credentials_updated_at = CASE WHEN credential_id IS DISTINCT FROM ? OR encrypted_credentials IS DISTINCT FROM ?::bytea THEN now() ELSE credentials_updated_at END", src.CredentialID, src.EncryptedCredentials).
			Set("encrypted_credentials = ?", src.EncryptedCredentials)
	case src.CredentialID != nil:
		// No blob is written: a referenced source carries no inline copy, so a
		// stored one is being dropped, and the source now reads the credential.
		q = q.Set("credentials_updated_at = CASE WHEN credential_id IS DISTINCT FROM ? OR encrypted_credentials IS NOT NULL THEN now() ELSE credentials_updated_at END", src.CredentialID).
			Set("encrypted_credentials = NULL")
	default:
		// Nothing credential-shaped is written; only a new reference is a change.
		q = q.Set("credentials_updated_at = CASE WHEN credential_id IS DISTINCT FROM ? THEN now() ELSE credentials_updated_at END", src.CredentialID)
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

// UpdateStatus applies an explicit lifecycle transition on a source — an enable
// or a disable submitted by the request. It is separate from Update because
// `status` is a lifecycle decision, not a field of the snapshot a settings save
// read (#2437): a plain edit must leave whatever park/unpark won the race alone,
// while an explicit enable/disable is allowed to override it.
//
// A transition to active from anything else clears the recorded failure with it.
// Leaving it behind leaves an *active* source still showing "the connection
// expired or was revoked; reconnect the source", which reads as a live problem on
// a source that was just retried. The condition is evaluated against the row, so
// an already-active source keeps the error its last sync recorded — that one is
// live. The whole transition is one statement, so a concurrent park is either
// before it (and overridden, as the user asked) or after it (and wins), never a
// mix of the two.
func (r *Repository) UpdateStatus(ctx context.Context, id, projectID uuid.UUID, status string) error {
	res, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("status = ?", status).
		Set("last_error = CASE WHEN status <> ? AND ? = ? THEN NULL ELSE last_error END",
			SourceStatusActive, status, SourceStatusActive).
		Set("last_error_at = CASE WHEN status <> ? AND ? = ? THEN NULL ELSE last_error_at END",
			SourceStatusActive, status, SourceStatusActive).
		Set("error_count = CASE WHEN status <> ? AND ? = ? THEN 0 ELSE error_count END",
			SourceStatusActive, status, SourceStatusActive).
		Set("updated_at = now()").
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update source status", logger.Error(err))
		return apperror.NewDatabase("failed to update source status", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
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
// source: the connect-time config metadata is replaced and the recorded error is
// cleared, because a dead grant is the failure a reconnect resolves. Every other
// column — name, sync policy, cursor, the source's own config keys — is left
// alone, so a reconnect cannot reset the sync scope or undo a pause (#2107).
// The blob is written only for a source that carries its own credentials; a
// source reading a saved credential gets it replaced through
// ReplaceSavedCredentialGrant instead (#2418).
//
// Status is restored from `needs_reconnect` to `active` — the parked state is
// exactly what a fresh consent clears, and it is what the scheduler was skipping.
// A `disabled` source keeps its pause: reconnecting restores access, not the
// operator's decision to stop syncing.
func (r *Repository) ReplaceOAuthGrant(ctx context.Context, id, projectID uuid.UUID, blob []byte, config map[string]any) error {
	if len(blob) == 0 {
		return apperror.NewValidation("credentials blob is empty")
	}
	res, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		// A source created from a saved credential reads the saved credential at
		// runtime (Service.credentialsForSource) and never its own row, so the grant
		// has to land there — writing it here would leave a stale copy that nothing
		// reads and the next edit would drop (#2418). The reconnect path replaces
		// the saved credential's blob through ReplaceSavedCredentialGrant and clears
		// the inline copy here.
		Set("encrypted_credentials = CASE WHEN credential_id IS NULL THEN ?::bytea ELSE NULL END", blob).
		Set("config = ?", config).
		Set("last_error = NULL").
		Set("last_error_at = NULL").
		Set("error_count = 0").
		Set("status = CASE WHEN status = ? THEN ? ELSE status END", SourceStatusNeedsReconnect, SourceStatusActive).
		// A consent replaces the grant, so the epoch moves: a sync job still
		// running against the old grant must not park the source afterwards
		// (#2425).
		Set("credentials_updated_at = now()").
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
