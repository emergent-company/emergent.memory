package graph

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// pagingEnqueuer is the unit-test double for an embedding enqueuer: it records
// one batch per call so a test can prove the corpus was split into pages.
type pagingEnqueuer struct {
	batches [][]string
}

func (p *pagingEnqueuer) enqueue(_ context.Context, ids []string) (int, error) {
	batch := append([]string(nil), ids...)
	p.batches = append(p.batches, batch)
	return len(batch), nil
}

func (p *pagingEnqueuer) ids() []string {
	var all []string
	for _, batch := range p.batches {
		all = append(all, batch...)
	}
	return all
}

// corpusFetch serves a fixed, ordered corpus through the same
// (created_at, id) keyset + LIMIT contract the SQL arms implement.
func corpusFetch(corpus []reindexRow, pageSize int) func(context.Context, *reindexCursor) ([]reindexRow, error) {
	return func(_ context.Context, after *reindexCursor) ([]reindexRow, error) {
		start := 0
		if after != nil {
			for i, row := range corpus {
				if row.CreatedAt.Equal(after.createdAt) && row.ID == after.id {
					start = i + 1
					break
				}
			}
		}
		end := start + pageSize
		if end > len(corpus) {
			end = len(corpus)
		}
		return corpus[start:end], nil
	}
}

// TestEnqueueReindexIDs_MultiplePagesAndSummedCounts pins the batching contract
// of issue #677: a corpus larger than the page size is split into several
// enqueue calls, every id is enqueued exactly once, and the returned count is
// the sum across pages.
func TestEnqueueReindexIDs_MultiplePagesAndSummedCounts(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	corpus := make([]reindexRow, 5)
	for i := range corpus {
		corpus[i] = reindexRow{ID: uuid.New(), CreatedAt: base.Add(time.Duration(i) * time.Second)}
	}
	// pageSize 2 over 5 rows: pages of 2, 2, 1.
	svc := &Service{reindexPageSize: 2}
	enqueuer := &pagingEnqueuer{}

	total, err := svc.enqueueReindexIDs(context.Background(), corpusFetch(corpus, 2), enqueuer.enqueue)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, enqueuer.batches, 3, "5 rows at page size 2 must take three pages")
	require.Equal(t, []int{2, 2, 1}, []int{len(enqueuer.batches[0]), len(enqueuer.batches[1]), len(enqueuer.batches[2])})

	seen := map[string]int{}
	for _, id := range enqueuer.ids() {
		seen[id]++
	}
	require.Len(t, seen, 5)
	for _, row := range corpus {
		require.Equal(t, 1, seen[row.ID.String()], "each id must be enqueued exactly once")
	}
}

// TestEnqueueReindexIDs_ExactPageBoundary makes sure a corpus that fills the
// last page exactly does not stop before the following empty page: the loop
// must terminate on the short/empty page, never on a count coincidentally equal
// to the page size.
func TestEnqueueReindexIDs_ExactPageBoundary(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	corpus := make([]reindexRow, 4)
	for i := range corpus {
		corpus[i] = reindexRow{ID: uuid.New(), CreatedAt: base.Add(time.Duration(i) * time.Second)}
	}
	svc := &Service{reindexPageSize: 2}
	enqueuer := &pagingEnqueuer{}

	total, err := svc.enqueueReindexIDs(context.Background(), corpusFetch(corpus, 2), enqueuer.enqueue)
	require.NoError(t, err)
	require.Equal(t, 4, total)
	require.Len(t, enqueuer.batches, 2)
	require.Len(t, enqueuer.ids(), 4)
}
