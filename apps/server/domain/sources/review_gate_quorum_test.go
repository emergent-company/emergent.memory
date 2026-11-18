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
			name:        "missing author metadata fails closed",
			subjectType: "GitHubPullRequest",
			properties:  nil,
			want:        ReviewGateMinReviewersSourceAuthored,
		},
		{
			name:        "empty author metadata fails closed",
			subjectType: "GitHubPullRequest",
			properties:  map[string]any{"author": "", "author_type": ""},
			want:        ReviewGateMinReviewersSourceAuthored,
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

func TestReviewGateAuthorUnknown(t *testing.T) {
	require.True(t, reviewGateAuthorUnknown("GitHubPullRequest", nil))
	require.True(t, reviewGateAuthorUnknown("GitHubPullRequest", map[string]any{"author": " ", "author_type": ""}))
	require.False(t, reviewGateAuthorUnknown("GitHubPullRequest", map[string]any{"author": "mkucharz", "author_type": "User"}))
	require.False(t, reviewGateAuthorUnknown("GitHubPullRequest", map[string]any{"author": "memory-coding[bot]", "author_type": "Bot"}))
	require.False(t, reviewGateAuthorUnknown("GitHubIssue", nil), "only pull-request subjects use the bot-only quorum")
}

// TestReviewGateMinimumReviewersCappedByConfigured pins the deadlock fix: the
// stored policy quorum (2 for a source-authored subject) is capped at the number
// of configured reviewer agents, so a one-reviewer project is satisfiable while a
// two-reviewer one still needs both.
func TestReviewGateMinimumReviewersCappedByConfigured(t *testing.T) {
	one := []ReviewReviewer{{AgentID: "a", Name: "review"}}
	two := []ReviewReviewer{{AgentID: "a", Name: "review"}, {AgentID: "b", Name: "security"}}

	require.Equal(t, 1, (&ReviewGate{MinReviewers: 2, RequiredReviewers: one}).minimumReviewers(),
		"a quorum of two with one configured reviewer would stall forever")
	require.Equal(t, 2, (&ReviewGate{MinReviewers: 2, RequiredReviewers: two}).minimumReviewers(),
		"two configured reviewers keep the two-reviewer rule")
	require.Equal(t, 1, (&ReviewGate{MinReviewers: 1, RequiredReviewers: one}).minimumReviewers())
	require.Equal(t, 2, (&ReviewGate{MinReviewers: 2}).minimumReviewers(),
		"no configured reviewer keeps the stored minimum so the gate stays visibly under-configured")
	require.Equal(t, ReviewGateMinReviewersDefault, (*ReviewGate)(nil).minimumReviewers())
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

// TestPublishReviewGateGreensASatisfiedQuorum covers the provider-check
// boundary. The effective quorum is capped at the configured reviewer count, so
// a single-reviewer project publishes success once that reviewer approved; a
// two-reviewer project keeps the two-reviewer rule (its status only reaches
// approved through reviewGateStatus).
func TestPublishReviewGateGreensASatisfiedQuorum(t *testing.T) {
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

	t.Run("single reviewer is satisfiable", func(t *testing.T) {
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
		require.Equal(t, "completed", body["status"])
		require.Equal(t, "success", body["conclusion"])
	})

	t.Run("two reviewers satisfied", func(t *testing.T) {
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
