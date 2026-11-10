package graph_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// source_outcome_db_test.go covers the adoption-feedback pull surface: the
// outcome record written where apply/withdraw/merge/adopt take the decision, and
// the cursor that lets a source resume without skipping or duplicating a record
// (issue #154, tasks 4.1-4.6).
//
// The fixtures write the kb.sources rows the changeset names, exactly as the
// changeset tests do, because source_id is a request parameter rather than a
// write-stamp field.

func readOutcome(t *testing.T, ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, externalID string) *graph.SourceOutcome {
	t.Helper()
	repo := graph.NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)), &config.Config{})
	row, err := repo.GetSourceOutcome(ctx, db, projectID, sourceID, externalID)
	require.NoError(t, err)
	return row
}

func requireOutcome(t *testing.T, ctx context.Context, db bun.IDB, projectID, sourceID uuid.UUID, externalID, state string) *graph.SourceOutcome {
	t.Helper()
	row := readOutcome(t, ctx, db, projectID, sourceID, externalID)
	require.NotNil(t, row, "no outcome row for %s", externalID)
	assert.Equal(t, state, row.State)
	return row
}

// listOutcomes reads every outcome of a source by paging the pull surface.
func listOutcomes(t *testing.T, ctx context.Context, svc *graph.Service, projectID, sourceID uuid.UUID) []graph.SourceOutcomeItem {
	t.Helper()
	var all []graph.SourceOutcomeItem
	cursor := ""
	for {
		page, err := svc.ListSourceOutcomes(ctx, projectID, sourceID, cursor, 100)
		require.NoError(t, err)
		all = append(all, page.Outcomes...)
		if !page.HasMore {
			return all
		}
		cursor = page.NextCursor
	}
}

// TestSourceOutcomeRecordedOnApply is task 4.1: a created record reads
// materialized with the canonical id and the revision it arrived at, and a
// re-push updates the same row rather than adding another.
func TestSourceOutcomeRecordedOnApply(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "outcome-apply")

	first, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "apply-1",
			ExternalRevision:    strPtr("rev-1"),
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonical := *first.Results[0].CanonicalID

	row := requireOutcome(t, ctx, db, projectID, sourceID, "apply-1", graph.SourceOutcomeMaterialized)
	require.NotNil(t, row.CanonicalID)
	assert.Equal(t, canonical, *row.CanonicalID)
	assert.Equal(t, "rev-1", row.ExternalRevision)
	assert.Equal(t, int64(1), row.ExternalRevisionSeq)
	assert.Equal(t, first.Generation, row.Generation)
	assert.Empty(t, row.Reason)

	second, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada Lovelace"},
			ExternalID:          "apply-1",
			ExternalRevision:    strPtr("rev-2"),
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeUpdated, second.Results[0].Outcome)

	updated := requireOutcome(t, ctx, db, projectID, sourceID, "apply-1", graph.SourceOutcomeMaterialized)
	require.NotNil(t, updated.CanonicalID)
	assert.Equal(t, canonical, *updated.CanonicalID, "a re-push versions the same object")
	assert.Equal(t, "rev-2", updated.ExternalRevision)
	assert.Equal(t, int64(2), updated.ExternalRevisionSeq)
	assert.Equal(t, second.Generation, updated.Generation)
	assert.True(t, updated.Seq > row.Seq, "a changed outcome takes the next sequence value")

	var count int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.source_outcomes WHERE project_id = ? AND source_id = ?`,
		projectID, sourceID).Scan(ctx, &count))
	assert.Equal(t, 1, count, "one row per (project, source, external_id)")
}

// TestSourceOutcomeSkippedOnStalePush is task 3.1: a stale re-push is visible as
// skipped with a reason, and the object is unchanged.
func TestSourceOutcomeSkippedOnStalePush(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "outcome-stale")

	first, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Grace"},
			ExternalID:          "stale-1",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	canonical := *first.Results[0].CanonicalID

	stale, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Grace Hopper"},
			ExternalID:          "stale-1",
			ExternalRevision:    strPtr("rev-2"),
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeSkipped, stale.Results[0].Outcome)

	row := requireOutcome(t, ctx, db, projectID, sourceID, "stale-1", graph.SourceOutcomeSkipped)
	require.NotNil(t, row.CanonicalID)
	assert.Equal(t, canonical, *row.CanonicalID)
	assert.NotEmpty(t, row.Reason, "a skipped outcome tells the source why")
	assert.Equal(t, int64(2), row.ExternalRevisionSeq)

	// The object is unchanged: the stale push wrote nothing.
	obj, err := svc.GetByID(ctx, projectID, canonical, true)
	require.NoError(t, err)
	assert.Equal(t, "Grace", obj.Properties["name"])
}

// TestSourceOutcomeWithdrawn is task 4.2: a withdrawal is a state on the
// contribution, not a disappearance, so the outcome says withdrawn and the object
// stays readable.
func TestSourceOutcomeWithdrawn(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "outcome-withdraw")

	applied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Alan"},
			ExternalID:          "withdraw-1",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonical := *applied.Results[0].CanonicalID

	withdrawn, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID:    sourceID,
		Withdrawals: []graph.ChangesetWithdrawalItem{{ExternalID: "withdraw-1"}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeWithdrawn, withdrawn.Results[0].Outcome)

	row := requireOutcome(t, ctx, db, projectID, sourceID, "withdraw-1", graph.SourceOutcomeWithdrawn)
	require.NotNil(t, row.CanonicalID)
	assert.Equal(t, canonical, *row.CanonicalID)

	obj, err := svc.GetByID(ctx, projectID, canonical, true)
	require.NoError(t, err, "withdrawal must not delete the object")
	assert.Equal(t, "Alan", obj.Properties["name"])
}

// TestSourceOutcomeMergedNamesDestination is task 4.3: a merge tells both
// contributing sources, naming the same destination canonical id, and leaves an
// untouched record alone.
func TestSourceOutcomeMergedNamesDestination(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "outcome-merge-a")
	sourceB := createTestSource(t, ctx, db, projectID, "outcome-merge-b")

	// A record that is not part of the merge must keep its outcome.
	untouched, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Untouched"},
			ExternalID:          "merge-untouched",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	untouchedCanonical := *untouched.Results[0].CanonicalID

	applied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Base"},
			ExternalID:          "merge-a",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	canonical := *applied.Results[0].CanonicalID

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "outcome-merge-branch"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forkResp.BranchID)

	branchApplied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		BranchID: &branchID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Branch side"},
			ExternalID:          "merge-a",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	branchVersion := *branchApplied.Results[0].SubjectID

	// The second source also backs the branch-side record, so the merge has two
	// contributing sources to tell.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		BranchID:            &branchID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    branchVersion,
		SourceID:            sourceB,
		ExternalID:          "merge-b",
		ExternalRevision:    "rev-1",
		ExternalRevisionSeq: 1,
	}))

	// The main line moves on independently, so the merge cannot fast-forward.
	_, err = svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Main side"},
			ExternalID:          "merge-a",
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)

	merge, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)
	require.True(t, merge.Applied)
	require.NotZero(t, merge.ConflictCount)

	aOutcome := requireOutcome(t, ctx, db, projectID, sourceA, "merge-a", graph.SourceOutcomeMerged)
	bOutcome := requireOutcome(t, ctx, db, projectID, sourceB, "merge-b", graph.SourceOutcomeMerged)
	require.NotNil(t, aOutcome.CanonicalID)
	require.NotNil(t, bOutcome.CanonicalID)
	assert.Equal(t, canonical, *aOutcome.CanonicalID)
	assert.Equal(t, *aOutcome.CanonicalID, *bOutcome.CanonicalID,
		"both sides must name the same destination")

	still := requireOutcome(t, ctx, db, projectID, sourceA, "merge-untouched", graph.SourceOutcomeMaterialized)
	require.NotNil(t, still.CanonicalID)
	assert.Equal(t, untouchedCanonical, *still.CanonicalID)
}

// TestSourceOutcomeAdoptionKeepsContribution is task 4.4: the previous owner's
// record reads adopted and names the adopter and the row; its contribution is
// preserved, and the adopter's own record is materialized.
func TestSourceOutcomeAdoptionKeepsContribution(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "outcome-adopt-a")
	sourceB := createTestSource(t, ctx, db, projectID, "outcome-adopt-b")

	applied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "adopt-a",
			ExternalRevision:    strPtr("a-1"),
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	versionID := *applied.Results[0].SubjectID
	canonical := *applied.Results[0].CanonicalID

	// B already contributes the same object under its own external id, which is
	// the shape an adopted record has: a record of B's on a row A owns.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    versionID,
		SourceID:            sourceB,
		ExternalID:          "adopt-b",
		ExternalRevision:    "b-1",
		ExternalRevisionSeq: 1,
	}))

	// The push runs as the system principal, which holds authority over both
	// sources; the apply therefore adopts A's object for B (credential-scoping,
	// #139) instead of refusing it.
	adopted, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada (adopted)"},
			ExternalID:          "adopt-b",
			ExternalRevision:    strPtr("b-2"),
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeUpdated, adopted.Results[0].Outcome)
	adoptedCanonical := *adopted.Results[0].CanonicalID
	assert.Equal(t, canonical, adoptedCanonical)

	aOutcome := requireOutcome(t, ctx, db, projectID, sourceA, "adopt-a", graph.SourceOutcomeAdopted)
	require.NotNil(t, aOutcome.CanonicalID)
	require.NotNil(t, aOutcome.AdoptedBySourceID)
	assert.Equal(t, canonical, *aOutcome.CanonicalID)
	assert.Equal(t, sourceB, *aOutcome.AdoptedBySourceID, "the outcome names the adopting owner")

	bOutcome := requireOutcome(t, ctx, db, projectID, sourceB, "adopt-b", graph.SourceOutcomeMaterialized)
	require.NotNil(t, bOutcome.CanonicalID)
	assert.Equal(t, canonical, *bOutcome.CanonicalID)
	assert.Nil(t, bOutcome.AdoptedBySourceID)

	// A's contribution is evidence of who supplied the content and must still be
	// present, not rewritten to the adopter.
	var present int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE project_id = ? AND source_id = ? AND external_id = 'adopt-a' AND state = 'present'`,
		projectID, sourceA).Scan(ctx, &present))
	assert.Equal(t, 1, present, "adoption must not rewrite the previous owner's contribution")

	// The head is owned by the adopter with the adopted data class, which is what
	// credential-scoping's write path honours.
	var head struct {
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
		DataClass     string     `bun:"data_class"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT owner_source_id, data_class FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, canonical).Scan(ctx, &head.OwnerSourceID, &head.DataClass))
	require.NotNil(t, head.OwnerSourceID)
	assert.Equal(t, sourceB, *head.OwnerSourceID)
	assert.Equal(t, graph.DataClassAdopted, head.DataClass)
}

// TestSourceOutcomeAdoptedBackIsMaterialized is the adopt-back case raised in
// review on #550: A owns, B adopts, then A adopts the row back. The apply path
// writes `materialized` for A, so the transition table must allow
// adopted -> materialized; otherwise the write is a silent no-op, A keeps
// reporting adopted_by_source_id = B while owning the row, and A can never
// return to materialized. B's record must read adopted naming A.
func TestSourceOutcomeAdoptedBackIsMaterialized(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "outcome-readopt-a")
	sourceB := createTestSource(t, ctx, db, projectID, "outcome-readopt-b")

	// A owns the row.
	applied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada"},
			ExternalID:          "readopt-a",
			ExternalRevision:    strPtr("a-1"),
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	versionID := *applied.Results[0].SubjectID
	canonical := *applied.Results[0].CanonicalID

	// B already contributes the same object under its own external id, the
	// shape a record that gets adopted has.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    versionID,
		SourceID:            sourceB,
		ExternalID:          "readopt-b",
		ExternalRevision:    "b-1",
		ExternalRevisionSeq: 1,
	}))

	// B adopts the row (the system principal holds authority over A).
	_, err = svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceB,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada (B)"},
			ExternalID:          "readopt-b",
			ExternalRevision:    strPtr("b-2"),
			ExternalRevisionSeq: seq(2),
		}},
	}, nil)
	require.NoError(t, err)

	aAdopted := requireOutcome(t, ctx, db, projectID, sourceA, "readopt-a", graph.SourceOutcomeAdopted)
	require.NotNil(t, aAdopted.AdoptedBySourceID, "A's record must name the adopter while B owns the row")
	assert.Equal(t, sourceB, *aAdopted.AdoptedBySourceID)
	adoptedSeq := aAdopted.Seq

	// A keeps syncing and pushes its record at a higher revision, adopting the
	// row back.
	back, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Ada (A again)"},
			ExternalID:          "readopt-a",
			ExternalRevision:    strPtr("a-2"),
			ExternalRevisionSeq: seq(3),
		}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, graph.ChangesetOutcomeUpdated, back.Results[0].Outcome)

	aBack := requireOutcome(t, ctx, db, projectID, sourceA, "readopt-a", graph.SourceOutcomeMaterialized)
	assert.Nil(t, aBack.AdoptedBySourceID, "adopting back must clear the adopter A's record named")
	require.NotNil(t, aBack.CanonicalID)
	assert.Equal(t, canonical, *aBack.CanonicalID)
	assert.Greater(t, aBack.Seq, adoptedSeq, "adopted -> materialized must consume a sequence value")

	// The adopter's record now reads adopted naming A.
	bAdopted := requireOutcome(t, ctx, db, projectID, sourceB, "readopt-b", graph.SourceOutcomeAdopted)
	require.NotNil(t, bAdopted.AdoptedBySourceID)
	assert.Equal(t, sourceA, *bAdopted.AdoptedBySourceID)

	// The head is owned by A with the adopted data class, and A's record is the
	// contribution backing it.
	var head struct {
		OwnerSourceID *uuid.UUID `bun:"owner_source_id"`
		DataClass     string     `bun:"data_class"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT owner_source_id, data_class FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, canonical).Scan(ctx, &head.OwnerSourceID, &head.DataClass))
	require.NotNil(t, head.OwnerSourceID)
	assert.Equal(t, sourceA, *head.OwnerSourceID)
	assert.Equal(t, graph.DataClassAdopted, head.DataClass)

	var present int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.provenance_contributions
		WHERE project_id = ? AND source_id = ? AND external_id = 'readopt-a' AND state = 'present'`,
		projectID, sourceA).Scan(ctx, &present))
	assert.Equal(t, 1, present, "A's contribution follows the row it owns again")
}

// TestSourceOutcomeCursorPagesEveryRecordOnce is task 4.5: a limit of one pages
// through every changed outcome without repeating or skipping a record, a
// persisted cursor resumes exactly where it left off, and a record written after
// the high-water mark is still seen.
func TestSourceOutcomeCursorPagesEveryRecordOnce(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "outcome-cursor")

	const records = 5
	items := make([]graph.ChangesetObjectItem, 0, records)
	for i := 0; i < records; i++ {
		items = append(items, graph.ChangesetObjectItem{
			Type:                "Person",
			ExternalID:          "cursor-" + string(rune('a'+i)),
			Properties:          map[string]any{"name": "Person"},
			ExternalRevisionSeq: seq(1),
		})
	}
	_, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects:  items,
	}, nil)
	require.NoError(t, err)

	// Page with a limit of one, exactly as a resuming client would.
	seen := map[string]int{}
	cursor := ""
	pages := 0
	var afterSecondPage string
	for {
		page, err := svc.ListSourceOutcomes(ctx, projectID, sourceID, cursor, 1)
		require.NoError(t, err)
		require.Lenf(t, page.Outcomes, 1, "page %d must carry exactly one outcome", pages)
		pages++
		id := page.Outcomes[0].ExternalID
		seen[id]++
		require.NotEqual(t, cursor, page.NextCursor, "the cursor must advance on every page")
		cursor = page.NextCursor
		if pages == 2 {
			afterSecondPage = cursor
		}
		if !page.HasMore {
			break
		}
		require.LessOrEqual(t, pages, records, "paging must terminate after %d records", records)
	}

	require.Equal(t, records, pages)
	require.Len(t, seen, records)
	for id, count := range seen {
		assert.Equalf(t, 1, count, "record %s must be seen exactly once", id)
	}

	// A resume from a persisted cursor sees only what comes after it.
	resumed, err := svc.ListSourceOutcomes(ctx, projectID, sourceID, afterSecondPage, 100)
	require.NoError(t, err)
	require.Len(t, resumed.Outcomes, records-2)
	assert.False(t, resumed.HasMore)
	for _, item := range resumed.Outcomes {
		assert.NotContains(t, []string{"cursor-a", "cursor-b"}, item.ExternalID,
			"a resumed cursor must not replay a record it already passed")
	}

	// A record written after the high-water mark is picked up by the same cursor.
	_, err = svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			ExternalID:          "cursor-late",
			Properties:          map[string]any{"name": "Late"},
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)

	late, err := svc.ListSourceOutcomes(ctx, projectID, sourceID, resumed.NextCursor, 100)
	require.NoError(t, err)
	require.Len(t, late.Outcomes, 1)
	assert.Equal(t, "cursor-late", late.Outcomes[0].ExternalID)
	assert.False(t, late.HasMore)

	// The high-water mark from an empty read is still usable.
	empty, err := svc.ListSourceOutcomes(ctx, projectID, sourceID, late.NextCursor, 100)
	require.NoError(t, err)
	assert.Empty(t, empty.Outcomes)
	assert.False(t, empty.HasMore)
}

// TestSourceOutcomeUnknownForPreChangeRecord is task 4.6: a contribution that
// predates the outcome table has no row and is reported as no entry, not as an
// invented materialized outcome.
func TestSourceOutcomeUnknownForPreChangeRecord(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)
	sourceA := createTestSource(t, ctx, db, projectID, "outcome-legacy-a")
	sourceB := createTestSource(t, ctx, db, projectID, "outcome-legacy-b")

	applied, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceA,
		Objects: []graph.ChangesetObjectItem{{
			Type:                "Person",
			Properties:          map[string]any{"name": "Host"},
			ExternalID:          "legacy-host",
			ExternalRevisionSeq: seq(1),
		}},
	}, nil)
	require.NoError(t, err)
	versionID := *applied.Results[0].SubjectID

	// A record pushed before outcomes existed: a present contribution with no
	// outcome row.
	require.NoError(t, repo.InsertContribution(ctx, db, &graph.ProvenanceContribution{
		ProjectID:           projectID,
		SubjectKind:         graph.ContributionSubjectObject,
		SubjectVersionID:    versionID,
		SourceID:            sourceB,
		ExternalID:          "legacy-1",
		ExternalRevisionSeq: 1,
	}))

	assert.Nil(t, readOutcome(t, ctx, db, projectID, sourceB, "legacy-1"),
		"a pre-change record must not get an outcome row")
	assert.Empty(t, listOutcomes(t, ctx, svc, projectID, sourceB),
		"the pull surface must report no entry for a pre-change record")
}

// TestListSourceOutcomesRoute exercises the HTTP layer of the pull surface:
// query parsing, the opaque cursor, and the source scope (a source from another
// project is not found rather than an empty page).
func TestListSourceOutcomesRoute(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "outcome-route")

	_, err := svc.ApplyChangeset(sourcePushCtx(ctx), projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects: []graph.ChangesetObjectItem{
			{Type: "Person", ExternalID: "route-1", Properties: map[string]any{"name": "One"}, ExternalRevisionSeq: seq(1)},
			{Type: "Person", ExternalID: "route-2", Properties: map[string]any{"name": "Two"}, ExternalRevisionSeq: seq(1)},
		},
	}, nil)
	require.NoError(t, err)

	h := graph.NewHandler(svc, &config.Config{})
	get := func(t *testing.T, path, project string) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		user := &auth.AuthUser{ID: uuid.NewString(), ProjectID: project}
		req = req.WithContext(auth.InjectAuthContext(req.Context(), user))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(string(auth.UserContextKey), user)
		c.SetParamNames("id")
		c.SetParamValues(sourceID.String())
		if err := h.ListSourceOutcomes(c); err != nil {
			switch e := err.(type) {
			case *apperror.Error:
				rec.Code = e.HTTPStatus
				body, _ := json.Marshal(map[string]any{"code": e.Code, "message": e.Message})
				_, _ = rec.Body.Write(body)
			default:
				t.Fatalf("%s: unexpected handler error %v", path, err)
			}
		}
		return rec
	}

	rec := get(t, "/api/graph/sources/"+sourceID.String()+"/outcomes?limit=1", projectID.String())
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var page graph.SourceOutcomesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Outcomes, 1)
	require.True(t, page.HasMore)
	require.NotEmpty(t, page.NextCursor)

	// Resume through the wire.
	rec = get(t, "/api/graph/sources/"+sourceID.String()+"/outcomes?limit=1&since="+page.NextCursor, projectID.String())
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var second graph.SourceOutcomesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	require.Len(t, second.Outcomes, 1)
	assert.NotEqual(t, page.Outcomes[0].ExternalID, second.Outcomes[0].ExternalID)

	// A cursor this system did not produce is a bad request, not "from the
	// beginning".
	rec = get(t, "/api/graph/sources/"+sourceID.String()+"/outcomes?since=garbage", projectID.String())
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// A source from another project reads as not found.
	otherProject := uuid.NewString()
	rec = get(t, "/api/graph/sources/"+sourceID.String()+"/outcomes", otherProject)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
