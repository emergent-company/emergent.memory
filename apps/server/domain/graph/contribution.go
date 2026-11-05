package graph

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Contribution subject kinds and states (migration 00261).
const (
	ContributionSubjectObject       = "object"
	ContributionSubjectRelationship = "relationship"

	ContributionStatePresent    = "present"
	ContributionStateSuperseded = "superseded"
	ContributionStateWithdrawn  = "withdrawn"
)

// ProvenanceContribution records that a source contributes a record to one
// version row of an object or relationship.
//
// It is keyed to the version row, not to (canonical_id, version): a branch fork
// re-uses the canonical id at version = 1, so the pair is not unique across
// branches.
//
// A contribution is never deleted — a re-contribution supersedes the previous
// row and a withdrawal changes the state — so the version each external
// revision arrived on stays recoverable from the table alone.
type ProvenanceContribution struct {
	bun.BaseModel `bun:"table:kb.provenance_contributions,alias:pc"`

	ID                  uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID           uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	BranchID            *uuid.UUID `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	SubjectKind         string     `bun:"subject_kind,notnull" json:"subject_kind"`
	SubjectVersionID    uuid.UUID  `bun:"subject_version_id,type:uuid,notnull" json:"subject_version_id"`
	SourceID            uuid.UUID  `bun:"source_id,type:uuid,notnull" json:"source_id"`
	ExternalID          string     `bun:"external_id,notnull" json:"external_id"`
	ExternalRevision    string     `bun:"external_revision,notnull,default:''" json:"external_revision"`
	ExternalRevisionSeq int64      `bun:"external_revision_seq,notnull,default:0" json:"external_revision_seq"`
	MergedFromVersionID *uuid.UUID `bun:"merged_from_version_id,type:uuid" json:"merged_from_version_id,omitempty"`
	State               string     `bun:"state,notnull,default:'present'" json:"state"`
	FirstSeenAt         time.Time  `bun:"first_seen_at,notnull,default:now()" json:"first_seen_at"`
	LastSeenAt          time.Time  `bun:"last_seen_at,notnull,default:now()" json:"last_seen_at"`
	WithdrawnAt         *time.Time `bun:"withdrawn_at" json:"withdrawn_at,omitempty"`
}

// SourceGeneration records one applied changeset for a source. It is written in
// the same transaction as the data it describes, so a reader never observes a
// generation whose items did not all land.
type SourceGeneration struct {
	bun.BaseModel `bun:"table:kb.source_generations,alias:sg"`

	ID         uuid.UUID `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID  uuid.UUID `bun:"project_id,type:uuid,notnull" json:"project_id"`
	SourceID   uuid.UUID `bun:"source_id,type:uuid,notnull" json:"source_id"`
	Generation int64     `bun:"generation,notnull" json:"generation"`
	ItemCount  int       `bun:"item_count,notnull,default:0" json:"item_count"`
	AppliedAt  time.Time `bun:"applied_at,notnull,default:now()" json:"applied_at"`
}

// SourceExistsInProject reports whether a kb.sources row belongs to the project.
// A changeset may only be pushed by a source registered to the caller's project;
// ownership *enforcement* is slice 2, this is only the scoping check.
func (r *Repository) SourceExistsInProject(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID) (bool, error) {
	var exists bool
	err := db.NewRaw(
		`SELECT EXISTS (SELECT 1 FROM kb.sources WHERE id = ? AND project_id = ?)`,
		sourceID, projectID,
	).Scan(ctx, &exists)
	if err != nil {
		return false, apperror.NewDatabase("Database operation failed", err)
	}
	return exists, nil
}

// GetPresentContribution finds the live contribution a source has for one of
// its external records on a branch. It is the apply path's lookup: a hit means
// the record already exists as a version row of an object/relationship.
//
// The branch is part of the key: a fork copies contributions onto its own
// version rows, so the same external id legitimately has one present
// contribution per branch, and a main-line push must not address the fork's
// copy.
//
// Returns (nil, nil) when the source does not currently contribute that record;
// the partial unique index on (project_id, [branch_id,] subject_kind, source_id,
// external_id) where state = 'present' serves it.
func (r *Repository) GetPresentContribution(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, branchID *uuid.UUID, kind, externalID string) (*ProvenanceContribution, error) {
	var c ProvenanceContribution
	q := db.NewSelect().
		Model(&c).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("subject_kind = ?", kind).
		Where("external_id = ?", externalID).
		Where("state = ?", ContributionStatePresent)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}
	if err := q.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return &c, nil
}

// GetContributions returns the contributions of one object version, grouped by
// version id. One query regardless of how many rows are asked for, so a list
// response does not become N+1.
func (r *Repository) GetContributions(ctx context.Context, projectID uuid.UUID, kind string, versionIDs []uuid.UUID) (map[uuid.UUID][]*ProvenanceContribution, error) {
	out := make(map[uuid.UUID][]*ProvenanceContribution, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}

	var rows []ProvenanceContribution
	err := r.db.NewSelect().
		Model(&rows).
		Where("project_id = ?", projectID).
		Where("subject_kind = ?", kind).
		Where("subject_version_id IN (?)", bun.In(versionIDs)).
		OrderExpr("first_seen_at ASC, source_id ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	for i := range rows {
		c := rows[i]
		out[c.SubjectVersionID] = append(out[c.SubjectVersionID], &c)
	}
	return out, nil
}

// InsertContribution writes one contribution row inside the caller's
// transaction.
func (r *Repository) InsertContribution(ctx context.Context, db bun.IDB, c *ProvenanceContribution) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now()
	if c.FirstSeenAt.IsZero() {
		c.FirstSeenAt = now
	}
	if c.LastSeenAt.IsZero() {
		c.LastSeenAt = now
	}
	if c.State == "" {
		c.State = ContributionStatePresent
	}
	if _, err := db.NewInsert().Model(c).Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// SupersedeContribution marks a contribution as superseded by a newer version
// of the same record. It is never a delete: the row keeps the version it
// arrived on, which is what makes a revision history recoverable.
func (r *Repository) SupersedeContribution(ctx context.Context, db bun.IDB, id uuid.UUID) error {
	_, err := db.NewUpdate().
		Model((*ProvenanceContribution)(nil)).
		Set("state = ?", ContributionStateSuperseded).
		Set("last_seen_at = ?", time.Now()).
		Where("id = ?", id).
		Where("state = ?", ContributionStatePresent).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// WithdrawContribution flips a present contribution to withdrawn. The object it
// belongs to is not touched: withdrawal is a property of the contribution, not
// of the row.
func (r *Repository) WithdrawContribution(ctx context.Context, db bun.IDB, id uuid.UUID, at time.Time) error {
	_, err := db.NewUpdate().
		Model((*ProvenanceContribution)(nil)).
		Set("state = ?", ContributionStateWithdrawn).
		Set("withdrawn_at = ?", at).
		Set("last_seen_at = ?", at).
		Where("id = ?", id).
		Where("state = ?", ContributionStatePresent).
		Exec(ctx)
	if err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// CopyContributionsToVersion carries every present contribution of a source
// version onto a newly written version (a merge), recording the version each
// contribution arrived on in merged_from_version_id.
//
// This is the union on merge: the target version reports every contributing
// source of both sides, and the merge itself stays attributed to the merging
// actor rather than to a contributing source.
func (r *Repository) CopyContributionsToVersion(ctx context.Context, db bun.IDB, projectID uuid.UUID, kind string, fromVersionIDs []uuid.UUID, toVersionID uuid.UUID, toBranchID *uuid.UUID) (int, error) {
	return r.copyContributions(ctx, db, projectID, kind, fromVersionIDs, toVersionID, toBranchID, nil)
}

// CarryForwardContributions moves the present contributions of one version onto
// a newly written one, except the source that wrote the new version. A revision
// by one source therefore does not evict the others from the live version: the
// set of sources backing an object survives edits, just as it survives a merge.
func (r *Repository) CarryForwardContributions(ctx context.Context, db bun.IDB, projectID uuid.UUID, kind string, fromVersionID, toVersionID uuid.UUID, toBranchID *uuid.UUID, excludeSource uuid.UUID) (int, error) {
	return r.copyContributions(ctx, db, projectID, kind, []uuid.UUID{fromVersionID}, toVersionID, toBranchID, &excludeSource)
}

// copyContributions is the shared body of the union-on-merge and the
// carry-forward-on-revision rules.
//
// A copied contribution is superseded at the version it arrived on before it is
// re-inserted at the new one, because the addressing index allows only one
// present row per (project, branch, kind, source, external_id): marking the old
// row superseded — never deleting it — is what keeps both the live set correct
// and the arrival history recoverable.
func (r *Repository) copyContributions(
	ctx context.Context,
	db bun.IDB,
	projectID uuid.UUID,
	kind string,
	fromVersionIDs []uuid.UUID,
	toVersionID uuid.UUID,
	toBranchID *uuid.UUID,
	excludeSource *uuid.UUID,
) (int, error) {
	if len(fromVersionIDs) == 0 {
		return 0, nil
	}

	q := db.NewSelect().
		Model((*ProvenanceContribution)(nil)).
		Where("project_id = ?", projectID).
		Where("subject_kind = ?", kind).
		Where("subject_version_id IN (?)", bun.In(fromVersionIDs)).
		Where("state = ?", ContributionStatePresent)
	if excludeSource != nil {
		q = q.Where("source_id <> ?", *excludeSource)
	}

	var rows []ProvenanceContribution
	if err := q.Scan(ctx, &rows); err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	// A source present on both sides appears once, keeping the higher revision
	// sequence (the union rule the `_sources` array already uses). The version
	// row carries at most one record per source — that is what the unique index
	// on (subject_version_id, source_id) enforces — so a source that arrived
	// with two different external ids on the joined objects keeps the newer
	// record, and the tie is broken on the external id so the outcome does not
	// depend on row order.
	bySource := make(map[uuid.UUID]ProvenanceContribution, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		cur, ok := bySource[row.SourceID]
		switch {
		case !ok:
			bySource[row.SourceID] = row
		case row.ExternalRevisionSeq > cur.ExternalRevisionSeq:
			bySource[row.SourceID] = row
		case row.ExternalRevisionSeq == cur.ExternalRevisionSeq && row.ExternalID < cur.ExternalID:
			bySource[row.SourceID] = row
		}
	}

	// Only rows on the destination branch are superseded. The source branch
	// keeps its own contributions: a merge must not take a branch's record away
	// from it, or the next push on that branch would find no anchor and create
	// a duplicate object instead of versioning the copy it already has.
	if _, err := db.NewUpdate().
		Model((*ProvenanceContribution)(nil)).
		Set("state = ?", ContributionStateSuperseded).
		Set("last_seen_at = ?", time.Now()).
		Where("id IN (?)", bun.In(ids)).
		Where("state = ?", ContributionStatePresent).
		Where("branch_id IS NOT DISTINCT FROM ?", toBranchID).
		Exec(ctx); err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}

	copied := 0
	for _, row := range bySource {
		from := row.SubjectVersionID
		c := &ProvenanceContribution{
			ProjectID:           projectID,
			BranchID:            toBranchID,
			SubjectKind:         kind,
			SubjectVersionID:    toVersionID,
			SourceID:            row.SourceID,
			ExternalID:          row.ExternalID,
			ExternalRevision:    row.ExternalRevision,
			ExternalRevisionSeq: row.ExternalRevisionSeq,
			MergedFromVersionID: &from,
			State:               ContributionStatePresent,
			FirstSeenAt:         row.FirstSeenAt,
		}
		if err := r.InsertContribution(ctx, db, c); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// bulkCopyContributions carries the present contributions of freshly copied
// HEAD rows onto their new copies, in one statement per batch of a fork. Each
// copy records the row it came from in merged_from_version_id, so the original
// arrival is still traceable from the fork.
func (r *Repository) bulkCopyContributions(
	ctx context.Context,
	projectID uuid.UUID,
	targetBranchID *uuid.UUID,
	kind string,
	pairs map[uuid.UUID]uuid.UUID,
) error {
	if len(pairs) == 0 {
		return nil
	}

	pairSQL := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*2+2)
	args = append(args, targetBranchID)
	for from, to := range pairs {
		pairSQL = append(pairSQL, "(?::uuid, ?::uuid)")
		args = append(args, from, to)
	}
	args = append(args, kind)

	query := `
		INSERT INTO kb.provenance_contributions
			(project_id, branch_id, subject_kind, subject_version_id, source_id,
			 external_id, external_revision, external_revision_seq,
			 merged_from_version_id, state, first_seen_at, last_seen_at)
		SELECT c.project_id, ?::uuid, c.subject_kind, m.to_id::uuid, c.source_id,
		       c.external_id, c.external_revision, c.external_revision_seq,
		       c.subject_version_id, 'present', c.first_seen_at, now()
		FROM kb.provenance_contributions c
		JOIN (VALUES ` + strings.Join(pairSQL, ", ") + `) AS m(from_id, to_id)
		  ON m.from_id::uuid = c.subject_version_id
		WHERE c.state = 'present' AND c.subject_kind = ?`

	if _, err := r.db.NewRaw(query, args...).Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// NextSourceGeneration returns the next generation number for a source. Reading
// the maximum and inserting inside the same transaction is safe because the
// unique index on (source_id, generation) refuses a concurrent duplicate.
func (r *Repository) NextSourceGeneration(ctx context.Context, db bun.IDB, sourceID uuid.UUID) (int64, error) {
	var next int64
	err := db.NewRaw(
		`SELECT COALESCE(MAX(generation), 0) + 1 FROM kb.source_generations WHERE source_id = ?`,
		sourceID,
	).Scan(ctx, &next)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 1, nil
		}
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	return next, nil
}

// InsertSourceGeneration records the applied changeset. Written inside the
// data's transaction so a generation is never visible without its items.
func (r *Repository) InsertSourceGeneration(ctx context.Context, db bun.IDB, g *SourceGeneration) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	if g.AppliedAt.IsZero() {
		g.AppliedAt = time.Now()
	}
	if _, err := db.NewInsert().Model(g).Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// GetObjectVersionByID reads one specific object version row, HEAD or not. The
// apply path needs it because a contribution points at the version it arrived
// on, which is not necessarily the current head.
func (r *Repository) GetObjectVersionByID(ctx context.Context, db bun.IDB, projectID, versionID uuid.UUID) (*GraphObject, error) {
	var obj GraphObject
	err := db.NewSelect().
		Model(&obj).
		Column(graphObjectDetailColumns...).
		Where("id = ?", versionID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return &obj, nil
}

// GetRelationshipVersionByID reads one specific relationship version row.
func (r *Repository) GetRelationshipVersionByID(ctx context.Context, db bun.IDB, projectID, versionID uuid.UUID) (*GraphRelationship, error) {
	var rel GraphRelationship
	err := db.NewSelect().
		Model(&rel).
		Where("id = ?", versionID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return &rel, nil
}
