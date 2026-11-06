package graph

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// CredentialGrantProject is the synthetic credential_kind used only by a grant
// row to mean "any project-scoped credential may act as this source". It is not
// a kind a caller authenticates with — the auth layer never produces it — but a
// grant whose kind is 'project' and whose credential_id is NULL is the wildcard
// the migration backfill (00265) seeds so pre-existing integrations keep
// working. An exact grant (credential_kind + credential_id) is the explicit
// binding a new source gets.
const CredentialGrantProject = "project"

// SourceGrant is a project-scoped grant of source authority to a credential
// (kb.source_grants). It is the row that answers "may this caller act as this
// source?" — revoking authority is a delete, auditing it is a query, and the
// granting actor is recorded rather than implied.
type SourceGrant struct {
	bun.BaseModel `bun:"table:kb.source_grants,alias:sg"`

	ID             uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID      uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	SourceID       uuid.UUID  `bun:"source_id,type:uuid,notnull" json:"source_id"`
	CredentialKind string     `bun:"credential_kind,notnull" json:"credential_kind"`
	CredentialID   *uuid.UUID `bun:"credential_id,type:uuid" json:"credential_id,omitempty"`
	GrantedByType  string     `bun:"granted_by_type,notnull,default:'system'" json:"granted_by_type"`
	GrantedByID    *uuid.UUID `bun:"granted_by_id,type:uuid" json:"granted_by_id,omitempty"`
	GrantedAt      time.Time  `bun:"granted_at,notnull,default:now()" json:"granted_at"`
}

// HasSourceGrant reports whether the credential (kind, id) holds authority to
// act as sourceID in projectID. Two grant shapes satisfy it: an exact match on
// the credential, and the project wildcard (credential_kind = 'project',
// credential_id NULL) that the migration backfill seeds for pre-existing
// sources. The wildcard is kind-agnostic: it authorises any project credential.
func (r *Repository) HasSourceGrant(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, credentialKind string, credentialID *uuid.UUID) (bool, error) {
	// Normalise the credential id to a plain value so a nil pointer renders as
	// SQL NULL (a typed nil would confuse the query formatter).
	var credID any
	if credentialID != nil {
		credID = *credentialID
	}

	var exists bool
	err := db.NewRaw(`
		SELECT EXISTS (
			SELECT 1 FROM kb.source_grants
			WHERE project_id = ?
			  AND source_id = ?
			  AND (
			        (credential_kind = ? AND credential_id IS NOT DISTINCT FROM ?)
			     OR (credential_kind = 'project' AND credential_id IS NULL)
			  )
		)`,
		projectID, sourceID, credentialKind, credID,
	).Scan(ctx, &exists)
	if err != nil {
		return false, apperror.NewDatabase("Database operation failed", err)
	}
	return exists, nil
}

// InsertSourceGrant writes one grant row inside the caller's transaction.
func (r *Repository) InsertSourceGrant(ctx context.Context, db bun.IDB, g *SourceGrant) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now()
	}
	if _, err := db.NewInsert().Model(g).Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// DeleteSourceGrant revokes one grant. A nil credentialID targets the wildcard
// grant for that kind; a non-nil credentialID targets the exact grant. It is a
// delete, not an update: authority once revoked is gone, and in-flight writes
// already past the check still commit (a transaction does not re-check).
func (r *Repository) DeleteSourceGrant(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, credentialKind string, credentialID *uuid.UUID) error {
	q := db.NewDelete().
		Model((*SourceGrant)(nil)).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("credential_kind = ?", credentialKind)
	if credentialID != nil {
		q = q.Where("credential_id = ?", *credentialID)
	} else {
		q = q.Where("credential_id IS NULL")
	}
	if _, err := q.Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// ListSourceGrants returns the grants held for one source, oldest first. It is
// the audit read: which credentials may act as this source, granted by whom and
// when.
func (r *Repository) ListSourceGrants(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID) ([]*SourceGrant, error) {
	var grants []*SourceGrant
	err := db.NewSelect().
		Model(&grants).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		OrderExpr("granted_at ASC, id ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return grants, nil
}
