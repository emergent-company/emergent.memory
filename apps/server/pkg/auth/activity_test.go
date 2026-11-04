package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActivityTestTracker() *activityTracker {
	return &activityTracker{
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		throttle:  activityThrottle,
		lastFlush: make(map[string]time.Time),
		now:       time.Now,
	}
}

// The throttle is keyed per (user, org, project): activity in one project must
// not suppress the first touch in another, and a repeat within the window must
// not enqueue a second write.
func TestActivityTrackerThrottleDecision(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	tr := newActivityTestTracker()
	tr.now = func() time.Time { return now }

	require.True(t, tr.shouldFlush("u1|o1|p1"), "first touch must be due")
	assert.False(t, tr.shouldFlush("u1|o1|p1"), "immediate repeat must be throttled")
	assert.True(t, tr.shouldFlush("u1|o1|p2"), "another project must be independent")

	now = now.Add(activityThrottle - time.Second)
	assert.False(t, tr.shouldFlush("u1|o1|p1"), "just inside the window is throttled")

	now = now.Add(time.Second)
	require.True(t, tr.shouldFlush("u1|o1|p1"), "at the window boundary it is due again")
}

// A nil tracker must be inert — the middleware is constructed in test and
// standalone postures where the tracker is absent.
func TestActivityTrackerNilSafe(t *testing.T) {
	var tr *activityTracker
	assert.NotPanics(t, func() { tr.touch("u1", "o1", "p1") })
}

// touch never blocks: the flush runs on a detached goroutine. Assert the
// arguments reach the flush seam and that a failure is swallowed.
func TestActivityTrackerFlushSeam(t *testing.T) {
	tr := newActivityTestTracker()
	tr.now = func() time.Time { return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC) }

	var (
		mu           sync.Mutex
		gotUser      string
		gotOrg       string
		gotProject   string
		flushEntered = make(chan struct{})
	)
	tr.flushFn = func(_ context.Context, userID, orgID, projectID string) error {
		mu.Lock()
		gotUser, gotOrg, gotProject = userID, orgID, projectID
		mu.Unlock()
		close(flushEntered)
		return errors.New("boom, must be swallowed")
	}

	tr.touch("user-1", "org-1", "proj-1")
	select {
	case <-flushEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("flush seam was not called")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "user-1", gotUser)
	assert.Equal(t, "org-1", gotOrg)
	assert.Equal(t, "proj-1", gotProject)
}
