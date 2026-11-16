package extraction

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin issue #793: the chunk embedding worker's "over budget" verdict
// must be shared state. A replica that did not serve the budget PATCH used to
// keep its own warm cache and re-park the jobs it dequeued until its 10-minute
// budgetBlockCacheTTL expired, even though the un-park had already made the
// rows due in the database.

// sharedBudgetBlockStore stands in for the database-backed store. One instance
// shared by two workers models two replicas reading the same rows, so the cache
// path can be exercised directly instead of with a live second process.
type sharedBudgetBlockStore struct {
	mu     sync.Mutex
	blocks map[string]time.Time
}

func newSharedBudgetBlockStore() *sharedBudgetBlockStore {
	return &sharedBudgetBlockStore{blocks: make(map[string]time.Time)}
}

func (s *sharedBudgetBlockStore) active(_ context.Context, projectID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.blocks[projectID]
	if !ok {
		return false, nil
	}
	if time.Now().After(until) {
		delete(s.blocks, projectID)
		return false, nil
	}
	return true, nil
}

func (s *sharedBudgetBlockStore) mark(_ context.Context, projectID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks[projectID] = time.Now().Add(ttl)
	return nil
}

func (s *sharedBudgetBlockStore) clear(_ context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blocks, projectID)
	return nil
}

// TestBudgetBlockInvalidationIsSharedAcrossReplicas is the acceptance test for
// #793. Replica A serves the budget change (it marks the project blocked, then
// un-parks it). Replica B never serves that request; without shared state it
// would keep its own over-budget verdict for the rest of the TTL and re-park
// every job it dequeued.
func TestBudgetBlockInvalidationIsSharedAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.NewString()
	store := newSharedBudgetBlockStore()

	// Two worker instances, each with its own in-memory state, reading one
	// shared cache: replica A handled the budget update, replica B did not.
	replicaA := &ChunkEmbeddingWorker{log: quietLogger(), budgetBlocks: store}
	replicaB := &ChunkEmbeddingWorker{log: quietLogger(), budgetBlocks: store}

	// Replica A discovers the exhausted budget and parks the project.
	replicaA.markBudgetBlocked(ctx, projectID)
	require.True(t, replicaA.budgetBlockActive(ctx, projectID))
	require.True(t, replicaB.budgetBlockActive(ctx, projectID),
		"both replicas must see the same verdict; otherwise this test proves nothing")

	// The admin raises the cap and A un-parks. B does not observe any local call.
	require.NoError(t, replicaA.UnparkProjectBudget(ctx, projectID))

	assert.False(t, replicaA.budgetBlockActive(ctx, projectID))
	assert.False(t, replicaB.budgetBlockActive(ctx, projectID),
		"a replica that did not serve the budget update must stop parking immediately, not after budgetBlockCacheTTL")
}

// TestBudgetBlockCacheWindowStillExpires guards the other half of the contract:
// the shared row is a bounded cache, so once its window passes the worker
// re-reads the budget instead of trusting a stale verdict forever.
func TestBudgetBlockCacheWindowStillExpires(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.NewString()
	store := newSharedBudgetBlockStore()
	worker := &ChunkEmbeddingWorker{log: quietLogger(), budgetBlocks: store}

	worker.markBudgetBlocked(ctx, projectID)
	require.True(t, worker.budgetBlockActive(ctx, projectID))

	// Move the window into the past, as time passing would.
	require.NoError(t, store.mark(ctx, projectID, -time.Second))
	assert.False(t, worker.budgetBlockActive(ctx, projectID),
		"an expired cache window must fall back to the authoritative budget check")
}

// failingBudgetBlockStore models a cache read that errors.
type failingBudgetBlockStore struct{ sharedBudgetBlockStore }

func (f *failingBudgetBlockStore) active(context.Context, string) (bool, error) {
	return false, assert.AnError
}

// TestBudgetBlockCacheReadFailsOpen: the cache may only skip the budget check,
// never block embedding, so a broken shared read must fall through to the
// authoritative check instead of failing the job.
func TestBudgetBlockCacheReadFailsOpen(t *testing.T) {
	worker := &ChunkEmbeddingWorker{log: quietLogger(), budgetBlocks: &failingBudgetBlockStore{}}
	assert.False(t, worker.budgetBlockActive(context.Background(), uuid.NewString()))
}
