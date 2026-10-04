package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/rerank"
)

type fakeReranker struct {
	order    []int
	err      error
	gotQuery string
	gotDocs  []string
}

func (f *fakeReranker) Rerank(_ context.Context, query string, documents []string) ([]rerank.Result, error) {
	f.gotQuery = query
	f.gotDocs = documents
	if f.err != nil {
		return nil, f.err
	}
	out := make([]rerank.Result, 0, len(f.order))
	for i, idx := range f.order {
		out = append(out, rerank.Result{Index: idx, Score: float64(len(f.order) - i)})
	}
	return out, nil
}

func items(ids ...string) []UnifiedSearchResultItem {
	out := make([]UnifiedSearchResultItem, len(ids))
	for i, id := range ids {
		out[i] = UnifiedSearchResultItem{Type: ItemTypeGraph, ID: id, Key: id, Score: float32(10 - i)}
	}
	return out
}

func idsOf(list []UnifiedSearchResultItem) []string {
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.ID
	}
	return out
}

func TestRerankItemsDefaultUnchanged(t *testing.T) {
	original := items("a", "b", "c", "d")

	got, ok := rerankItems(context.Background(), "q", original, nil, 50)
	assert.False(t, ok)
	assert.Equal(t, []string{"a", "b", "c", "d"}, idsOf(got))

	// topN <= 0 is also a no-op.
	got, ok = rerankItems(context.Background(), "q", original, &fakeReranker{order: []int{1, 0}}, 0)
	assert.False(t, ok)
	assert.Equal(t, []string{"a", "b", "c", "d"}, idsOf(got))
}

func TestRerankItemsReordersTopBlockOnly(t *testing.T) {
	original := items("a", "b", "c", "d")
	fr := &fakeReranker{order: []int{1, 0}}

	got, ok := rerankItems(context.Background(), "q", original, fr, 2)
	require.True(t, ok)

	// Top-2 block reordered (b, a) with provider scores; tail preserved.
	assert.Equal(t, []string{"b", "a", "c", "d"}, idsOf(got))
	assert.InDelta(t, 2.0, got[0].Score, 1e-6)
	assert.InDelta(t, 1.0, got[1].Score, 1e-6)

	// Only the top-N block was sent, carrying the query.
	assert.Equal(t, "q", fr.gotQuery)
	require.Len(t, fr.gotDocs, 2)
	// Documents are sent in the original block order (index i = block[i]).
	assert.Contains(t, fr.gotDocs[0], "a")
	assert.Contains(t, fr.gotDocs[1], "b")
}

func TestRerankItemsErrorFallsBackToFusedOrder(t *testing.T) {
	original := items("a", "b", "c")
	fr := &fakeReranker{err: errors.New("provider down")}

	got, ok := rerankItems(context.Background(), "q", original, fr, 3)
	assert.False(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, idsOf(got))
	assert.InDelta(t, 10.0, got[0].Score, 1e-6, "scores must be untouched on fallback")
}

func TestRerankItemsTopNLargerThanInput(t *testing.T) {
	original := items("a", "b", "c")
	fr := &fakeReranker{order: []int{2, 1, 0}}

	got, ok := rerankItems(context.Background(), "q", original, fr, 50)
	require.True(t, ok)
	assert.Equal(t, []string{"c", "b", "a"}, idsOf(got))
	require.Len(t, fr.gotDocs, 3)
}

func TestRerankItemsIgnoresOutOfRangeIndexes(t *testing.T) {
	original := items("a", "b")
	fr := &fakeReranker{order: []int{5, 1}} // 5 is out of range

	got, ok := rerankItems(context.Background(), "q", original, fr, 2)
	require.True(t, ok)
	// Index 1 first, then the omitted index 0 appended in original order.
	assert.Equal(t, []string{"b", "a"}, idsOf(got))
}

func TestApplyRerankNilServiceKeepsOrder(t *testing.T) {
	svc := newTestService()
	original := items("a", "b")
	got := svc.applyRerank(context.Background(), "q", original)
	assert.Equal(t, []string{"a", "b"}, idsOf(got))
}

func TestSearchItemText(t *testing.T) {
	text := searchItemText(UnifiedSearchResultItem{Type: ItemTypeText, Snippet: "hello world"})
	assert.Equal(t, "hello world", text)

	rel := searchItemText(UnifiedSearchResultItem{Type: ItemTypeRelationship, TripletText: "a -> b"})
	assert.Equal(t, "a -> b", rel)

	graph := searchItemText(UnifiedSearchResultItem{
		Type:   ItemTypeGraph,
		Key:    "quarterly-report",
		Fields: map[string]any{"title": "Q3"},
	})
	assert.True(t, strings.Contains(graph, "quarterly-report"))
	assert.True(t, strings.Contains(graph, "Q3"))
}
