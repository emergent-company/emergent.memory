package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
