package journal

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestList_KeysetPaginationStable pages through a real database using the
// next_cursor from each response and asserts the cursor walk is a stable,
// duplicate-free, gap-free traversal of every inserted row, in strict
// (created_at DESC, id DESC) order.
//
// Regression guards this test bites:
//   - flipping the keyset comparison direction (`<` → `>`) would re-serve
//     already-seen rows / loop, failing the duplicate/no-progress checks;
//   - dropping the `id DESC` tie-break would make same-timestamp ordering
//     nondeterministic across pages.
func TestList_KeysetPaginationStable(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "journal_list")
	t.Cleanup(tdb.Close)

	repo := NewRepository(tdb.DB, branches.NewStore(tdb.DB))
	ctx := context.Background()

	projectID := uuid.New()
	// Microsecond-truncated base, matching Postgres timestamptz precision.
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	// 25 entries; three share each timestamp so the id tie-break is exercised.
	var entries []*JournalEntry
	for i := 0; i < 25; i++ {
		entries = append(entries, &JournalEntry{
			ID:        uuid.New(),
			ProjectID: projectID,
			EventType: EventTypeCreated,
			ActorType: ActorSystem,
			Metadata:  map[string]any{},
			CreatedAt: base.Add(-time.Duration(i/3) * time.Second),
		})
	}
	require.NoError(t, repo.InsertBatch(ctx, entries), "InsertBatch must accept a slice of entries")

	seen := map[uuid.UUID]int{}
	var ordered []*JournalEntry
	var cursor *string
	const limit = 4

	for pages := 0; ; pages++ {
		require.Less(t, pages, 50, "pagination failed to terminate")

		page, hasMore, next, err := repo.List(ctx, ListParams{
			ProjectID: projectID,
			Limit:     limit,
			Cursor:    cursor,
		})
		require.NoError(t, err)

		for _, e := range page {
			seen[e.ID]++
			ordered = append(ordered, e)
		}

		if !hasMore {
			assert.Empty(t, next, "final page must not return a cursor")
			break
		}
		require.NotEmpty(t, next, "hasMore page must return a cursor")
		cursor = &next
	}

	require.Len(t, seen, len(entries), "cursor walk must visit every row with no gaps")
	for id, count := range seen {
		require.Equal(t, 1, count, "entry %s visited more than once", id)
	}

	for i := 1; i < len(ordered); i++ {
		prev, cur := ordered[i-1], ordered[i]
		if prev.CreatedAt.Equal(cur.CreatedAt) {
			assert.Greater(t, bytes.Compare(prev.ID[:], cur.ID[:]), 0,
				"ties must be ordered by id DESC at index %d", i)
		} else {
			assert.True(t, prev.CreatedAt.After(cur.CreatedAt),
				"entries must be ordered by created_at DESC at index %d", i)
		}
	}
}

// TestList_InvalidCursorRejected verifies a malformed cursor fails the read
// (handler maps ErrInvalidCursor to 400) rather than silently returning a page.
func TestList_InvalidCursorRejected(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "journal_cursor")
	t.Cleanup(tdb.Close)

	repo := NewRepository(tdb.DB, branches.NewStore(tdb.DB))

	bad := "not-a-cursor"
	_, _, _, err := repo.List(context.Background(), ListParams{
		ProjectID: uuid.New(),
		Limit:     10,
		Cursor:    &bad,
	})
	assert.ErrorIs(t, err, ErrInvalidCursor)
}
