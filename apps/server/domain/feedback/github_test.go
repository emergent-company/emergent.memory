package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNewGitHubClientRejectsRepoThatWouldChangeThePath(t *testing.T) {
	for _, repo := range []string{"o/r/extra", "o/..", "../../etc", "o/r?x=1", "o/r#f", "o/r sp", "/r", "o/"} {
		t.Run(repo, func(t *testing.T) {
			_, err := newGitHubClient(config.FeedbackConfig{GitHubToken: "t", Repo: repo})
			require.Error(t, err)
		})
	}
}

func TestGitHubClientDecodesResponseCarryingTheEchoedIssueBody(t *testing.T) {
	// GitHub answers a create with the whole issue object, echoing the body it
	// just stored. With the 16 KiB message cap that response is far larger than
	// the error-text bound; decoding it through that bound failed *after* the
	// issue had been created, which surfaced as a 502 on a report that actually
	// landed — and the caller's retry then filed a duplicate.
	message := strings.Repeat("detail ", 2048) // ~14 KiB, well past 4 KiB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"number":   21,
			"html_url": "https://github.com/o/r/issues/21",
			"title":    "t",
			"body":     message,
		}))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	issue, err := client.CreateIssue(context.Background(), "title", message, nil)
	require.NoError(t, err)
	require.Equal(t, 21, issue.Number)
}

func TestGitHubClientBoundsTheErrorText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 64<<10)))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	_, err = client.CreateIssue(context.Background(), "title", "body", nil)
	require.Error(t, err)
	require.Less(t, len(err.Error()), maxGitHubErrorBytes)
	require.Contains(t, err.Error(), "500")
}

func TestGitHubClientRejectsAnOversizedSuccessBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":1,"html_url":"https://github.com/o/r/issues/1","body":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxGitHubResponseBytes)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	client, err := newGitHubClient(config.FeedbackConfig{
		GitHubToken: "secret-token",
		Repo:        "o/r",
		APIBaseURL:  srv.URL,
	})
	require.NoError(t, err)

	_, err = client.CreateIssue(context.Background(), "title", "body", nil)
	require.ErrorContains(t, err, "exceeds")
}
