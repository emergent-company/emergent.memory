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
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestReviewSubmitRequiresDiffInSameRun pins BLOCKING 2's cheap mitigation: a
// verdict may only be posted for a commit whose non-empty diff this run read.
// It is DB-free: the client is pointed at an httptest server and the methods
// under test never touch a repository.
func TestReviewSubmitRequiresDiffInSameRun(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	files := []map[string]any{{
		"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@",
	}}
	reviews, checks, comments := 0, 0, 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "state": "open",
				"head": map[string]any{"sha": head, "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/commits/"):
			sha := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/commits/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sha": sha, "commit": map[string]any{"message": "fix"}, "files": files,
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7/reviews":
			reviews++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/repos/owner/repo/check-runs":
			checks++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		case r.URL.Path == "/repos/owner/repo/issues/7/comments":
			comments++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := githubapp.NewClient(srv.URL, "ghs_test", srv.Client())
	h := &ReviewToolHandler{}
	args := func() map[string]any {
		return map[string]any{
			"repo": "owner/repo", "number": float64(7), "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"verdict": "MERGE", "summary": "looks good",
		}
	}

	// No run cache at all: no evidence can exist, so the submit fails closed.
	_, err := h.executeReviewSubmit(context.Background(), client, "owner/repo", args())
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")

	// A run that read nothing is refused.
	ctx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err = h.executeReviewSubmit(ctx, client, "owner/repo", args())
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	require.Equal(t, 0, reviews, "no review may be posted without reading the diff")

	// Reading the exact commit records evidence; the same run may now submit.
	_, err = h.executePRDiff(ctx, client, "owner/repo", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, "owner/repo", args())
	require.NoError(t, err)
	require.Equal(t, 1, reviews)
	require.Equal(t, 1, checks)
	require.Equal(t, 1, comments)

	// An empty diff is not reviewable: it records no evidence, so a submit for
	// that commit is refused even in a run that successfully called the diff.
	head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	files = nil
	emptyCtx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err = h.executePRDiff(emptyCtx, client, "owner/repo", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(emptyCtx, client, "owner/repo", map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"verdict": "MERGE", "summary": "approved without reading anything",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	require.Equal(t, 1, reviews)

	// A branch name is not a commit: the diff tool refuses it before any request.
	_, err = h.executePRDiff(context.Background(), client, "owner/repo", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a commit SHA")
}
