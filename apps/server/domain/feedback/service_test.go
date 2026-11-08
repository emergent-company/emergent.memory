package feedback

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

type fakeIssueCreator struct {
	calls []fakeIssueCall
	issue *createdIssue
	err   error
}

type fakeIssueCall struct {
	title  string
	body   string
	labels []string
}

func (f *fakeIssueCreator) CreateIssue(_ context.Context, title, body string, labels []string) (*createdIssue, error) {
	f.calls = append(f.calls, fakeIssueCall{title: title, body: body, labels: labels})
	if f.err != nil {
		return nil, f.err
	}
	if f.issue != nil {
		return f.issue, nil
	}
	return &createdIssue{Number: 42, HTMLURL: "https://github.com/o/r/issues/42"}, nil
}

func testService(t *testing.T, mutate func(*config.FeedbackConfig)) (*Service, *fakeIssueCreator) {
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
	svc := NewService(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fake := &fakeIssueCreator{}
	if svc.Enabled() {
		svc.client = fake
	}
	return svc, fake
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
	guard := newSubmissionGuard(1, dedupeWindow, func() time.Time { return now })

	require.Zero(t, guard.reserve("user-1"))
	require.Positive(t, guard.reserve("user-1"))

	now = now.Add(time.Hour + time.Second)
	require.Zero(t, guard.reserve("user-1"))
}

func TestGuardDuplicateExpires(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	guard := newSubmissionGuard(5, dedupeWindow, func() time.Time { return now })

	require.Nil(t, guard.lookupDuplicate("user-1", "fp"))
	guard.rememberResult("user-1", "fp", &Result{IssueNumber: 7, IssueURL: "url"})
	dup := guard.lookupDuplicate("user-1", "fp")
	require.NotNil(t, dup)
	require.Equal(t, 7, dup.IssueNumber)

	now = now.Add(dedupeWindow + time.Second)
	require.Nil(t, guard.lookupDuplicate("user-1", "fp"))
}
