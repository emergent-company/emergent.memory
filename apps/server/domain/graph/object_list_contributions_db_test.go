package graph_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// applyContributedObjects applies one changeset of n source-owned objects, so
// every one of them carries a contribution on its version row.
func applyContributedObjects(t *testing.T, ctx context.Context, svc *graph.Service, projectID, sourceID uuid.UUID, prefix, name string, n int) {
	t.Helper()
	items := make([]graph.ChangesetObjectItem, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, graph.ChangesetObjectItem{
			Type:                "Person",
			Properties:          map[string]any{"name": name},
			ExternalID:          fmt.Sprintf("%s-%d", prefix, i),
			ExternalRevisionSeq: seq(1),
		})
	}
	_, err := svc.ApplyChangeset(ctx, projectID, &graph.ApplyChangesetRequest{
		SourceID: sourceID,
		Objects:  items,
	}, nil)
	require.NoError(t, err)
}

// assertContributed checks every object in a page reports the one source that
// backs its version row.
func assertContributed(t *testing.T, sourceID uuid.UUID, objects []*graph.GraphObjectResponse) {
	t.Helper()
	for _, obj := range objects {
		require.NotNil(t, obj)
		require.Len(t, obj.Contributions, 1, "object %s lost its contributor", obj.ID)
		contrib := obj.Contributions[0]
		assert.Equal(t, obj.ID, contrib.SubjectVersionID, "the contribution must describe the version the response names")
		assert.Equal(t, sourceID, contrib.SourceID)
		assert.Equal(t, graph.ContributionSubjectObject, contrib.SubjectKind)
		assert.Equal(t, graph.ContributionStatePresent, contrib.State)
	}
}

// TestObjectListExposesContributionsInOneQuery is 12.1's list half: the object
// list reports which source backs each listed version, using the same
// omit-when-empty rule as the detail path, and a page of N contributed rows
// costs exactly one contribution query.
func TestObjectListExposesContributionsInOneQuery(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "object-list")

	const n = 3
	applyContributedObjects(t, ctx, svc, projectID, sourceID, "list-contrib", "Listed Person", n)

	bdb, ok := db.(*bun.DB)
	require.True(t, ok, "test DB must be a *bun.DB when no transaction is active")
	hook := &countingContributionSelectHook{}
	bdb.AddQueryHook(hook)

	list, err := svc.List(ctx, graph.ListParams{ProjectID: projectID, Limit: 50})
	require.NoError(t, err)
	require.Len(t, list.Items, n)
	assert.Equal(t, 1, hook.count, "the object list must read contributions once for the page, not once per row")
	assertContributed(t, sourceID, list.Items)

	// A row Memory synthesized itself reports no contributors: the field stays
	// nil (and is omitted from JSON), exactly as on the detail path.
	plain, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "Person"}, nil)
	require.NoError(t, err)
	plainList, err := svc.List(ctx, graph.ListParams{ProjectID: projectID, IDs: []uuid.UUID{plain.ID}, Limit: 1})
	require.NoError(t, err)
	require.Len(t, plainList.Items, 1)
	assert.Empty(t, plainList.Items[0].Contributions)
}

// TestObjectSearchExposesContributionsInOneQuery covers the page-shaped search
// responses (FTS, hybrid and search-with-neighbours): each reports the listed
// versions' contributors and still costs exactly one contribution query for the
// whole page.
func TestObjectSearchExposesContributionsInOneQuery(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)
	sourceID := createTestSource(t, ctx, db, projectID, "object-search")

	const (
		n    = 3
		term = "contributionsearchneedle"
	)
	applyContributedObjects(t, ctx, svc, projectID, sourceID, "search-contrib", term, n)

	bdb, ok := db.(*bun.DB)
	require.True(t, ok, "test DB must be a *bun.DB when no transaction is active")
	hook := &countingContributionSelectHook{}
	bdb.AddQueryHook(hook)

	searchObjects := func(items []*graph.SearchResultItem) []*graph.GraphObjectResponse {
		objects := make([]*graph.GraphObjectResponse, 0, len(items))
		for _, item := range items {
			objects = append(objects, item.Object)
		}
		return objects
	}

	t.Run("full-text search", func(t *testing.T) {
		hook.count = 0
		fts, err := svc.FTSSearch(ctx, projectID, &graph.FTSSearchRequest{Query: term, Limit: 50})
		require.NoError(t, err)
		require.Len(t, fts.Data, n)
		assert.Equal(t, 1, hook.count, "FTS search must read contributions once for the page")
		assertContributed(t, sourceID, searchObjects(fts.Data))
	})

	t.Run("hybrid search", func(t *testing.T) {
		hook.count = 0
		lexicalOnly := float32(0)
		hybrid, err := svc.HybridSearch(ctx, projectID, &graph.HybridSearchRequest{
			Query:        term,
			Limit:        50,
			VectorWeight: &lexicalOnly,
		}, nil)
		require.NoError(t, err)
		require.Len(t, hybrid.Data, n)
		assert.Equal(t, 1, hook.count, "hybrid search must read contributions once for the page")
		assertContributed(t, sourceID, searchObjects(hybrid.Data))
	})

	t.Run("search with neighbours", func(t *testing.T) {
		hook.count = 0
		withNeighbors, err := svc.SearchWithNeighbors(ctx, projectID, &graph.SearchWithNeighborsRequest{
			Query: term,
			Limit: 50,
		})
		require.NoError(t, err)
		require.Len(t, withNeighbors.PrimaryResults, n)
		assert.Equal(t, 1, hook.count, "search-with-neighbours must read contributions once for the response")
		primary := make([]*graph.GraphObjectResponse, 0, len(withNeighbors.PrimaryResults))
		for _, item := range withNeighbors.PrimaryResults {
			primary = append(primary, item.Object)
		}
		assertContributed(t, sourceID, primary)
	})
}
