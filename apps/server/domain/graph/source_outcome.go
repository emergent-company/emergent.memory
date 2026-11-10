package graph

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Source record outcomes (migration 00284).
//
// An outcome is what the graph currently holds for one source record, keyed by
// the source's own addressing standpoint `(project, source, external_id)` — the
// key the changeset apply resolves a record with. It is written where the
// decision is taken (apply, withdrawal, merge, adoption) and never derived at
// read time: a cursor cannot page a meaning recomputed on every request.
const (
	// SourceOutcomeMaterialized: the source's record backs a graph object or
	// relationship it currently owns.
	SourceOutcomeMaterialized = "materialized"
	// SourceOutcomeMerged: the record's object was merged into another object.
	// canonical_id names the destination the source should follow.
	SourceOutcomeMerged = "merged"
	// SourceOutcomeAdopted: the record's object changed owner to another source.
	// adopted_by_source_id names the adopter and canonical_id the destination.
	SourceOutcomeAdopted = "adopted"
	// SourceOutcomeWithdrawn: the source withdrew its contribution.
	SourceOutcomeWithdrawn = "withdrawn"
	// SourceOutcomeSkipped: the push wrote nothing. reason says why (a stale
	// revision, a refused cross-owner write).
	SourceOutcomeSkipped = "skipped"
)

// sourceOutcomeTransitions is the legal next state of each state. A write that
// would leave a state through an edge that is not here is a no-op, so an outcome
// can never be talked out of a decision by a later irrelevant write.
//
// The reasoning, per state:
//
//   - materialized: the record can be merged into another object, adopted by
//     another owner, withdrawn by its source, or a later stale push can be
//     skipped.
//   - skipped: nothing was written, so the source can fix it (materialized);
//     the record can also be withdrawn, merged or adopted by another decision.
//   - withdrawn: only the source revives it with a real push.
//   - adopted: the previous owner lost the row to another source, so its record
//     can be merged or withdrawn; it can also come back: a re-push while it holds
//     a grant (or as the system actor) adopts the row back, the adoption is
//     recorded on the adopter's record, and this record materializes afresh with
//     adopted_by_source_id cleared. The edge must exist because the adopt-back
//     path writes materialized (changeset.go); a table that refused it would keep
//     reporting `adopted` naming a source the record's owner no longer is.
//   - merged: terminal until the source addresses the destination. The source
//     re-pushes the record itself (materialized) or a later merge moves the
//     destination (merged). A merge names the destination in canonical_id
//     precisely so the source can stop pushing the old record.
var sourceOutcomeTransitions = map[string]map[string]bool{
	SourceOutcomeMaterialized: {
		SourceOutcomeMerged:    true,
		SourceOutcomeAdopted:   true,
		SourceOutcomeWithdrawn: true,
		SourceOutcomeSkipped:   true,
	},
	SourceOutcomeSkipped: {
		SourceOutcomeMaterialized: true,
		SourceOutcomeMerged:       true,
		SourceOutcomeAdopted:      true,
		SourceOutcomeWithdrawn:    true,
	},
	SourceOutcomeWithdrawn: {
		SourceOutcomeMaterialized: true,
	},
	SourceOutcomeAdopted: {
		SourceOutcomeMaterialized: true,
		SourceOutcomeMerged:       true,
		SourceOutcomeWithdrawn:    true,
	},
	SourceOutcomeMerged: {
		SourceOutcomeMaterialized: true,
		SourceOutcomeMerged:       true,
	},
}

// SourceOutcomeTransitionAllowed reports whether a record may move from one
// outcome state to another. Staying in the same state is always allowed: a
// repeated decision (a re-push at a higher revision, a second merge) updates the
// facts without breaking the state machine.
func SourceOutcomeTransitionAllowed(from, to string) bool {
	if from == "" {
		return true
	}
	if from == to {
		return true
	}
	return sourceOutcomeTransitions[from][to]
}

// SourceOutcome is one source record's current outcome. It is the row shape, not
// the API shape: the pull surface reports a projection of it.
type SourceOutcome struct {
	bun.BaseModel `bun:"table:kb.source_outcomes,alias:so"`

	ID                  uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID           uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	SourceID            uuid.UUID  `bun:"source_id,type:uuid,notnull" json:"source_id"`
	SubjectKind         string     `bun:"subject_kind,notnull,default:'object'" json:"subject_kind"`
	ExternalID          string     `bun:"external_id,notnull" json:"external_id"`
	State               string     `bun:"state,notnull" json:"state"`
	CanonicalID         *uuid.UUID `bun:"canonical_id,type:uuid" json:"canonical_id,omitempty"`
	ExternalRevision    string     `bun:"external_revision,notnull,default:''" json:"external_revision"`
	ExternalRevisionSeq int64      `bun:"external_revision_seq,notnull,default:0" json:"external_revision_seq"`
	Generation          int64      `bun:"generation,notnull,default:0" json:"generation"`
	AdoptedBySourceID   *uuid.UUID `bun:"adopted_by_source_id,type:uuid" json:"adopted_by_source_id,omitempty"`
	Reason              string     `bun:"reason,notnull,default:''" json:"reason,omitempty"`
	// Seq is the project-scoped monotonic sequence the pull cursor pages.
	Seq         int64     `bun:"seq,notnull" json:"seq"`
	FirstSeenAt time.Time `bun:"first_seen_at,notnull,default:now()" json:"first_seen_at"`
	UpdatedAt   time.Time `bun:"updated_at,notnull,default:now()" json:"updated_at"`
}

// NextSourceOutcomeSeq allocates the next project-scoped sequence value. The
// upsert takes the project's counter row for update, so a concurrent writer
// waits and is handed the next value rather than the same one; the counter is a
// row rather than a column on the outcome table because a gap-free per-project
// order is what a resuming cursor needs.
func (r *Repository) NextSourceOutcomeSeq(ctx context.Context, db bun.IDB, projectID uuid.UUID) (int64, error) {
	var next int64
	err := db.NewRaw(`
		INSERT INTO kb.source_outcome_sequences (project_id, last_seq)
		VALUES (?, 1)
		ON CONFLICT (project_id)
		DO UPDATE SET last_seq = kb.source_outcome_sequences.last_seq + 1
		RETURNING last_seq
	`, projectID).Scan(ctx, &next)
	if err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	return next, nil
}

// GetSourceOutcome reads the current outcome of one record, or (nil, nil) when
// the record has none — a record pushed before outcomes existed, which is
// deliberately not invented by a backfill.
func (r *Repository) GetSourceOutcome(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, externalID string) (*SourceOutcome, error) {
	var o SourceOutcome
	err := db.NewSelect().
		Model(&o).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("external_id = ?", externalID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return &o, nil
}

// WriteSourceOutcome records the current outcome of one record inside the
// caller's transaction. It is the single write path every decision site uses, so
// the state machine and the sequence allocation cannot diverge between the apply,
// withdraw, merge and adoption paths.
//
// A write that is not a legal transition is a no-op: the existing outcome is
// returned unchanged and no sequence value is consumed, so a source is not told
// that something changed when the decision did not. A write that changes nothing
// (same state, destination, revision and reason) is also a no-op for the same
// reason.
func (r *Repository) WriteSourceOutcome(ctx context.Context, db bun.IDB, o *SourceOutcome) (*SourceOutcome, error) {
	existing, err := r.GetSourceOutcome(ctx, db, o.ProjectID, o.SourceID, o.ExternalID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if !SourceOutcomeTransitionAllowed(existing.State, o.State) {
			return existing, nil
		}
		if sourceOutcomeUnchanged(existing, o) {
			return existing, nil
		}
	}

	seq, err := r.NextSourceOutcomeSeq(ctx, db, o.ProjectID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	o.Seq = seq
	o.UpdatedAt = now
	if o.FirstSeenAt.IsZero() {
		o.FirstSeenAt = now
	}
	if o.SubjectKind == "" {
		o.SubjectKind = ContributionSubjectObject
	}
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if _, err := db.NewInsert().Model(o).
		On(`CONFLICT (project_id, source_id, external_id) DO UPDATE SET
			subject_kind = EXCLUDED.subject_kind,
			state = EXCLUDED.state,
			canonical_id = EXCLUDED.canonical_id,
			external_revision = EXCLUDED.external_revision,
			external_revision_seq = EXCLUDED.external_revision_seq,
			generation = EXCLUDED.generation,
			adopted_by_source_id = EXCLUDED.adopted_by_source_id,
			reason = EXCLUDED.reason,
			seq = EXCLUDED.seq,
			updated_at = EXCLUDED.updated_at`).
		Exec(ctx); err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return o, nil
}

// sourceOutcomeUnchanged reports whether a write states the same facts as the
// stored row. first_seen_at and the sequence are deliberately not compared: they
// are the write's own bookkeeping, not the decision.
func sourceOutcomeUnchanged(existing, next *SourceOutcome) bool {
	return existing.State == next.State &&
		sameOptionalUUID(existing.CanonicalID, next.CanonicalID) &&
		existing.ExternalRevision == next.ExternalRevision &&
		existing.ExternalRevisionSeq == next.ExternalRevisionSeq &&
		existing.Generation == next.Generation &&
		sameOptionalUUID(existing.AdoptedBySourceID, next.AdoptedBySourceID) &&
		existing.Reason == next.Reason
}

func sameOptionalUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ListSourceOutcomes returns one page of a source's outcomes changed after a
// sequence cursor, in sequence order. The limit is the page size and the extra
// row read tells the caller whether another page exists.
func (r *Repository) ListSourceOutcomes(ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, afterSeq int64, limit int) ([]*SourceOutcome, bool, error) {
	var rows []*SourceOutcome
	err := db.NewSelect().
		Model(&rows).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("seq > ?", afterSeq).
		OrderExpr("seq ASC").
		Limit(limit + 1).
		Scan(ctx)
	if err != nil {
		return nil, false, apperror.NewDatabase("Database operation failed", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

// GetPresentContributionsForVersion returns the present contributions on one
// version row inside the caller's transaction. The adoption and merge paths need
// it to name the record that produced an outcome without reading uncommitted
// rows through a second connection.
func (r *Repository) GetPresentContributionsForVersion(ctx context.Context, db bun.IDB, projectID uuid.UUID, kind string, versionID uuid.UUID) ([]*ProvenanceContribution, error) {
	var rows []*ProvenanceContribution
	err := db.NewSelect().
		Model(&rows).
		Where("project_id = ?", projectID).
		Where("subject_kind = ?", kind).
		Where("subject_version_id = ?", versionID).
		Where("state = ?", ContributionStatePresent).
		OrderExpr("source_id ASC, external_id ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return rows, nil
}

// SourceOutcomeItem is the pull surface's projection of one outcome: what became
// of one record, how to follow it, and which decision produced it.
type SourceOutcomeItem struct {
	SourceID            uuid.UUID  `json:"source_id"`
	ExternalID          string     `json:"external_id"`
	SubjectKind         string     `json:"subject_kind"`
	State               string     `json:"state"`
	CanonicalID         *uuid.UUID `json:"canonical_id,omitempty"`
	ExternalRevision    string     `json:"external_revision,omitempty"`
	ExternalRevisionSeq int64      `json:"external_revision_seq"`
	Generation          int64      `json:"generation"`
	AdoptedBySourceID   *uuid.UUID `json:"adopted_by_source_id,omitempty"`
	Reason              string     `json:"reason,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// SourceOutcomesResponse is one page of a source's outcomes.
type SourceOutcomesResponse struct {
	Outcomes []SourceOutcomeItem `json:"outcomes"`
	// NextCursor is the opaque cursor of the last outcome in the page. It is
	// returned even when HasMore is false, so a source can persist it as its
	// high-water mark and resume from the same place after a restart.
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

const (
	// defaultSourceOutcomePageSize is the page size when the client does not ask
	// for one.
	defaultSourceOutcomePageSize = 100
	// maxSourceOutcomePageSize bounds one page so a single request cannot read a
	// project's whole outcome table.
	maxSourceOutcomePageSize = 500
	// sourceOutcomeCursorPrefix versions the opaque cursor so a future cursor
	// shape can be rejected rather than misread.
	sourceOutcomeCursorPrefix = "so1:"
)

// encodeSourceOutcomeCursor renders a sequence value as an opaque cursor. A
// client never constructs one; it only stores and returns it.
func encodeSourceOutcomeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sourceOutcomeCursorPrefix + strconv.FormatInt(seq, 10)))
}

// decodeSourceOutcomeCursor parses a cursor produced by encodeSourceOutcomeCursor.
// An empty cursor means "from the beginning"; anything else that is not a cursor
// this system produced is a bad request, not "from the beginning", so a typo can
// never silently replay the whole table.
func decodeSourceOutcomeCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, apperror.NewBadRequest("invalid since cursor")
	}
	value, ok := strings.CutPrefix(string(raw), sourceOutcomeCursorPrefix)
	if !ok {
		return 0, apperror.NewBadRequest("invalid since cursor")
	}
	seq, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seq < 0 {
		return 0, apperror.NewBadRequest("invalid since cursor")
	}
	return seq, nil
}

// ListSourceOutcomes serves the pull surface: the source's outcomes changed after
// a cursor, in a total order, one page at a time.
//
// The source is resolved against the caller's project first, so a source id from
// another project reads as not found rather than as an empty page.
func (s *Service) ListSourceOutcomes(ctx context.Context, projectID, sourceID uuid.UUID, cursor string, limit int) (*SourceOutcomesResponse, error) {
	ok, err := s.repo.SourceExistsInProject(ctx, s.repo.DB(), projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(404, "not_found", "source not found in this project")
	}

	afterSeq, err := decodeSourceOutcomeCursor(cursor)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultSourceOutcomePageSize
	}
	if limit > maxSourceOutcomePageSize {
		limit = maxSourceOutcomePageSize
	}

	rows, hasMore, err := s.repo.ListSourceOutcomes(ctx, s.repo.DB(), projectID, sourceID, afterSeq, limit)
	if err != nil {
		return nil, err
	}

	resp := &SourceOutcomesResponse{
		Outcomes:   make([]SourceOutcomeItem, 0, len(rows)),
		HasMore:    hasMore,
		NextCursor: cursor,
	}
	for _, row := range rows {
		resp.Outcomes = append(resp.Outcomes, SourceOutcomeItem{
			SourceID:            row.SourceID,
			ExternalID:          row.ExternalID,
			SubjectKind:         row.SubjectKind,
			State:               row.State,
			CanonicalID:         row.CanonicalID,
			ExternalRevision:    row.ExternalRevision,
			ExternalRevisionSeq: row.ExternalRevisionSeq,
			Generation:          row.Generation,
			AdoptedBySourceID:   row.AdoptedBySourceID,
			Reason:              row.Reason,
			UpdatedAt:           row.UpdatedAt,
		})
	}
	if len(rows) > 0 {
		resp.NextCursor = encodeSourceOutcomeCursor(rows[len(rows)-1].Seq)
	}
	return resp, nil
}

// recordOutcome is the decision-site helper: it writes one outcome and turns a
// write failure into the caller's error, because a decision that is not recorded
// must fail the transaction that made it rather than leave the feedback view
// behind.
func (s *Service) recordOutcome(ctx context.Context, db bun.IDB, o *SourceOutcome) error {
	if _, err := s.repo.WriteSourceOutcome(ctx, db, o); err != nil {
		return fmt.Errorf("record source outcome %s: %w", o.ExternalID, err)
	}
	return nil
}

// markMergedOutcomes records `merged` for every source record that now backs a
// merged destination version, naming the destination's canonical id. Both sides
// of a merge are told, so either source can stop pushing its old record and
// follow the destination.
func (s *Service) markMergedOutcomes(ctx context.Context, db bun.IDB, projectID uuid.UUID, kind string, destVersionID, destCanonicalID uuid.UUID) error {
	contribs, err := s.repo.GetPresentContributionsForVersion(ctx, db, projectID, kind, destVersionID)
	if err != nil {
		return err
	}
	for _, c := range contribs {
		dest := destCanonicalID
		if err := s.recordOutcome(ctx, db, &SourceOutcome{
			ProjectID:           projectID,
			SourceID:            c.SourceID,
			SubjectKind:         kind,
			ExternalID:          c.ExternalID,
			State:               SourceOutcomeMerged,
			CanonicalID:         &dest,
			ExternalRevision:    c.ExternalRevision,
			ExternalRevisionSeq: c.ExternalRevisionSeq,
		}); err != nil {
			return err
		}
	}
	return nil
}

// canonicalForVersion resolves the canonical id a version row belongs to, so a
// skipped or withdrawn outcome can still name the graph row the source's record
// maps to. It returns nil rather than failing the write when the version cannot
// be read: the canonical id is reported "when it has one", and failing an apply
// because a read-back of a display field missed would be worse than omitting it.
func (s *Service) canonicalForVersion(ctx context.Context, db bun.IDB, projectID uuid.UUID, kind string, versionID uuid.UUID) *uuid.UUID {
	switch kind {
	case ContributionSubjectObject:
		row, err := s.repo.GetObjectVersionByID(ctx, db, projectID, versionID)
		if err != nil {
			return nil
		}
		canonical := row.CanonicalID
		return &canonical
	case ContributionSubjectRelationship:
		row, err := s.repo.GetRelationshipVersionByID(ctx, db, projectID, versionID)
		if err != nil {
			return nil
		}
		canonical := row.CanonicalID
		return &canonical
	default:
		return nil
	}
}

// recordAdoptedOutcome marks the previous owner's record `adopted` after another
// source adopted the row it owns. It names the adopter and the destination row,
// and it deliberately does not touch the contribution: the record of who supplied
// the content is evidence, not something the adoption overwrites (task 1.4).
func (s *Service) recordAdoptedOutcome(ctx context.Context, db bun.IDB, projectID, previousOwnerID, adopterID, versionID, canonicalID uuid.UUID, generation int64) error {
	contribs, err := s.repo.GetPresentContributionsForVersion(ctx, db, projectID, ContributionSubjectObject, versionID)
	if err != nil {
		return err
	}
	for _, c := range contribs {
		if c.SourceID != previousOwnerID {
			continue
		}
		dest := canonicalID
		adopter := adopterID
		if err := s.recordOutcome(ctx, db, &SourceOutcome{
			ProjectID:           projectID,
			SourceID:            c.SourceID,
			SubjectKind:         ContributionSubjectObject,
			ExternalID:          c.ExternalID,
			State:               SourceOutcomeAdopted,
			CanonicalID:         &dest,
			ExternalRevision:    c.ExternalRevision,
			ExternalRevisionSeq: c.ExternalRevisionSeq,
			Generation:          generation,
			AdoptedBySourceID:   &adopter,
		}); err != nil {
			return err
		}
	}
	return nil
}
