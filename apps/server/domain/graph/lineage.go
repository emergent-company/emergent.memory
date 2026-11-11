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

// Input kinds recorded in kb.synthesis_lineage. The set is closed: the
// migration carries a CHECK constraint with the same values, so a caller cannot
// invent a free-text provenance vocabulary that no reader knows how to resolve.
const (
	LineageInputDocument      = "document"
	LineageInputExtractionJob = "extraction_job"
	LineageInputVersion       = "version"
)

// SynthesisLineage records one input a synthesized version was derived from.
//
// It is keyed to the version row, not to (canonical_id, version): a branch fork
// re-uses the canonical id at version = 1, so the pair is not unique across
// branches. Rows are immutable; a new version inherits its predecessor's inputs
// and names the predecessor itself with input_kind = version.
type SynthesisLineage struct {
	bun.BaseModel `bun:"table:kb.synthesis_lineage,alias:sl"`

	ID               uuid.UUID `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID        uuid.UUID `bun:"project_id,type:uuid,notnull" json:"project_id"`
	SubjectKind      string    `bun:"subject_kind,notnull" json:"subject_kind"`
	SubjectVersionID uuid.UUID `bun:"subject_version_id,type:uuid,notnull" json:"subject_version_id"`
	InputKind        string    `bun:"input_kind,notnull" json:"input_kind"`
	InputID          uuid.UUID `bun:"input_id,type:uuid,notnull" json:"input_id"`
	DerivedAt        time.Time `bun:"derived_at,notnull,default:now()" json:"derived_at"`
}

// SynthesisInput is one input of a version that is about to be written.
type SynthesisInput struct {
	Kind string
	ID   uuid.UUID
}

// synthesisInputsKey carries the inputs of the synthesis operation currently
// running on a context.
//
// It mirrors the write stamp: the synthesis path (extraction worker, ingestion)
// states its inputs once on the context, and every version insert reached from
// that context records them beside the version row. A path that mints a version
// cannot forget a separate lineage write, because there is no separate write to
// forget.
type synthesisInputsKey struct{}

// WithSynthesisInputs declares the inputs of the synthesis operation represented
// by ctx. Unknown kinds and zero ids are ignored at write time; dedupe happens
// there too.
func WithSynthesisInputs(ctx context.Context, inputs ...SynthesisInput) context.Context {
	if len(inputs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, synthesisInputsKey{}, append([]SynthesisInput(nil), inputs...))
}

// synthesisInputsFromContext returns the inputs declared on ctx, if any.
func synthesisInputsFromContext(ctx context.Context) []SynthesisInput {
	inputs, _ := ctx.Value(synthesisInputsKey{}).([]SynthesisInput)
	return inputs
}

// validSynthesisInputKind reports whether kind is one the schema and readers
// understand.
func validSynthesisInputKind(kind string) bool {
	switch kind {
	case LineageInputDocument, LineageInputExtractionJob, LineageInputVersion:
		return true
	default:
		return false
	}
}

// lineageAppliesTo reports whether a version with this data class records input
// lineage. Source-owned rows are described by contributions, not by synthesis
// lineage; adopted rows are synthesized content a source has taken into its own
// write path, so they do carry it.
func lineageAppliesTo(dataClass string) bool {
	return dataClass != DataClassSource
}

// normalizeSynthesisInputs drops unknown kinds, zero ids and duplicates, so a
// caller can pass the same input twice without violating the unique index.
func normalizeSynthesisInputs(inputs []SynthesisInput) []SynthesisInput {
	if len(inputs) == 0 {
		return nil
	}
	seen := make(map[SynthesisInput]struct{}, len(inputs))
	out := make([]SynthesisInput, 0, len(inputs))
	for _, in := range inputs {
		if in.ID == uuid.Nil || !validSynthesisInputKind(in.Kind) {
			continue
		}
		if _, dup := seen[in]; dup {
			continue
		}
		seen[in] = struct{}{}
		out = append(out, in)
	}
	return out
}

// insertSynthesisLineage writes the inputs of one version row, in the caller's
// database handle (the same transaction as the version write when the caller
// has one). A no-op when there is nothing to record.
func (r *Repository) insertSynthesisLineage(
	ctx context.Context,
	db bun.IDB,
	projectID uuid.UUID,
	subjectKind string,
	subjectVersionID uuid.UUID,
	inputs []SynthesisInput,
) error {
	inputs = normalizeSynthesisInputs(inputs)
	if len(inputs) == 0 {
		return nil
	}

	rows := make([]SynthesisLineage, 0, len(inputs))
	now := time.Now()
	for _, in := range inputs {
		rows = append(rows, SynthesisLineage{
			ID:               uuid.New(),
			ProjectID:        projectID,
			SubjectKind:      subjectKind,
			SubjectVersionID: subjectVersionID,
			InputKind:        in.Kind,
			InputID:          in.ID,
			DerivedAt:        now,
		})
	}
	if _, err := db.NewInsert().Model(&rows).
		On("CONFLICT (subject_kind, subject_version_id, input_kind, input_id) DO NOTHING").
		Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// carryForwardSynthesisLineage copies the content inputs (everything but the
// version link) of a predecessor version onto a new version.
//
// The version link is deliberately not copied: the new version records its own
// direct predecessor with input_kind = version, so a chain of edits stays a
// chain instead of accumulating every version ever superseded.
func (r *Repository) carryForwardSynthesisLineage(
	ctx context.Context,
	db bun.IDB,
	subjectKind string,
	fromVersionID, toVersionID uuid.UUID,
) (int, error) {
	res, err := db.NewRaw(`
		INSERT INTO kb.synthesis_lineage
			(project_id, subject_kind, subject_version_id, input_kind, input_id, derived_at)
		SELECT sl.project_id, sl.subject_kind, ?::uuid, sl.input_kind, sl.input_id, now()
		FROM kb.synthesis_lineage sl
		WHERE sl.subject_kind = ? AND sl.subject_version_id = ? AND sl.input_kind <> ?
		ON CONFLICT (subject_kind, subject_version_id, input_kind, input_id) DO NOTHING`,
		toVersionID, subjectKind, fromVersionID, LineageInputVersion,
	).Exec(ctx)
	if err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// copySynthesisLineageToVersion copies every recorded input of one or more
// source versions onto a destination version. It is the union-on-merge and the
// fork-copy rule: the destination reports the same inputs, including the version
// links that describe where the content came from.
func (r *Repository) copySynthesisLineageToVersion(
	ctx context.Context,
	db bun.IDB,
	subjectKind string,
	fromVersionIDs []uuid.UUID,
	toVersionID uuid.UUID,
) (int, error) {
	if len(fromVersionIDs) == 0 {
		return 0, nil
	}
	// Lineage is synthesis provenance, never ownership. A merge can land on a
	// source-owned destination (the changeset apply path mints source rows), and
	// copying synthesis inputs onto it would let a later re-extraction treat the
	// source's content as its own output. Relationships have no data class, so
	// this gate only applies to objects.
	if subjectKind == ContributionSubjectObject {
		var dataClass string
		err := db.NewSelect().
			TableExpr("kb.graph_objects").
			Column("data_class").
			Where("id = ?", toVersionID).
			Scan(ctx, &dataClass)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, apperror.NewDatabase("Database operation failed", err)
		}
		if dataClass == DataClassSource {
			return 0, nil
		}
	}
	res, err := db.NewRaw(`
		INSERT INTO kb.synthesis_lineage
			(project_id, subject_kind, subject_version_id, input_kind, input_id, derived_at)
		SELECT sl.project_id, sl.subject_kind, ?::uuid, sl.input_kind, sl.input_id, now()
		FROM kb.synthesis_lineage sl
		WHERE sl.subject_kind = ? AND sl.subject_version_id IN (?)
		ON CONFLICT (subject_kind, subject_version_id, input_kind, input_id) DO NOTHING`,
		toVersionID, subjectKind, bun.In(fromVersionIDs),
	).Exec(ctx)
	if err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// bulkCopySynthesisLineage carries the recorded inputs of freshly copied HEAD
// rows onto their copies, one statement per batch of a fork. Each copy keeps the
// same inputs; the subject version id is the copy's own row.
func (r *Repository) bulkCopySynthesisLineage(
	ctx context.Context,
	subjectKind string,
	pairs map[uuid.UUID]uuid.UUID,
) error {
	if len(pairs) == 0 {
		return nil
	}

	pairSQL := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*2+1)
	for from, to := range pairs {
		pairSQL = append(pairSQL, "(?::uuid, ?::uuid)")
		args = append(args, from, to)
	}
	args = append(args, subjectKind)

	query := `
		INSERT INTO kb.synthesis_lineage
			(project_id, subject_kind, subject_version_id, input_kind, input_id, derived_at)
		SELECT sl.project_id, sl.subject_kind, m.to_id::uuid, sl.input_kind, sl.input_id, now()
		FROM kb.synthesis_lineage sl
		JOIN (VALUES ` + strings.Join(pairSQL, ", ") + `) AS m(from_id, to_id)
		  ON m.from_id::uuid = sl.subject_version_id
		WHERE sl.subject_kind = ?
		ON CONFLICT (subject_kind, subject_version_id, input_kind, input_id) DO NOTHING`

	if _, err := r.db.NewRaw(query, args...).Exec(ctx); err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// GetSynthesisLineage returns the recorded inputs of one or more version rows,
// grouped by version id. One query regardless of how many rows are asked for,
// so an object history does not become N+1.
func (r *Repository) GetSynthesisLineage(
	ctx context.Context,
	projectID uuid.UUID,
	subjectKind string,
	versionIDs []uuid.UUID,
) (map[uuid.UUID][]*SynthesisLineage, error) {
	out := make(map[uuid.UUID][]*SynthesisLineage, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}

	var rows []SynthesisLineage
	err := r.db.NewSelect().
		Model(&rows).
		Where("project_id = ?", projectID).
		Where("subject_kind = ?", subjectKind).
		Where("subject_version_id IN (?)", bun.In(versionIDs)).
		OrderExpr("derived_at ASC, input_kind ASC, input_id ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	for i := range rows {
		l := rows[i]
		out[l.SubjectVersionID] = append(out[l.SubjectVersionID], &l)
	}
	return out, nil
}

// CanonicalIDsDerivedFromInput returns the canonical object ids whose HEAD
// version records the given input on a branch. It is the reverse direction of
// lineage: "what did this document/job previously produce?".
func (r *Repository) CanonicalIDsDerivedFromInput(
	ctx context.Context,
	projectID uuid.UUID,
	inputKind string,
	inputID uuid.UUID,
	branchID *uuid.UUID,
) (map[uuid.UUID]struct{}, error) {
	q := r.db.NewSelect().
		TableExpr("kb.synthesis_lineage AS sl").
		ColumnExpr("DISTINCT go.canonical_id").
		Join("JOIN kb.graph_objects AS go ON go.id = sl.subject_version_id").
		Where("sl.project_id = ?", projectID).
		Where("sl.subject_kind = ?", ContributionSubjectObject).
		Where("sl.input_kind = ?", inputKind).
		Where("sl.input_id = ?", inputID).
		Where("go.supersedes_id IS NULL").
		Where("go.deleted_at IS NULL").
		Where("go.data_class = ?", DataClassSynthesized).
		Where("go.owner_source_id IS NULL")
	if branchID == nil {
		q = q.Where("go.branch_id IS NULL")
	} else {
		q = q.Where("go.branch_id = ?", *branchID)
	}

	var ids []uuid.UUID
	if err := q.Scan(ctx, &ids); err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	out := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

// SupersedeStaleDerivedVersions tombstones the HEAD object versions on a branch
// that were derived from the given input and are not in keepCanonicalIDs.
//
// This is the supersede-by-lineage rule: a re-extraction of a document finds
// what it previously produced through the recorded lineage — not through keys or
// names — and supersedes exactly those rows. Rows derived from another document
// do not name the input and are never candidates.
//
// The tombstone is an ordinary new version (deleted_at set), so the superseded
// row remains in history and the tombstone inherits the lineage it derives from.
func (r *Repository) SupersedeStaleDerivedVersions(
	ctx context.Context,
	projectID uuid.UUID,
	inputKind string,
	inputID uuid.UUID,
	branchID *uuid.UUID,
	keepCanonicalIDs map[uuid.UUID]struct{},
	types []string,
	reason string,
) (int, error) {
	if !validSynthesisInputKind(inputKind) {
		return 0, apperror.ErrBadRequest.WithMessage("unknown lineage input kind")
	}

	tx, err := r.BeginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := tx.NewSelect().
		TableExpr("kb.synthesis_lineage AS sl").
		ColumnExpr("DISTINCT go.id, go.canonical_id").
		Join("JOIN kb.graph_objects AS go ON go.id = sl.subject_version_id").
		Where("sl.project_id = ?", projectID).
		Where("sl.subject_kind = ?", ContributionSubjectObject).
		Where("sl.input_kind = ?", inputKind).
		Where("sl.input_id = ?", inputID).
		Where("go.supersedes_id IS NULL").
		Where("go.deleted_at IS NULL").
		// Supersede-by-lineage replaces synthesized output only. An adopted row
		// or one a source owns is not the extraction's to delete; its provenance
		// lives in contributions, not in this table.
		Where("go.data_class = ?", DataClassSynthesized).
		Where("go.owner_source_id IS NULL")
	if branchID == nil {
		q = q.Where("go.branch_id IS NULL")
	} else {
		q = q.Where("go.branch_id = ?", *branchID)
	}
	if len(types) > 0 {
		q = q.Where("go.type IN (?)", bun.In(types))
	}
	if len(keepCanonicalIDs) > 0 {
		keep := make([]uuid.UUID, 0, len(keepCanonicalIDs))
		for id := range keepCanonicalIDs {
			keep = append(keep, id)
		}
		q = q.Where("go.canonical_id NOT IN (?)", bun.In(keep))
	}

	var candidates []struct {
		ID          uuid.UUID `bun:"id"`
		CanonicalID uuid.UUID `bun:"canonical_id"`
	}
	if err := q.Scan(ctx, &candidates); err != nil {
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	superseded := 0
	for _, candidate := range candidates {
		// Re-check under a row lock inside the transaction: a concurrent edit may
		// have superseded the candidate between the read above and this write.
		var stillHead uuid.UUID
		err := tx.NewSelect().
			TableExpr("kb.graph_objects").
			Column("id").
			Where("id = ?", candidate.ID).
			Where("supersedes_id IS NULL").
			Where("deleted_at IS NULL").
			For("UPDATE").
			Scan(ctx, &stillHead)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, apperror.NewDatabase("Database operation failed", err)
		}

		obj, err := r.GetObjectVersionByID(ctx, tx.Tx, projectID, candidate.ID)
		if err != nil {
			return 0, err
		}
		reasonCopy := reason
		if err := r.SoftDelete(ctx, tx.Tx, obj, ActorSystem, nil, &reasonCopy); err != nil {
			return 0, err
		}
		superseded++
	}
	if err := tx.Commit(); err != nil {
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return superseded, nil
}

// fillObjectSynthesisLineage attaches the derived-from inputs of each response's
// version row. One query serves the whole slice, so a history or list of N
// versions costs one lineage read rather than N.
//
// A version with no recorded input reports Known = false: that is the
// diagnosable "unknown inputs" state of a row synthesized before lineage
// existed, not an error. Source-owned rows are described by contributions and
// are left without a derived_from block.
func (s *Service) fillObjectSynthesisLineage(ctx context.Context, projectID uuid.UUID, items []*GraphObjectResponse) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item == nil || !lineageAppliesTo(item.DataClass) {
			continue
		}
		if _, dup := seen[item.ID]; dup {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return nil
	}

	byVersion, err := s.repo.GetSynthesisLineage(ctx, projectID, ContributionSubjectObject, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == nil || !lineageAppliesTo(item.DataClass) {
			continue
		}
		rows := byVersion[item.ID]
		inputs := make([]*DerivedFromInput, 0, len(rows))
		for _, row := range rows {
			inputs = append(inputs, &DerivedFromInput{
				Kind:      row.InputKind,
				ID:        row.InputID,
				DerivedAt: row.DerivedAt,
			})
		}
		item.DerivedFrom = &DerivedFromResponse{Known: len(inputs) > 0, Inputs: inputs}
	}
	return nil
}

// CanonicalIDsDerivedFromInput is the service-level reverse read used by the
// extraction worker to learn what a job produced before superseding stale rows.
func (s *Service) CanonicalIDsDerivedFromInput(
	ctx context.Context,
	projectID uuid.UUID,
	inputKind string,
	inputID uuid.UUID,
	branchID *uuid.UUID,
) (map[uuid.UUID]struct{}, error) {
	return s.repo.CanonicalIDsDerivedFromInput(ctx, projectID, inputKind, inputID, branchID)
}

// SupersedeStaleDerivedVersions is the service-level entry point for
// supersede-by-lineage.
func (s *Service) SupersedeStaleDerivedVersions(
	ctx context.Context,
	projectID uuid.UUID,
	inputKind string,
	inputID uuid.UUID,
	branchID *uuid.UUID,
	keepCanonicalIDs map[uuid.UUID]struct{},
	types []string,
	reason string,
) (int, error) {
	return s.repo.SupersedeStaleDerivedVersions(ctx, projectID, inputKind, inputID, branchID, keepCanonicalIDs, types, reason)
}

// GetSynthesisLineage reads the recorded inputs of specific version rows. It is
// exposed for callers that already know the version ids (the extraction worker
// and tests) and returns a map keyed by version id.
func (s *Service) GetSynthesisLineage(
	ctx context.Context,
	projectID uuid.UUID,
	subjectKind string,
	versionIDs []uuid.UUID,
) (map[uuid.UUID][]*SynthesisLineage, error) {
	return s.repo.GetSynthesisLineage(ctx, projectID, subjectKind, versionIDs)
}

// SupersedeStaleAfterBranchMerge applies supersede-by-lineage after an
// extraction branch is merged, not only after the worker's own auto-merge.
//
// The branch is the record of what the re-extraction produced: its document
// inputs say which documents to reconcile, and its extraction-job inputs say
// which rows on the target the job re-produced (the keep set). Only a merge with
// nothing left for review reaches here — while a replacement is still awaiting
// an operator's decision, removing the old content would delete data whose
// successor was not accepted.
func (s *Service) SupersedeStaleAfterBranchMerge(
	ctx context.Context,
	projectID uuid.UUID,
	targetBranchID, sourceBranchID *uuid.UUID,
) (int, error) {
	documents, err := s.distinctLineageInputsOnBranch(ctx, projectID, sourceBranchID, LineageInputDocument)
	if err != nil || len(documents) == 0 {
		return 0, err
	}
	jobs, err := s.distinctLineageInputsOnBranch(ctx, projectID, sourceBranchID, LineageInputExtractionJob)
	if err != nil {
		return 0, err
	}

	keep := make(map[uuid.UUID]struct{})
	for _, jobID := range jobs {
		ids, err := s.repo.CanonicalIDsDerivedFromInput(ctx, projectID, LineageInputExtractionJob, jobID, targetBranchID)
		if err != nil {
			return 0, err
		}
		for id := range ids {
			keep[id] = struct{}{}
		}
	}

	total := 0
	for _, documentID := range documents {
		n, err := s.repo.SupersedeStaleDerivedVersions(ctx, projectID,
			LineageInputDocument, documentID, targetBranchID, keep, nil,
			"superseded by re-extraction of document "+documentID.String())
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// distinctLineageInputsOnBranch returns the distinct ids of one input kind
// recorded on the HEAD object versions of a branch.
func (s *Service) distinctLineageInputsOnBranch(
	ctx context.Context,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	inputKind string,
) ([]uuid.UUID, error) {
	q := s.repo.DB().NewSelect().
		TableExpr("kb.synthesis_lineage AS sl").
		ColumnExpr("DISTINCT sl.input_id").
		Join("JOIN kb.graph_objects AS go ON go.id = sl.subject_version_id").
		Where("sl.project_id = ?", projectID).
		Where("sl.subject_kind = ?", ContributionSubjectObject).
		Where("sl.input_kind = ?", inputKind).
		Where("go.supersedes_id IS NULL").
		Where("go.deleted_at IS NULL")
	if branchID == nil {
		q = q.Where("go.branch_id IS NULL")
	} else {
		q = q.Where("go.branch_id = ?", *branchID)
	}

	var ids []uuid.UUID
	if err := q.Scan(ctx, &ids); err != nil {
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return ids, nil
}
