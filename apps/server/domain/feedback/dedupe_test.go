package feedback

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
)

type storedClaim struct {
	State       string    `bun:"state"`
	ClaimToken  string    `bun:"claim_token"`
	IssueNumber *int      `bun:"issue_number"`
	IssueURL    *string   `bun:"issue_url"`
	ClaimedAt   time.Time `bun:"claimed_at"`
}

func getClaim(t *testing.T, db bun.IDB, identity, key string) (*storedClaim, bool) {
	t.Helper()
	var row storedClaim
	err := db.NewRaw(`SELECT state, claim_token, issue_number, issue_url, claimed_at
		FROM kb.feedback_dedupe_claims WHERE owner_identity = ? AND dedupe_key = ?`,
		identity, key).Scan(context.Background(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	require.NoError(t, err)
	return &row, true
}

func claimCount(t *testing.T, db bun.IDB) int {
	t.Helper()
	var n int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.feedback_dedupe_claims`).Scan(context.Background(), &n))
	return n
}

func insertClaim(t *testing.T, db bun.IDB, identity, key, state, token string, number *int, url *string, claimedAt time.Time) {
	t.Helper()
	_, err := db.NewRaw(`INSERT INTO kb.feedback_dedupe_claims
		(owner_identity, dedupe_key, state, claim_token, issue_number, issue_url, claimed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		identity, key, state, token, number, url, claimedAt, claimedAt).Exec(context.Background())
	require.NoError(t, err)
}

func setClaimedAt(t *testing.T, db bun.IDB, identity, key string, at time.Time) {
	t.Helper()
	_, err := db.NewRaw(`UPDATE kb.feedback_dedupe_claims SET claimed_at = ?, updated_at = ? WHERE owner_identity = ? AND dedupe_key = ?`,
		at, at, identity, key).Exec(context.Background())
	require.NoError(t, err)
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

type submitOutcome struct {
	result *Result
	err    error
}

func receiveOutcome(t *testing.T, ch <-chan submitOutcome) submitOutcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for a submission result")
		return submitOutcome{}
	}
}

// TestConcurrentIdenticalSubmissionsFileOneIssue is the core atomicity test:
// N goroutines submit the identical submission for one identity while the first
// GitHub call is held in flight. Exactly one issue is filed, one budget slot is
// spent, and every loser gets the documented "in progress" answer.
func TestConcurrentIdenticalSubmissionsFileOneIssue(t *testing.T) {
	svc, fake, db := testServiceWithDB(t, func(c *config.FeedbackConfig) {
		c.RateLimitPerHour = 2
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	fake.onCall = func(int) {
		startOnce.Do(func() { close(started) })
		<-release
	}

	const n = 8
	outs := make(chan submitOutcome, n)
	for range n {
		go func() {
			result, err := svc.Submit(context.Background(), validSubmission())
			outs <- submitOutcome{result: result, err: err}
		}()
	}

	<-started
	for range n - 1 {
		o := receiveOutcome(t, outs)
		require.Nil(t, o.result, "a concurrent duplicate must not return an issue")
		var inProgress *InProgressError
		require.ErrorAs(t, o.err, &inProgress)
		require.Positive(t, inProgress.RetryAfter)
	}
	close(release)

	winner := receiveOutcome(t, outs)
	require.NoError(t, winner.err)
	require.NotNil(t, winner.result)
	require.Equal(t, 42, winner.result.IssueNumber)
	require.False(t, winner.result.Duplicate)
	require.Equal(t, 1, fake.callCount(), "exactly one GitHub issue may be created")
	require.Equal(t, 1, claimCount(t, db), "claims rotate; they do not accumulate")

	// A budget of two: one slot was spent by the burst, so one distinct message
	// still fits and the next is limited. If every loser had reserved a slot,
	// this first distinct message would already be limited.
	svc.guard.mu.Lock()
	hits := len(svc.guard.entries["user-1"].hits)
	svc.guard.mu.Unlock()
	require.Equal(t, 1, hits, "exactly one budget slot consumed")

	first := validSubmission()
	first.Message = "a different report"
	_, err := svc.Submit(context.Background(), first)
	require.NoError(t, err)

	second := validSubmission()
	second.Message = "yet another report"
	_, err = svc.Submit(context.Background(), second)
	var limited *RateLimitError
	require.ErrorAs(t, err, &limited)
}

// TestFailedGitHubCallReleasesClaim proves the claim is given back on a
// delivery failure so a retry can file, and that the retry then records filed.
func TestFailedGitHubCallReleasesClaim(t *testing.T) {
	svc, fake, db := testServiceWithDB(t, nil)

	fake.mu.Lock()
	fake.err = errors.New("github is down")
	fake.mu.Unlock()

	_, err := svc.Submit(context.Background(), validSubmission())
	require.Error(t, err)
	require.Zero(t, claimCount(t, db), "the pending claim must be released")

	fake.mu.Lock()
	fake.err = nil
	fake.mu.Unlock()

	result, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)
	require.Equal(t, 42, result.IssueNumber)
	require.False(t, result.Duplicate)
	require.Equal(t, 2, fake.callCount())

	claim, ok := getClaim(t, db, "user-1", submissionFingerprint(validSubmission()))
	require.True(t, ok)
	require.Equal(t, "filed", claim.State)
	require.NotNil(t, claim.IssueNumber)
	require.Equal(t, 42, *claim.IssueNumber)
}

// TestSameTextFromTwoIdentitiesFilesTwice proves identity is part of the key:
// one user's submission must not suppress another user's.
func TestSameTextFromTwoIdentitiesFilesTwice(t *testing.T) {
	svc, fake, db := testServiceWithDB(t, nil)

	a := validSubmission()
	a.UserID = "user-a"
	a.UserEmail = "a@example.com"
	b := validSubmission()
	b.UserID = "user-b"
	b.UserEmail = "b@example.com"

	ra, err := svc.Submit(context.Background(), a)
	require.NoError(t, err)
	require.False(t, ra.Duplicate)

	rb, err := svc.Submit(context.Background(), b)
	require.NoError(t, err)
	require.False(t, rb.Duplicate, "a different identity must not be suppressed")

	require.Equal(t, 2, fake.callCount())
	require.Equal(t, 2, claimCount(t, db))
}

// TestFiledClaimWindowRotation: a filed claim inside the dedupe window is the
// answer; once the window has passed the same content may be filed again by
// rotating the existing row.
func TestFiledClaimWindowRotation(t *testing.T) {
	svc, fake, db := testServiceWithDB(t, nil)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	stale := validSubmission()
	staleKey := submissionFingerprint(stale)
	insertClaim(t, db, "user-1", staleKey, "filed", uuid.NewString(), intPtr(99), strPtr("https://example.test/99"),
		now.Add(-dedupeWindow-time.Minute))

	result, err := svc.Submit(ctx, stale)
	require.NoError(t, err)
	require.False(t, result.Duplicate, "a claim outside the window may be taken over")
	require.Equal(t, 42, result.IssueNumber)
	require.Equal(t, 1, fake.callCount())

	fresh := validSubmission()
	fresh.Message = "a fresh duplicate within the window"
	freshKey := submissionFingerprint(fresh)
	insertClaim(t, db, "user-1", freshKey, "filed", uuid.NewString(), intPtr(77), strPtr("https://example.test/77"),
		now.Add(-time.Minute))

	dup, err := svc.Submit(ctx, fresh)
	require.NoError(t, err)
	require.True(t, dup.Duplicate, "a fresh filed claim is the answer")
	require.Equal(t, 77, dup.IssueNumber)
	require.Equal(t, 1, fake.callCount(), "a duplicate must not file another issue")

	claim, ok := getClaim(t, db, "user-1", freshKey)
	require.True(t, ok)
	require.Equal(t, "filed", claim.State)
	require.Equal(t, 77, *claim.IssueNumber)
}

// TestPendingClaimTTLTakeover: a fresh pending claim answers "in progress"; one
// older than the crash TTL is abandoned and may be taken over.
func TestPendingClaimTTLTakeover(t *testing.T) {
	svc, fake, db := testServiceWithDB(t, nil)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	sub := validSubmission()
	key := submissionFingerprint(sub)
	token := uuid.NewString()
	insertClaim(t, db, "user-1", key, "pending", token, nil, nil, now.Add(-30*time.Second))

	_, err := svc.Submit(ctx, sub)
	var inProgress *InProgressError
	require.ErrorAs(t, err, &inProgress)
	require.Positive(t, inProgress.RetryAfter)
	require.Zero(t, fake.callCount(), "a fresh pending claim must not be duplicated")

	setClaimedAt(t, db, "user-1", key, now.Add(-claimTTL-time.Minute))

	result, err := svc.Submit(ctx, sub)
	require.NoError(t, err)
	require.Equal(t, 42, result.IssueNumber)
	require.False(t, result.Duplicate)
	require.Equal(t, 1, fake.callCount(), "an expired pending claim is taken over")

	claim, ok := getClaim(t, db, "user-1", key)
	require.True(t, ok)
	require.Equal(t, "filed", claim.State)
	require.NotEqual(t, token, claim.ClaimToken, "takeover rotates the claim token")
}
