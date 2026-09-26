package graph_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestFTSSearchDisjoinsMultiTermQuery is the regression test for the keyword
// leg of issue #996: a natural multi-term query whose terms are spread across
// many objects (no single object contains every term) matches nothing under
// websearch_to_tsquery's AND semantics, so the lexical leg returns zero rows and
// cannot correct the vector leg. The disjoined fallback ORs the terms and ranks
// by ts_rank_cd, restoring recall without trading away single-term precision.
func TestFTSSearchDisjoinsMultiTermQuery(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	// Legal-ish fixture from the issue: each object shares some but not all of
	// the query's terms, so the AND query is unsatisfiable while each term is
	// individually well represented.
	lawID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)"}`)
	insertKeyedObject(t, ctx, db, projectID, "LegalParagraph", "lov/1997-06-13-44#kapittel-2-kapittel-1-paragraf-7",
		`{"title":"Innbetaling av aksjekapitalen"}`)
	insertKeyedObject(t, ctx, db, projectID, "LegalParagraph", "lov/1997-06-13-44#kapittel-2-kapittel-1-paragraf-12",
		`{"title":"Innskudd penger andre formuesgoder"}`)
	insertKeyedObject(t, ctx, db, projectID, "Regulation", "forskrift/2007-06-29-876",
		`{"title":"Innbetaling av aksjekapitalen innskudd penger"}`)

	const query = "aksjeloven innbetaling av aksjekapitalen innskudd penger andre formuesgoder"

	// Fail-first premise: the strict AND query really does match nothing here, so
	// the results below can only come from the disjoined OR fallback.
	if got := countStrictMatchesDual(t, ctx, db, projectID, query); got != 0 {
		t.Fatalf("premise changed: strict tsquery matches %d object(s), so this test no longer proves the disjoined fallback is what restored recall", got)
	}

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results, "multi-term query must return results via the disjoined fallback")

	// Recall restored: the correct statute is present in the result set even
	// though no single object holds every term.
	var foundLaw bool
	for _, r := range results {
		if r.Object.ID == lawID {
			foundLaw = true
			break
		}
	}
	assert.True(t, foundLaw, "the law object must be returned by the disjoined fallback")
}

// TestFTSSearchDisjoinOrdersByCoverage pins the ranking property of the OR
// fallback: ts_rank_cd over the disjunction rewards objects that cover more of
// the query's terms, so ranking is not collapsed to "any one term".
func TestFTSSearchDisjoinOrdersByCoverage(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	// Three objects covering 1, 2 and 4 of the query terms respectively.
	lowID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/aaaa-1",
		`{"title":"aksjeloven"}`)
	midID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/bbbb-2",
		`{"title":"innbetaling aksjekapital"}`)
	highID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/cccc-3",
		`{"title":"innbetaling aksjekapital innskudd penger"}`)

	const query = "aksjeloven innbetaling aksjekapital innskudd penger"

	if got := countStrictMatchesDual(t, ctx, db, projectID, query); got != 0 {
		t.Fatalf("premise changed: strict tsquery matches %d object(s), so this test no longer proves ordering comes from the disjoined fallback", got)
	}

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, results, 3, "all three objects should be returned by the disjoined fallback")

	// Coverage ordering: 4-term object first, 1-term object last.
	assert.Equal(t, highID, results[0].Object.ID, "highest-coverage object must rank first")
	assert.Equal(t, midID, results[1].Object.ID, "medium-coverage object must rank second")
	assert.Equal(t, lowID, results[2].Object.ID, "lowest-coverage object must rank last")
}

// TestFTSSearchSingleTermKeepsPrecision guards that the disjoined fallback never
// engages for a single-term query, so single-term precision is unchanged: the
// strict AND query already matches and its result is returned verbatim.
func TestFTSSearchSingleTermKeepsPrecision(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	lawID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)"}`)
	// A distractor that shares no lexeme with the single-term query.
	insertKeyedObject(t, ctx, db, projectID, "Regulation", "forskrift/2007-06-29-876",
		`{"title":"Forskrift om utbytte"}`)

	const query = "aksjeloven"

	// Premise: the strict query matches exactly the law, so the fallback must not
	// run and widen the result set.
	require.Equal(t, 1, countStrictMatchesDual(t, ctx, db, projectID, query),
		"premise changed: strict single-term query should match exactly one object")

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1, "single-term query must not be widened by the disjoined fallback")
	assert.Equal(t, lawID, results[0].Object.ID)
}

// TestFTSSearchDisjoinGatedToFirstPage mirrors the Relax gating: a later page
// returning zero rows means the caller paged past every strict match, so the
// disjoined fallback must not refill it with matches the strict query never
// ranked.
func TestFTSSearchDisjoinGatedToFirstPage(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/aaaa-1", `{"title":"aksjeloven"}`)
	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/bbbb-2", `{"title":"innbetaling aksjekapital"}`)

	const query = "aksjeloven innbetaling aksjekapital"

	if got := countStrictMatchesDual(t, ctx, db, projectID, query); got != 0 {
		t.Fatalf("premise changed: strict tsquery matches %d object(s), so this test no longer proves the fallback is gated", got)
	}

	first, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, first, 2, "disjoined fallback should find both objects on the first page")

	second, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
		Offset:    1,
	})
	require.NoError(t, err)
	assert.Empty(t, second, "disjoined fallback must not refill a page past all strict matches")
}
