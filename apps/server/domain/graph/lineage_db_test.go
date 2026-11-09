package graph_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// createSynthesized writes an object the way a synthesis path does: the inputs
// are declared on the context once and the version write records them.
func createSynthesized(
	t *testing.T,
	ctx context.Context,
	svc *graph.Service,
	projectID uuid.UUID,
	key string,
	inputs ...graph.SynthesisInput,
) *graph.GraphObjectResponse {
	t.Helper()
	writeCtx := graph.WithSynthesisInputs(ctx, inputs...)
	resp, err := svc.Create(writeCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "LineageObj",
		Key:        &key,
		Properties: map[string]any{"name": key},
	}, nil)
	require.NoError(t, err)
	return resp
}

// readLineageRows reads the recorded input rows of one version, ordered.
func readLineageRows(t *testing.T, ctx context.Context, db bun.IDB, versionID uuid.UUID) []graph.SynthesisLineage {
	t.Helper()
	var rows []graph.SynthesisLineage
	require.NoError(t, db.NewSelect().
		Model(&rows).
		Where("subject_version_id = ?", versionID).
		OrderExpr("input_kind ASC, input_id ASC").
		Scan(ctx))
	return rows
}

func lineageKinds(rows []graph.SynthesisLineage) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.InputKind)
	}
	return out
}

// countingLineageSelectHook counts SELECTs against the lineage table. A detail
// or history response must cost one lineage query, however many rows it
// describes.
type countingLineageSelectHook struct {
	count int
}

func (h *countingLineageSelectHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	if event.Operation() == "SELECT" && strings.Contains(event.Query, "synthesis_lineage") {
		h.count++
	}
	return ctx
}

func (h *countingLineageSelectHook) AfterQuery(context.Context, *bun.QueryEvent) {}

// TestExtractionInputsAreWrittenBesideVersion is acceptance 1: an extraction
// path declares the document and job on the context, and every object (and
// relationship) it creates records both inputs beside its version row.
func TestExtractionInputsAreWrittenBesideVersion(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()
	jobID := uuid.New()

	obj := createSynthesized(t, ctx, svc, projectID, "extracted-1",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: jobID},
	)

	rows := readLineageRows(t, ctx, db, obj.ID)
	require.Len(t, rows, 2, "one row per input: document + extraction job")
	assert.Equal(t, []string{graph.LineageInputDocument, graph.LineageInputExtractionJob}, lineageKinds(rows))
	assert.Equal(t, documentID, rows[0].InputID)
	assert.Equal(t, jobID, rows[1].InputID)
	for _, row := range rows {
		assert.Equal(t, obj.ID, row.SubjectVersionID, "keyed to the version row")
		assert.Equal(t, graph.ContributionSubjectObject, row.SubjectKind)
		assert.Equal(t, projectID, row.ProjectID)
	}

	// Relationships are synthesized by the same job and carry the same inputs.
	other := createSynthesized(t, ctx, svc, projectID, "extracted-2",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: jobID},
	)
	relCtx := graph.WithSynthesisInputs(ctx,
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: jobID},
	)
	rel, err := svc.CreateRelationship(relCtx, projectID, &graph.CreateGraphRelationshipRequest{
		Type:  "LineageRel",
		SrcID: obj.ID,
		DstID: other.ID,
	}, nil)
	require.NoError(t, err)

	relRows := readLineageRows(t, ctx, db, rel.ID)
	require.Len(t, relRows, 2)
	assert.Equal(t, graph.ContributionSubjectRelationship, relRows[0].SubjectKind)
}

// TestVersionDerivedFromVersionRecordsVersionRowID is acceptance 2: a version
// derived from an earlier version names that version row's id, never the
// canonical id shared across the branch.
func TestVersionDerivedFromVersionRecordsVersionRowID(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()
	key := "lineage-chain"

	v1 := createSynthesized(t, ctx, svc, projectID, key,
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID})
	v2, created, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "LineageObj",
		Key:        &key,
		Properties: map[string]any{"name": key, "v": 2},
	}, nil)
	require.NoError(t, err)
	require.False(t, created)
	require.NotEqual(t, v1.ID, v2.ID)
	require.Equal(t, v1.CanonicalID, v2.CanonicalID)

	v3, created, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "LineageObj",
		Key:        &key,
		Properties: map[string]any{"name": key, "v": 3},
	}, nil)
	require.NoError(t, err)
	require.False(t, created)

	rows := readLineageRows(t, ctx, db, v3.ID)
	require.Len(t, rows, 2, "inherited document + direct predecessor version")

	var versionInput *graph.SynthesisLineage
	for i := range rows {
		if rows[i].InputKind == graph.LineageInputVersion {
			versionInput = &rows[i]
		}
	}
	require.NotNil(t, versionInput, "a derived version records its predecessor")
	assert.Equal(t, v2.ID, versionInput.InputID, "physical version row id")
	assert.NotEqual(t, v3.CanonicalID, versionInput.InputID, "not the canonical id")

	// The document input is inherited from the version it derives from.
	var hasDocument bool
	for _, row := range rows {
		if row.InputKind == graph.LineageInputDocument {
			hasDocument = true
			assert.Equal(t, documentID, row.InputID)
		}
	}
	assert.True(t, hasDocument, "an ordinary edit inherits the content lineage")
}

// TestHistorySynthesisLineageCostsOneQuery is acceptance 5: reading N versions'
// derived-from inputs costs one query, so a per-row fetch fails this test.
func TestHistorySynthesisLineageCostsOneQuery(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()
	key := "lineage-query-count"

	first := createSynthesized(t, ctx, svc, projectID, key,
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID})
	for i := 0; i < 2; i++ {
		_, _, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "LineageObj",
			Key:        &key,
			Properties: map[string]any{"name": key, "rev": i},
		}, nil)
		require.NoError(t, err)
	}

	bdb, ok := db.(*bun.DB)
	require.True(t, ok, "test DB must be a *bun.DB when no transaction is active")
	hook := &countingLineageSelectHook{}
	bdb.AddQueryHook(hook)

	history, err := svc.GetHistory(ctx, projectID, first.CanonicalID)
	require.NoError(t, err)
	require.Len(t, history.Versions, 3)
	assert.Equal(t, 1, hook.count, "one lineage query for the whole history, not one per version")

	// Detail is one query too, and the response answers "derived from what".
	hook.count = 0
	detail, err := svc.GetByID(ctx, projectID, first.CanonicalID, true)
	require.NoError(t, err)
	assert.Equal(t, 1, hook.count)
	require.NotNil(t, detail.DerivedFrom)
	assert.True(t, detail.DerivedFrom.Known)
	require.Len(t, detail.DerivedFrom.Inputs, 2) // HEAD inherits the document and names its predecessor
}

// TestPreChangeRowReadsAsUnknownInputs is acceptance 6: a synthesized row with
// no recorded lineage reads as unknown inputs without failing.
func TestPreChangeRowReadsAsUnknownInputs(t *testing.T) {
	ctx, _, _, svc, projectID := setupProvenanceTest(t)

	key := "pre-lineage-row"
	pre := createSynthesized(t, ctx, svc, projectID, key) // no inputs: a pre-change row

	detail, err := svc.GetByID(ctx, projectID, pre.CanonicalID, true)
	require.NoError(t, err)
	require.NotNil(t, detail.DerivedFrom)
	assert.False(t, detail.DerivedFrom.Known, "no recorded inputs is the unknown state")
	assert.Empty(t, detail.DerivedFrom.Inputs)

	history, err := svc.GetHistory(ctx, projectID, pre.CanonicalID)
	require.NoError(t, err)
	require.Len(t, history.Versions, 1)
	require.NotNil(t, history.Versions[0].DerivedFrom)
	assert.False(t, history.Versions[0].DerivedFrom.Known)
}

// TestForkCopiesLineageOntoCopyVersionRows is acceptance 4 (fork half): the
// copy's lineage names the copy's version id and the same inputs.
func TestForkCopiesLineageOntoCopyVersionRows(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()
	jobID := uuid.New()

	obj := createSynthesized(t, ctx, svc, projectID, "forked-lineage",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: jobID},
	)

	targetBranchID := uuid.New()
	copied, n, err := repo.BulkCopyObjectsToBranch(ctx, projectID, nil, &targetBranchID, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, copied[obj.CanonicalID], "the object's canonical id is copied")

	var copyID uuid.UUID
	require.NoError(t, db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id").
		Where("branch_id = ?", targetBranchID).
		Where("canonical_id = ?", obj.CanonicalID).
		Where("supersedes_id IS NULL").
		Scan(ctx, &copyID))
	require.NotEqual(t, obj.ID, copyID)

	rows := readLineageRows(t, ctx, db, copyID)
	require.Len(t, rows, 2, "the copy keeps the same inputs")
	assert.Equal(t, []string{graph.LineageInputDocument, graph.LineageInputExtractionJob}, lineageKinds(rows))
	for _, row := range rows {
		assert.Equal(t, copyID, row.SubjectVersionID, "the copy's own version row is the subject")
	}
}

// TestReextractionSupersedesOnlyItsOwnDocument is acceptance 3: supersede by
// lineage touches exactly the rows derived from the re-extracted document and
// leaves rows derived from another document alone.
func TestReextractionSupersedesOnlyItsOwnDocument(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentA := uuid.New()
	documentB := uuid.New()
	jobA := uuid.New()

	stale := createSynthesized(t, ctx, svc, projectID, "stale-from-a",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentA},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: jobA})
	otherDoc := createSynthesized(t, ctx, svc, projectID, "from-b",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentB})
	// A row this re-extraction did re-produce: same document input, but it must
	// survive because the new job produced it.
	reproduced := createSynthesized(t, ctx, svc, projectID, "reproduced-from-a",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentA},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: uuid.New()})

	keep := map[uuid.UUID]struct{}{reproduced.CanonicalID: {}}
	n, err := svc.SupersedeStaleDerivedVersions(ctx, projectID,
		graph.LineageInputDocument, documentA, nil, keep, nil, "superseded in test")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the row the job did not re-produce")

	headDeleted := func(canonicalID uuid.UUID) *time.Time {
		var deletedAt *time.Time
		require.NoError(t, db.NewSelect().
			TableExpr("kb.graph_objects").
			Column("deleted_at").
			Where("canonical_id = ?", canonicalID).
			Where("supersedes_id IS NULL").
			Scan(ctx, &deletedAt))
		return deletedAt
	}
	assert.NotNil(t, headDeleted(stale.CanonicalID), "rows derived from the document are superseded")
	assert.Nil(t, headDeleted(otherDoc.CanonicalID), "rows derived from another document are untouched")
	assert.Nil(t, headDeleted(reproduced.CanonicalID), "rows this job re-produced are kept")

	// The tombstone is a new version of the stale row and inherits its lineage.
	var tombstoneID uuid.UUID
	require.NoError(t, db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id").
		Where("canonical_id = ?", stale.CanonicalID).
		Where("supersedes_id IS NULL").
		Scan(ctx, &tombstoneID))
	tombstoneRows := readLineageRows(t, ctx, db, tombstoneID)
	var hasDoc bool
	for _, row := range tombstoneRows {
		if row.InputKind == graph.LineageInputDocument && row.InputID == documentA {
			hasDoc = true
		}
	}
	assert.True(t, hasDoc, "the tombstone inherits the lineage of the version it derives from")
}

// TestLineageIsNotOwnership is acceptance "lineage is not ownership": recording
// inputs never sets owner_source_id and the row stays synthesized.
func TestLineageIsNotOwnership(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()

	obj := createSynthesized(t, ctx, svc, projectID, "not-owned",
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID})

	var row struct {
		DataClass     string     `bun:"data_class"`
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
	}
	require.NoError(t, db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("data_class", "owner_source_id").
		Where("id = ?", obj.ID).
		Scan(ctx, &row))
	assert.Equal(t, graph.DataClassSynthesized, row.DataClass)
	assert.Nil(t, row.OwnerSourceID, "lineage never makes a source the owner")

	rows := readLineageRows(t, ctx, db, obj.ID)
	require.Len(t, rows, 1)
	assert.Equal(t, projectID, rows[0].ProjectID)
}

// TestReproducedUnchangedRowRecordsJobWithoutNewVersion covers the upsert no-op
// path: a re-extraction that produces identical content mints no version, but
// it still derived the row and must be visible to the supersede rule instead of
// being mistaken for stale output.
func TestReproducedUnchangedRowRecordsJobWithoutNewVersion(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	documentID := uuid.New()
	firstJob := uuid.New()
	secondJob := uuid.New()
	key := "reproduced-unchanged"

	v1 := createSynthesized(t, ctx, svc, projectID, key,
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: firstJob})

	reproduceCtx := graph.WithSynthesisInputs(ctx,
		graph.SynthesisInput{Kind: graph.LineageInputDocument, ID: documentID},
		graph.SynthesisInput{Kind: graph.LineageInputExtractionJob, ID: secondJob})
	out, created, err := svc.CreateOrUpdate(reproduceCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "LineageObj",
		Key:        &key,
		Properties: map[string]any{"name": key},
	}, nil)
	require.NoError(t, err)
	require.False(t, created, "identical content takes the no-op path")
	require.Equal(t, v1.ID, out.ID, "no new version row is minted")

	rows := readLineageRows(t, ctx, db, v1.ID)
	kinds := lineageKinds(rows)
	assert.Contains(t, kinds, graph.LineageInputDocument)
	assert.Len(t, rows, 3, "document + both jobs are recorded on the row that survived")
}
