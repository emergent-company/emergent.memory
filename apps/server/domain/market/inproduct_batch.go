package market

import (
	"sync"
	"time"
)

// batchCache holds the most recent authorization batch per entitlement. It is
// the instance's outage buffer: a batch lives for up to MaxBatchTTL, so calls
// continue while the market is unreachable. Reporting is tracked separately, so
// a call made inside a batch can still be attributed after the batch expires.
type batchCache struct {
	mu     sync.Mutex
	now    func() time.Time
	states map[string]*batchState
}

type batchState struct {
	batch    *Batch
	used     map[string]struct{}
	reported map[string]struct{}
}

func newBatchCache(now func() time.Time) *batchCache {
	return &batchCache{now: now, states: make(map[string]*batchState)}
}

// put replaces the entitlement's batch. A fresh authorization supersedes the
// old one; ids from the old batch are no longer acceptable for new calls, while
// usage reports already queued keep their own request id.
func (c *batchCache) put(batch *Batch) {
	if batch == nil || batch.EntitlementID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.states[batch.EntitlementID] = &batchState{
		batch:    batch,
		used:     make(map[string]struct{}, len(batch.RequestIDs)),
		reported: make(map[string]struct{}, len(batch.RequestIDs)),
	}
}

// get returns the entitlement's cached batch without a liveness filter.
func (c *batchCache) get(entitlementID string) *Batch {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.states[entitlementID]; state != nil {
		return state.batch
	}
	return nil
}

// live returns the entitlement's batch when the assertion is still valid and at
// least one request id is unused.
func (c *batchCache) live(entitlementID string, now time.Time) *Batch {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[entitlementID]
	if state == nil || !state.batch.ExpiresAt.After(now) {
		return nil
	}
	if len(state.used) >= len(state.batch.RequestIDs) {
		return nil
	}
	return state.batch
}

// remaining reports how many pre-issued request ids the live batch still has.
func (c *batchCache) remaining(entitlementID string, now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[entitlementID]
	if state == nil || !state.batch.ExpiresAt.After(now) {
		return 0
	}
	remaining := len(state.batch.RequestIDs) - len(state.used)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// take consumes one pre-issued request id. An expired batch is refused even if
// ids remain: the seller would reject the assertion anyway.
func (c *batchCache) take(entitlementID string, now time.Time) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[entitlementID]
	if state == nil || !state.batch.ExpiresAt.After(now) {
		return "", ErrBatchExpired(entitlementID)
	}
	for _, id := range state.batch.RequestIDs {
		if _, used := state.used[id]; used {
			continue
		}
		state.used[id] = struct{}{}
		return id, nil
	}
	return "", ErrBatchExhausted(entitlementID)
}

// reserve claims a request id for one usage report. Unknown ids are refused
// locally (the fabricated-report control), and a second report for the same id
// is refused as a duplicate. Reserving before the network call also prevents
// concurrent duplicate sends.
func (c *batchCache) reserve(entitlementID, requestID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[entitlementID]
	if state == nil || !containsRequestID(state.batch.RequestIDs, requestID) {
		return ErrUsageRequestUnknown(requestID)
	}
	if _, reported := state.reported[requestID]; reported {
		return ErrUsageRequestDuplicated(requestID)
	}
	state.reported[requestID] = struct{}{}
	return nil
}

func containsRequestID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
