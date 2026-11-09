package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/feedback"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestFeedbackSubmitToolFilesIssue proves the catalog tool is not merely
// discoverable: ExecuteTool routes it to the feedback domain, the authenticated
// caller's identity reaches the issue, and the result carries the issue URL.
func TestFeedbackSubmitToolFilesIssue(t *testing.T) {
	db := connectTestDB(t)
	var issueBody string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		issueBody = payload.Body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 77, "html_url": "https://github.com/o/r/issues/77"}`))
	}))
	defer github.Close()

	fb := feedback.NewService(&config.Config{
		Environment: "test",
		Feedback: config.FeedbackConfig{
			Enabled:          true,
			GitHubToken:      "test-token",
			Repo:             "o/r",
			APIBaseURL:       github.URL,
			Label:            "feedback",
			RateLimitPerHour: 5,
		},
	}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	svc := &Service{feedbackSvc: fb}
	_ = svc.GetToolDefinitions() // populate the tool index the authority gate reads

	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{
		ID: "user-1", Email: "ada@example.com", OrgID: "org-1",
	})
	result, err := svc.ExecuteTool(ctx, "project-1", "feedback-submit", map[string]any{
		"message":  "search returned stale results",
		"category": "bug",
	})
	require.NoError(t, err)

	require.Contains(t, issueBody, "ada@example.com")
	require.Contains(t, issueBody, "search returned stale results")
	require.NotNil(t, result.StructuredContent)
	require.Equal(t, float64(77), result.StructuredContent["issue_number"])
	require.Equal(t, "https://github.com/o/r/issues/77", result.StructuredContent["issue_url"])
}

func TestFeedbackSubmitToolFailsClosedWithoutService(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()

	_, err := svc.ExecuteTool(context.Background(), "project-1", "feedback-submit", map[string]any{"message": "hi"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enabled")
}

// newFeedbackToolService builds a bare Service wired to a real feedback service
// (and database) plus a GitHub stub that records bodies.
func newFeedbackToolService(t *testing.T, githubURL string) (*Service, *feedback.Service) {
	t.Helper()
	db := connectTestDB(t)
	fb := feedback.NewService(&config.Config{
		Environment: "test",
		Feedback: config.FeedbackConfig{
			Enabled:          true,
			GitHubToken:      "test-token",
			Repo:             "o/r",
			APIBaseURL:       githubURL,
			Label:            "feedback",
			RateLimitPerHour: 10,
		},
	}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := &Service{feedbackSvc: fb}
	_ = svc.GetToolDefinitions()
	return svc, fb
}

// TestFeedbackSubmitToolUsesClientNameFromContext proves the tools/call context
// value reaches the issue's Client row.
func TestFeedbackSubmitToolUsesClientNameFromContext(t *testing.T) {
	var body string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body = payload.Body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 77, "html_url": "https://github.com/o/r/issues/77"}`))
	}))
	defer github.Close()

	svc, _ := newFeedbackToolService(t, github.URL)
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "user-1", Email: "ada@example.com"})
	ctx = withClientName(ctx, "claude-code")

	_, err := svc.ExecuteTool(ctx, "project-1", "feedback-submit", map[string]any{"message": "client name plumbing"})
	require.NoError(t, err)
	require.Contains(t, body, "claude-code")
	require.Contains(t, body, "| Client | claude-code |")
}

// TestFeedbackSubmitToolFallsBackToMCP covers a transport with no session (the
// per-agent endpoint): the documented fallback is the constant "MCP".
func TestFeedbackSubmitToolFallsBackToMCP(t *testing.T) {
	var body string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body = payload.Body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 78, "html_url": "https://github.com/o/r/issues/78"}`))
	}))
	defer github.Close()

	svc, _ := newFeedbackToolService(t, github.URL)
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "user-1", Email: "ada@example.com"})

	_, err := svc.ExecuteTool(ctx, "project-1", "feedback-submit", map[string]any{"message": "no session client name"})
	require.NoError(t, err)
	require.Contains(t, body, "| Client | MCP |")
}

// TestFeedbackSubmitToolReportsInProgress proves the loser of a concurrent
// duplicate gets a defined retryable answer, not a tool failure or a second
// issue, and that it agrees with the REST 409.
func TestFeedbackSubmitToolReportsInProgress(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 79, "html_url": "https://github.com/o/r/issues/79"}`))
	}))
	defer github.Close()

	svc, _ := newFeedbackToolService(t, github.URL)
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "user-1", Email: "ada@example.com"})
	args := map[string]any{"message": "in progress case"}

	type toolOutcome struct {
		result *ToolResult
		err    error
	}
	done := make(chan toolOutcome, 1)
	go func() {
		result, err := svc.ExecuteTool(ctx, "project-1", "feedback-submit", args)
		done <- toolOutcome{result: result, err: err}
	}()
	<-started

	inProgress, err := svc.ExecuteTool(ctx, "project-1", "feedback-submit", args)
	require.NoError(t, err)
	require.NotNil(t, inProgress)
	require.False(t, inProgress.IsError, "in progress is a defined answer, not a tool failure")
	require.Equal(t, "in_progress", inProgress.StructuredContent["status"])
	require.Greater(t, inProgress.StructuredContent["retry_after_seconds"], float64(0))

	close(release)
	first := <-done
	require.NoError(t, first.err)
	require.NotNil(t, first.result)
	require.EqualValues(t, 1, calls.Load(), "the loser must not create a second issue")
}
