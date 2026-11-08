package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

type fakeIssueCreator struct {
	mu    sync.Mutex
	calls []fakeIssueCall
	issue *createdIssue
	err   error
	// onCall, when set, runs after the call is recorded and before it returns.
	// A test uses it to hold the first (winning) GitHub call in flight while
	// the other submissions arrive.
	onCall func(callIndex int)
}

type fakeIssueCall struct {
	title  string
	body   string
	labels []string
}

func (f *fakeIssueCreator) CreateIssue(_ context.Context, title, body string, labels []string) (*createdIssue, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeIssueCall{title: title, body: body, labels: labels})
	index := len(f.calls)
	issue, err, onCall := f.issue, f.err, f.onCall
	f.mu.Unlock()

	if onCall != nil {
		onCall(index)
	}
	if err != nil {
		return nil, err
	}
	if issue != nil {
		return issue, nil
	}
	return &createdIssue{Number: 42, HTMLURL: "https://github.com/o/r/issues/42"}, nil
}

func (f *fakeIssueCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// connectFeedbackDB opens a throwaway test database owned by this test. The
// harness applies the embedded migrations to a template first, so a new
// migration is exercised here rather than only on a developer's database.
func connectFeedbackDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "feedback")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

func testService(t *testing.T, mutate func(*config.FeedbackConfig)) (*Service, *fakeIssueCreator) {
	t.Helper()
	svc, fake, _ := testServiceWithDB(t, mutate)
	return svc, fake
}

func testServiceWithDB(t *testing.T, mutate func(*config.FeedbackConfig)) (*Service, *fakeIssueCreator, *bun.DB) {
	t.Helper()
	cfg := &config.Config{
		Environment: "test",
		Feedback: config.FeedbackConfig{
			Enabled:          true,
			GitHubToken:      "test-token",
			Repo:             "acme/feedback-intake",
			APIBaseURL:       "https://api.github.com",
			Label:            "feedback",
			RateLimitPerHour: 5,
		},
	}
	if mutate != nil {
		mutate(&cfg.Feedback)
	}
	db := connectFeedbackDB(t)
	svc := NewService(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fake := &fakeIssueCreator{}
	if svc.Enabled() {
		svc.client = fake
	}
	return svc, fake, db
}

func validSubmission() Submission {
	return Submission{
		Message:        "Search returns stale results after an entity update.",
		Category:       "bug",
		Severity:       "medium",
		IncludeContext: true,
		Source:         SourceAPI,
		Client:         "test-agent/1.0",
		UserID:         "user-1",
		UserEmail:      "ada@example.com",
		ProjectID:      "project-1",
	}
}

func TestSubmitFilesIssueWithIdentityAndContext(t *testing.T) {
	svc, fake := testService(t, nil)

	result, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)
	require.Equal(t, 42, result.IssueNumber)
	require.Equal(t, "https://github.com/o/r/issues/42", result.IssueURL)
	require.False(t, result.Duplicate)

	require.Len(t, fake.calls, 1)
	call := fake.calls[0]
	require.Equal(t, "feedback", call.labels[0])
	require.Contains(t, call.title, "[feedback]")
	require.Contains(t, call.title, "Search returns stale results")
	require.Contains(t, call.body, "Search returns stale results after an entity update.")
	require.Contains(t, call.body, "ada@example.com")
	require.Contains(t, call.body, "`user-1`")
	require.Contains(t, call.body, "`project-1`")
	require.Contains(t, call.body, "test-agent/1.0")
	require.Contains(t, call.body, "`test`")
}

func TestSubmitDedupesIdenticalRetry(t *testing.T) {
	svc, fake := testService(t, nil)

	first, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)
	second, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)

	require.Len(t, fake.calls, 1, "a retry must not file a second issue")
	require.True(t, second.Duplicate)
	require.Equal(t, first.IssueNumber, second.IssueNumber)
}

func TestSubmitRateLimitsPerUser(t *testing.T) {
	svc, _ := testService(t, func(c *config.FeedbackConfig) { c.RateLimitPerHour = 2 })

	for i := range 2 {
		sub := validSubmission()
		sub.Message = strings.Repeat("distinct message ", i+1)
		_, err := svc.Submit(context.Background(), sub)
		require.NoError(t, err)
	}

	sub := validSubmission()
	sub.Message = "a third, distinct message"
	_, err := svc.Submit(context.Background(), sub)

	var limited *RateLimitError
	require.ErrorAs(t, err, &limited)
	require.Positive(t, limited.RetryAfter)
}

func TestSubmitValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Submission)
	}{
		{"empty message", func(s *Submission) { s.Message = "   " }},
		{"unknown category", func(s *Submission) { s.Category = "complaint" }},
		{"unknown severity", func(s *Submission) { s.Severity = "urgent" }},
		{"message too long", func(s *Submission) { s.Message = strings.Repeat("x", maxMessageBytes+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, fake := testService(t, nil)
			sub := validSubmission()
			tc.mutate(&sub)
			_, err := svc.Submit(context.Background(), sub)
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Empty(t, fake.calls)
		})
	}
}

func TestSubmitRefusedWhenNotConfigured(t *testing.T) {
	svc, _ := testService(t, func(c *config.FeedbackConfig) { c.GitHubToken = "" })
	require.False(t, svc.Enabled())

	_, err := svc.Submit(context.Background(), validSubmission())
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestServiceDisabledWithoutRepository(t *testing.T) {
	// There is no built-in destination: a token alone must not file anywhere,
	// and must not produce the "invalid GitHub configuration" warning that a
	// malformed value earns.
	svc, _ := testService(t, func(c *config.FeedbackConfig) { c.Repo = "" })
	require.False(t, svc.Enabled())

	_, err := svc.Submit(context.Background(), validSubmission())
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestServiceDisabledByKillSwitch(t *testing.T) {
	svc, _ := testService(t, func(c *config.FeedbackConfig) { c.Enabled = false })
	require.False(t, svc.Enabled())
}

func TestSubmitWithoutContextOmitsProjectAndEnvironment(t *testing.T) {
	svc, fake := testService(t, nil)
	sub := validSubmission()
	sub.IncludeContext = false

	_, err := svc.Submit(context.Background(), sub)
	require.NoError(t, err)

	body := fake.calls[0].body
	require.Contains(t, body, "ada@example.com")
	require.NotContains(t, body, "`project-1`")
	require.NotContains(t, body, "test-agent/1.0")
	require.NotContains(t, body, "Environment")
}

func TestSubmitReturnsDeliveryError(t *testing.T) {
	svc, fake := testService(t, nil)
	fake.err = errors.New("boom")

	_, err := svc.Submit(context.Background(), validSubmission())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotConfigured)
}

func TestSubmissionFingerprintIsStableAndContentSensitive(t *testing.T) {
	a := validSubmission()
	b := validSubmission()
	require.Equal(t, submissionFingerprint(a), submissionFingerprint(b))

	b.Message += "!"
	require.NotEqual(t, submissionFingerprint(a), submissionFingerprint(b))
}

func TestGuardRateLimitWindowExpires(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	guard := newSubmissionGuard(1, func() time.Time { return now })

	require.Zero(t, guard.reserve("user-1"))
	require.Positive(t, guard.reserve("user-1"))

	now = now.Add(time.Hour + time.Second)
	require.Zero(t, guard.reserve("user-1"))
}

// A rate-limited identity must not lose its budget to a flood of new
// identities: eviction is idle-only (issue #472).
func TestGuardFloodKeepsLiveBudget(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	guard := newSubmissionGuard(1, func() time.Time { return now })
	guard.maxEntries = 3

	require.Zero(t, guard.reserve("victim"))
	require.Positive(t, guard.reserve("victim"))

	for i := range 50 {
		guard.reserve(fmt.Sprintf("flood-%d", i))
	}

	// The victim's hit is still live, so its budget is still exhausted.
	require.Positive(t, guard.reserve("victim"), "a live hit must never be evicted")
	require.Contains(t, guard.entries, "victim")
	require.Len(t, guard.entries["victim"].hits, 1)
	// The map is allowed past maxEntries rather than refunding a budget.
	require.Greater(t, len(guard.entries), guard.maxEntries)
}

// When an idle identity is available it is the one evicted, before any
// identity that holds a live hit.
func TestGuardEvictsIdleIdentityFirst(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	guard := newSubmissionGuard(1, func() time.Time { return now })
	guard.maxEntries = 2

	require.Zero(t, guard.reserve("live"))
	guard.entries["idle"] = &guardEntry{lastSeen: now.Add(-2 * time.Hour)}
	guard.lastSweep = time.Time{}

	require.Zero(t, guard.reserve("newcomer"))

	require.NotContains(t, guard.entries, "idle", "the idle identity is evicted first")
	require.Contains(t, guard.entries, "live")
	require.Contains(t, guard.entries, "newcomer")
}

func TestFailedDeliveryConsumesTheBudgetSlot(t *testing.T) {
	svc, fake := testService(t, func(c *config.FeedbackConfig) { c.RateLimitPerHour = 1 })
	fake.err = errors.New("github is down")

	_, err := svc.Submit(context.Background(), validSubmission())
	require.Error(t, err)

	// The slot is reserved before the GitHub call and never refunded: an outage
	// must not become an unlimited retry amplifier.
	fake.err = nil
	sub := validSubmission()
	sub.Message = "a different report, same hour"
	_, err = svc.Submit(context.Background(), sub)

	var limited *RateLimitError
	require.ErrorAs(t, err, &limited)
}

func TestDuplicateRetryDoesNotConsumeTheBudgetSlot(t *testing.T) {
	svc, fake := testService(t, func(c *config.FeedbackConfig) { c.RateLimitPerHour = 1 })

	first, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)

	// The duplicate check runs before the reservation, so with a budget of one a
	// retried identical call still returns the original issue instead of a 429.
	again, err := svc.Submit(context.Background(), validSubmission())
	require.NoError(t, err)
	require.True(t, again.Duplicate)
	require.Equal(t, first.IssueNumber, again.IssueNumber)
	require.Len(t, fake.calls, 1)
}

func TestNormaliseIDKeepsOnlyUUIDs(t *testing.T) {
	const id = "3f1c1a9e-1b6e-4f2a-9f0e-2b7c4d5e6f70"
	require.Equal(t, id, normaliseID("  "+id+" "))
	require.Empty(t, normaliseID("project-1"))
	require.Empty(t, normaliseID("../../etc/passwd"))
	require.Empty(t, normaliseID(""))
}
