package graph_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestFTSSearchFallbackDoesNotAdvertiseHasMore is the regression test for issue
// #1052: the Relax/Disjoin fallback is gated to the first page, so a multi-term
// query whose OR fallback matches more than `limit` documents must NOT report
// HasMore=true — page 2 would be empty. The fix must leave page 1's results
// unchanged and only correct the flag.
func TestFTSSearchFallbackDoesNotAdvertiseHasMore(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	// Four objects, each holding a strict subset of the query's terms, so the
	// strict AND query is unsatisfiable while the OR fallback matches all four.
	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/aaaa-1", `{"title":"aksjeloven"}`)
	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/bbbb-2", `{"title":"innbetaling"}`)
	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/cccc-3", `{"title":"aksjekapital"}`)
	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/dddd-4", `{"title":"innskudd penger"}`)

	const query = "aksjeloven innbetaling aksjekapital innskudd penger"

	// Guard the premise: the strict query really matches nothing, so the results
	// below can only come from the disjoined OR fallback.
	if got := countStrictMatchesDual(t, ctx, db, projectID, query); got != 0 {
		t.Fatalf("premise changed: strict tsquery matches %d object(s), so this test no longer proves the fallback is what filled the page", got)
	}

	const limit = 2

	// Confirm the fallback engages and matches more than `limit` documents, so
	// the pre-fix `limit+1` has-more computation would report HasMore=true.
	fallbackResults, fellBack, err := repo.FTSSearchWithFallback(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     limit + 1,
	})
	require.NoError(t, err)
	require.True(t, fellBack, "disjoined fallback must engage for the multi-term query")
	require.Len(t, fallbackResults, limit+1, "fallback must match more than limit documents")

	// The fix: page 1 returns exactly `limit` results with HasMore=false, and
	// those results are unchanged (still the fallback's top-`limit` documents).
	resp, err := svc.FTSSearch(ctx, projectID, &graph.FTSSearchRequest{Query: query, Limit: limit})
	require.NoError(t, err)
	require.Len(t, resp.Data, limit, "page 1 must return exactly limit documents")
	require.False(t, resp.HasMore, "first-page-only fallback must not advertise a second page")

	for i, item := range resp.Data {
		require.Equal(t, fallbackResults[i].Object.ID, item.Object.ID,
			"page 1 result set must be unchanged (top-%d fallback documents)", limit)
	}

	// And page 2 (the page HasMore would have promised) is empty.
	second, err := svc.FTSSearch(ctx, projectID, &graph.FTSSearchRequest{Query: query, Limit: limit, Offset: limit})
	require.NoError(t, err)
	require.Empty(t, second.Data, "page 2 must be empty: the fallback is gated to the first page")
	require.False(t, second.HasMore, "page 2 must not advertise a third page")
}
