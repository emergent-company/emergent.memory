package extraction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The chunk embedding budget block is a shared cache: whichever replica
// detected the exhausted budget writes the window, and every replica reads it
// (#793). These tests pin the two follow-ups from #818 against a real database —
// that the deadline lives on the database clock, and that a re-mark which would
// shorten a live window does not rewrite the row.

// TestBudgetBlockStoreDeadlineUsesDatabaseClock pins that mark derives the
// deadline in SQL. The caller passes a TTL, not an instant, so a replica whose
// application clock is off cannot cache a verdict for longer (or shorter) than
// budgetBlockCacheTTL, and active's `blocked_until > now()` judges the row on
// the clock that wrote it.
func TestBudgetBlockStoreDeadlineUsesDatabaseClock(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	store := newBudgetBlockStore(db)

	active, err := store.active(ctx, projectID)
	require.NoError(t, err)
	require.False(t, active, "a project that was never marked must not be blocked")

	require.NoError(t, store.mark(ctx, projectID, budgetBlockCacheTTL))

	// The remaining window is measured against the database's own now(), so a
	// Go-clock deadline would show up here as a skew-sized difference.
	var remainingSeconds float64
	require.NoError(t, db.NewRaw(`SELECT EXTRACT(EPOCH FROM blocked_until - now())
		FROM kb.chunk_embedding_budget_blocks WHERE project_id = ?`, projectID).
		Scan(ctx, &remainingSeconds))
	assert.InDelta(t, budgetBlockCacheTTL.Seconds(), remainingSeconds, 5,
		"the deadline must be now() + TTL on the database clock")

	active, err = store.active(ctx, projectID)
	require.NoError(t, err)
	assert.True(t, active, "a freshly marked project is inside its cached window")

	// Once the window passes (written directly here — the store has no way to
	// shorten a live window) active falls back to the authoritative check.
	_, err = db.NewRaw(`UPDATE kb.chunk_embedding_budget_blocks
		SET blocked_until = now() - interval '1 second' WHERE project_id = ?`, projectID).Exec(ctx)
	require.NoError(t, err)

	active, err = store.active(ctx, projectID)
	require.NoError(t, err)
	assert.False(t, active, "an expired window must not keep the project blocked")
}

// TestBudgetBlockStoreRemarkDoesNotRewriteALiveWindow pins the ON CONFLICT
// guard: a re-mark whose deadline is not later than the stored one updates no
// row, so `updated_at` (and the table's dead tuples) stay put. Before #818 the
// unconditional DO UPDATE moved blocked_until *backwards* and rewrote the row
// for nothing.
func TestBudgetBlockStoreRemarkDoesNotRewriteALiveWindow(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	store := newBudgetBlockStore(db)

	require.NoError(t, store.mark(ctx, projectID, budgetBlockCacheTTL))

	var firstBlockedUntil, firstUpdatedAt time.Time
	require.NoError(t, db.NewRaw(`SELECT blocked_until, updated_at
		FROM kb.chunk_embedding_budget_blocks WHERE project_id = ?`, projectID).
		Scan(ctx, &firstBlockedUntil, &firstUpdatedAt))

	// A shorter re-mark must not shorten (or rewrite) the live window.
	require.NoError(t, store.mark(ctx, projectID, time.Minute))

	var blockedUntil, updatedAt time.Time
	require.NoError(t, db.NewRaw(`SELECT blocked_until, updated_at
		FROM kb.chunk_embedding_budget_blocks WHERE project_id = ?`, projectID).
		Scan(ctx, &blockedUntil, &updatedAt))
	assert.True(t, blockedUntil.Equal(firstBlockedUntil),
		"a re-mark with an earlier deadline must leave blocked_until alone, got %s then %s",
		firstBlockedUntil, blockedUntil)
	assert.True(t, updatedAt.Equal(firstUpdatedAt),
		"a re-mark with an earlier deadline must not rewrite the row, got updated_at %s then %s",
		firstUpdatedAt, updatedAt)

	// A later re-mark still extends the window, so a still-over-budget project
	// keeps its cache after the first window expires.
	require.NoError(t, store.mark(ctx, projectID, 2*budgetBlockCacheTTL))

	require.NoError(t, db.NewRaw(`SELECT blocked_until FROM kb.chunk_embedding_budget_blocks
		WHERE project_id = ?`, projectID).Scan(ctx, &blockedUntil))
	assert.True(t, blockedUntil.After(firstBlockedUntil),
		"a re-mark with a later deadline must extend the window")
}
