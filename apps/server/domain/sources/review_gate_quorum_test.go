package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
)

// TestReviewGateMinReviewersSourceAuthored is the regression guard for #1005: a
// pull request authored by a source-trigger run needs two reviewer agents, so a
// single reviewing agent cannot satisfy "all reviewers approved" on its own.
func TestReviewGateMinReviewersSourceAuthored(t *testing.T) {
	cases := []struct {
		name        string
		subjectType string
		properties  map[string]any
		want        int
	}{
		{
			name:        "bot author type",
			subjectType: "GitHubPullRequest",
			properties:  map[string]any{"author": "memory-coding-memory-coding[bot]", "author_type": "Bot"},
			want:        ReviewGateMinReviewersSourceAuthored,
		},
		{
			name:        "bot login without a type",
			subjectType: "GitHubPullRequest",
			properties:  map[string]any{"author": "memory-coding-memory-coding[bot]"},
			want:        ReviewGateMinReviewersSourceAuthored,
		},
		{
			name:        "human author",
			subjectType: "GitHubPullRequest",
			properties:  map[string]any{"author": "mkucharz", "author_type": "User"},
			want:        ReviewGateMinReviewersDefault,
		},
		{
			name:        "missing author metadata defaults to one",
			subjectType: "GitHubPullRequest",
			properties:  nil,
			want:        ReviewGateMinReviewersDefault,
		},
		{
			name:        "other subject types keep the default",
			subjectType: "GitHubIssue",
			properties:  map[string]any{"author": "memory-coding[bot]", "author_type": "Bot"},
			want:        ReviewGateMinReviewersDefault,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, reviewGateMinReviewers(tc.subjectType, tc.properties))
		})
	}
}

func TestReviewGateStatusRequiresQuorum(t *testing.T) {
	reviewer := func(id string) ReviewReviewer { return ReviewReviewer{AgentID: id, Name: id} }
	one := []ReviewReviewer{reviewer("a")}
	two := []ReviewReviewer{reviewer("a"), reviewer("b")}

	require.Equal(t, ReviewGatePending, reviewGateStatus(one, ReviewGateMinReviewersSourceAuthored, map[string]string{
		"a": "MERGE",
	}), "a single agent must not approve a source-authored subject")

	require.Equal(t, ReviewGatePending, reviewGateStatus(two, ReviewGateMinReviewersSourceAuthored, map[string]string{
		"a": "MERGE",
	}), "one of two required reviewers is not all reviewers")

	require.Equal(t, ReviewGateApproved, reviewGateStatus(two, ReviewGateMinReviewersSourceAuthored, map[string]string{
		"a": "MERGE", "b": "MERGE",
	}))

	require.Equal(t, ReviewGateChangesRequested, reviewGateStatus(two, ReviewGateMinReviewersSourceAuthored, map[string]string{
		"a": "MERGE", "b": "CHANGES_REQUESTED",
	}), "a rejection outranks approvals")

	require.Equal(t, ReviewGateChangesRequested, reviewGateStatus(one, ReviewGateMinReviewersSourceAuthored, map[string]string{
		"a": "CHANGES_REQUESTED",
	}), "a rejection must publish red even when the quorum is unmet")

	require.Equal(t, ReviewGateApproved, reviewGateStatus(one, 0, map[string]string{
		"a": "MERGE",
	}), "a zero minimum keeps the legacy single-reviewer rule")

	require.Equal(t, ReviewGatePending, reviewGateStatus(nil, ReviewGateMinReviewersDefault, nil),
		"no reviewer configured cannot approve")
}

// TestPublishReviewGateNeverGreensUnderQuorum covers the provider-check
// boundary: an approved status for an under-quorum gate must still publish a
// pending check, never success (#1005).
func TestPublishReviewGateNeverGreensUnderQuorum(t *testing.T) {
	newServer := func(body *map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/repos/acme/repo/check-runs", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(body))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		}))
	}

	head := strings.Repeat("a", 40)
	handler := &ReviewToolHandler{}

	t.Run("under quorum stays pending", func(t *testing.T) {
		var body map[string]any
		srv := newServer(&body)
		defer srv.Close()

		gate := &ReviewGate{
			Status:            ReviewGateApproved,
			RequiredReviewers: []ReviewReviewer{{AgentID: "a", Name: "security"}},
			MinReviewers:      ReviewGateMinReviewersSourceAuthored,
		}
		err := handler.publishReviewGate(context.Background(), githubapp.NewClient(srv.URL, "t", srv.Client()), "acme/repo", head, gate, "looks good")
		require.NoError(t, err)
		require.Equal(t, "in_progress", body["status"])
		require.NotContains(t, body, "conclusion")
		require.Contains(t, body["output"].(map[string]any)["summary"], "2 independent reviewers")
	})

	t.Run("satisfied quorum goes green", func(t *testing.T) {
		var body map[string]any
		srv := newServer(&body)
		defer srv.Close()

		gate := &ReviewGate{
			Status: ReviewGateApproved,
			RequiredReviewers: []ReviewReviewer{
				{AgentID: "a", Name: "security"},
				{AgentID: "b", Name: "ui"},
			},
			MinReviewers: ReviewGateMinReviewersSourceAuthored,
		}
		err := handler.publishReviewGate(context.Background(), githubapp.NewClient(srv.URL, "t", srv.Client()), "acme/repo", head, gate, "looks good")
		require.NoError(t, err)
		require.Equal(t, "completed", body["status"])
		require.Equal(t, "success", body["conclusion"])
	})
}
