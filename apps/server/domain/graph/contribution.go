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
	// MergedFromVersionID is the version this contribution was carried from,
	// when it was not written onto the row it describes: a merge union, a fork
	// copy, or a new version that inherits its predecessor's contributors. It is
	// null when the source's record arrived on this row.
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
//
// It upserts the present row of (version, source) rather than blindly inserting
// one: a new version inherits its predecessor's contributions (CreateVersion),
// so the source that wrote the version records its revised contribution onto a
// version that already carries its previous one. Replacing it in place is what
// keeps "one present row per version per source" true — the invariant the
// partial unique index uq_provenance_contributions_version_source states — and
// first_seen_at is deliberately left alone so the source's original sighting
// survives a revision.
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
	if _, err := db.NewInsert().Model(c).
		On(`CONFLICT (subject_kind, subject_version_id, source_id) WHERE state = 'present' DO UPDATE SET
			external_id = EXCLUDED.external_id,
			external_revision = EXCLUDED.external_revision,
			external_revision_seq = EXCLUDED.external_revision_seq,
			merged_from_version_id = EXCLUDED.merged_from_version_id,
			last_seen_at = EXCLUDED.last_seen_at`).
		Exec(ctx); err != nil {
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
	return r.copyContributions(ctx, db, projectID, kind, fromVersionIDs, toVersionID, toBranchID, true)
}

// address is one source's record on the destination branch: the key the partial
// unique indexes uq_provenance_contributions_present_{main,branch} enforce.
type address struct {
	sourceID   uuid.UUID
	externalID string
}

// crossesBranch reports whether a copy reads rows from a branch other than the
// one it writes to. Such a copy can land on an address the destination branch
// already holds; a same-branch carry-forward cannot, because the row it copies
// is the row it supersedes.
func crossesBranch(rows []ProvenanceContribution, toBranchID *uuid.UUID) bool {
	for _, row := range rows {
		a, b := row.BranchID, toBranchID
		if a == nil || b == nil {
			if a != nil || b != nil {
				return true
			}
			continue
		}
		if *a != *b {
			return true
		}
	}
	return false
}

// copyContributions is the shared body of the union-on-merge and the
// carry-forward-on-version-write rules.
//
// A copied contribution is superseded at the version it arrived on before it is
// written at the new one, because the addressing index allows only one present
// row per (project, branch, kind, source, external_id): marking the old row
// superseded — never deleting it — is what keeps both the live set correct and
// the arrival history recoverable.
//
// includeDestination adds the rows already present on the destination version as
// candidates. Only a merge needs it: it writes its target version through
// CreateVersion, which has already carried the target head's contributions
// forward, so the union has to dedupe against them rather than collide with (or
// overwrite them with a lower) revision. The plain inheritance call skips the
// lookup because the destination version it writes is brand new.
func (r *Repository) copyContributions(
	ctx context.Context,
	db bun.IDB,
	projectID uuid.UUID,
	kind string,
	fromVersionIDs []uuid.UUID,
	toVersionID uuid.UUID,
	toBranchID *uuid.UUID,
	includeDestination bool,
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

	var rows []ProvenanceContribution
	if err := q.Scan(ctx, &rows); err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	// Contributions the destination version already carries — the ones
	// CreateVersion inherited from the head a merge is superseding.
	var inherited []ProvenanceContribution
	if includeDestination {
		if err := db.NewSelect().
			Model(&inherited).
			Where("project_id = ?", projectID).
			Where("subject_kind = ?", kind).
			Where("subject_version_id = ?", toVersionID).
			Where("state = ?", ContributionStatePresent).
			Scan(ctx); err != nil {
			return 0, apperror.NewDatabase("Database operation failed", err)
		}
	}

	// A source present on both sides appears once, keeping the higher revision
	// sequence (the union rule the `_sources` array already uses). The version
	// row carries at most one record per source — that is what the unique index
	// on (subject_version_id, source_id) enforces — so a source that arrived
	// with two different external ids on the joined objects keeps the newer
	// record, and the tie is broken on the external id so the outcome does not
	// depend on row order.
	//
	// Each candidate remembers which version its record arrived on: a row read
	// from a source version arrived there, an inherited row keeps whatever
	// arrival version it already recorded (NULL if it was written onto the
	// version it describes).
	type candidate struct {
		row        ProvenanceContribution
		mergedFrom *uuid.UUID
	}
	bySource := make(map[uuid.UUID]candidate, len(rows)+len(inherited))
	ids := make([]uuid.UUID, 0, len(rows))
	consider := func(c candidate) {
		cur, ok := bySource[c.row.SourceID]
		switch {
		case !ok:
			bySource[c.row.SourceID] = c
		case c.row.ExternalRevisionSeq > cur.row.ExternalRevisionSeq:
			bySource[c.row.SourceID] = c
		case c.row.ExternalRevisionSeq == cur.row.ExternalRevisionSeq && c.row.ExternalID < cur.row.ExternalID:
			bySource[c.row.SourceID] = c
		}
	}
	for _, row := range rows {
		ids = append(ids, row.ID)
		arrivedOn := row.SubjectVersionID
		consider(candidate{row: row, mergedFrom: &arrivedOn})
	}
	for _, row := range inherited {
		consider(candidate{row: row, mergedFrom: row.MergedFromVersionID})
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

	// A cross-branch copy can collide with a record the destination branch
	// already carries: the same source may have pushed that external id on the
	// branch itself, and a branch-scoped delete of a main-only object copies the
	// main line's contributors onto a branch tombstone while the branch's own
	// object for that record already holds the addressing slot. The branch's row
	// is the live one there — writing the copy would violate
	// uq_provenance_contributions_present_{main,branch} and leave the branch
	// addressing one record twice — so a candidate the destination branch
	// already addresses is skipped, unless the row it collides with is one this
	// copy supersedes.
	occupied := map[address]struct{}{}
	if crossesBranch(rows, toBranchID) {
		superseded := make(map[uuid.UUID]struct{}, len(ids))
		for _, id := range ids {
			superseded[id] = struct{}{}
		}
		// The destination version's own rows are not foreign occupants: they
		// are the ones this copy is here to write over (includeDestination), so
		// only rows on some *other* live version of the branch can be in the way.
		//
		// Only an address in hand can be occupied, so the lookup is narrowed to
		// those addresses. A merge writes many objects in one transaction, and
		// reading every present row of the destination branch made it cost
		// O(merged objects × branch contributions); the row-wise IN is one index
		// probe per candidate on uq_provenance_contributions_present_{main,branch}
		// instead (#125).
		addrs := make([]address, 0, len(bySource))
		for _, cand := range bySource {
			addrs = append(addrs, address{sourceID: cand.row.SourceID, externalID: cand.row.ExternalID})
		}
		if len(addrs) > 0 {
			placeholders := make([]string, len(addrs))
			args := make([]any, 0, len(addrs)*2+5)
			for i, a := range addrs {
				placeholders[i] = "(?, ?)"
				args = append(args, a.sourceID, a.externalID)
			}

			// Both address indexes are partial, and the planner can only prove a
			// partial predicate from a plain comparison: IS NOT DISTINCT FROM is
			// semantically the same but hid the index and fell back to a full
			// branch scan.
			branchPredicate := "branch_id IS NULL"
			if toBranchID != nil {
				branchPredicate = "branch_id = ?"
			}
			args = append(args, projectID, kind)
			if toBranchID != nil {
				args = append(args, *toBranchID)
			}
			args = append(args, toVersionID, ContributionStatePresent)

			var holders []struct {
				ID         uuid.UUID `bun:"id"`
				SourceID   uuid.UUID `bun:"source_id"`
				ExternalID string    `bun:"external_id"`
			}
			query := `SELECT id, source_id, external_id
				FROM kb.provenance_contributions
				WHERE (source_id, external_id) IN (` + strings.Join(placeholders, ", ") + `)
				  AND project_id = ? AND subject_kind = ?
				  AND ` + branchPredicate + `
				  AND subject_version_id <> ?
				  AND state = ?`
			if err := db.NewRaw(query, args...).Scan(ctx, &holders); err != nil {
				return 0, apperror.NewDatabase("Database operation failed", err)
			}
			for _, holder := range holders {
				if _, moved := superseded[holder.ID]; moved {
					continue
				}
				occupied[address{sourceID: holder.SourceID, externalID: holder.ExternalID}] = struct{}{}
			}
		}
	}

	copied := 0
	for _, cand := range bySource {
		row := cand.row
		if _, taken := occupied[address{sourceID: row.SourceID, externalID: row.ExternalID}]; taken {
			continue
		}
		c := &ProvenanceContribution{
			ProjectID:           projectID,
			BranchID:            toBranchID,
			SubjectKind:         kind,
			SubjectVersionID:    toVersionID,
			SourceID:            row.SourceID,
			ExternalID:          row.ExternalID,
			ExternalRevision:    row.ExternalRevision,
			ExternalRevisionSeq: row.ExternalRevisionSeq,
			MergedFromVersionID: cand.mergedFrom,
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

// fillObjectContributions attaches the contributions of each response's version
// row. One query serves the whole slice, so a history or list of N versions
// costs one contribution read rather than N.
//
// A version with no contribution keeps a nil slice and the field is omitted: no
// source backs it, which is every row Memory synthesized itself.
func (s *Service) fillObjectContributions(ctx context.Context, projectID uuid.UUID, items []*GraphObjectResponse) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if _, dup := seen[item.ID]; dup {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	byVersion, err := s.repo.GetContributions(ctx, projectID, ContributionSubjectObject, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		item.Contributions = byVersion[item.ID]
	}
	return nil
}

// fillRelationshipContributions is the relationship half of
// fillObjectContributions, reading subject_kind = relationship.
func (s *Service) fillRelationshipContributions(ctx context.Context, projectID uuid.UUID, items []*GraphRelationshipResponse) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if _, dup := seen[item.ID]; dup {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	byVersion, err := s.repo.GetContributions(ctx, projectID, ContributionSubjectRelationship, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		item.Contributions = byVersion[item.ID]
	}
	return nil
}
