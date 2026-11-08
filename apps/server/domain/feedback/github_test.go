package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func TestGitHubClientCreatesIssue(t *testing.T) {
	var got struct {
		method string
		path   string
		auth   string
		body   map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got.body))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 12, "html_url": "https://github.com/o/r/issues/12"}`))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	issue, err := client.CreateIssue(context.Background(), "title", "body", []string{"feedback"})
	require.NoError(t, err)
	require.Equal(t, 12, issue.Number)
	require.Equal(t, "https://github.com/o/r/issues/12", issue.HTMLURL)

	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/repos/o/r/issues", got.path)
	require.Equal(t, "Bearer secret-token", got.auth)
	require.Equal(t, "title", got.body["title"])
	require.Equal(t, "body", got.body["body"])
	require.Equal(t, []any{"feedback"}, got.body["labels"])
}

func TestGitHubClientRetriesWithoutLabelsOn422(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if _, hasLabels := body["labels"]; hasLabels {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 13, "html_url": "https://github.com/o/r/issues/13"}`))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	issue, err := client.CreateIssue(context.Background(), "title", "body", []string{"missing-label"})
	require.NoError(t, err)
	require.Equal(t, 13, issue.Number)
	require.Equal(t, 2, requests)
}

func TestGitHubClientReportsAPIErrorWithoutLeakingToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "super-secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	_, err = client.CreateIssue(context.Background(), "title", "body", nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "super-secret-token")
	require.Contains(t, err.Error(), "403")
}

func TestNewGitHubClientRejectsInvalidConfig(t *testing.T) {
	_, err := newGitHubClient(config.FeedbackConfig{GitHubToken: "t", Repo: "no-slash"})
	require.Error(t, err)

	_, err = newGitHubClient(config.FeedbackConfig{GitHubToken: "t", Repo: "o/r", APIBaseURL: "ftp://example.com"})
	require.Error(t, err)

	_, err = newGitHubClient(config.FeedbackConfig{Repo: "o/r"})
	require.Error(t, err)
}
