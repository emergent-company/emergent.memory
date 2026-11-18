package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// largePatch is a patch body well past the old 6000-byte per-file bound. A
// release promotion is one commit carrying every change since the last one, and
// the review agent refused to approve v0.103.0's promotion because patches above
// that bound were cut — which left ~43% of the change unreadable and the gate
// unable to reach a verdict (#1022).
func largePatch() string {
	var b strings.Builder
	b.WriteString("@@ -1,400 +1,400 @@\n")
	for i := 0; b.Len() < 20000; i++ {
		fmt.Fprintf(&b, "+line %d of a rewritten file that a reviewer must be able to read in full\n", i)
	}
	b.WriteString("+LAST-VISIBLE-LINE\n")
	return b.String()
}

func diffServer(t *testing.T, head string, files []map[string]any) (*githubapp.Client, *int) {
	t.Helper()
	reviews := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "state": "open",
				"head": map[string]any{"sha": head, "ref": "feat/x"},
				"base": map[string]any{"ref": "main"},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sha":    strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/commits/"),
				"commit": map[string]any{"message": "promote"}, "files": files,
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7/reviews":
			reviews++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/repos/owner/repo/check-runs", r.URL.Path == "/repos/owner/repo/issues/7/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return githubapp.NewClient(srv.URL, "ghs_test", srv.Client()), &reviews
}

// TestPRDiffServesFullPatches pins that a file larger than the historical
// 6000-byte per-file bound arrives complete, and that the coverage line tells
// the agent the read was complete rather than leaving it to guess.
func TestPRDiffServesFullPatches(t *testing.T) {
	head := "cccccccccccccccccccccccccccccccccccccccc"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": largePatch()},
		{"filename": "apps/server/b.go", "status": "added", "additions": 3, "deletions": 0, "patch": "@@ -0,0 +1,3 @@"},
		{"filename": "apps/server/c.go", "status": "removed", "additions": 0, "deletions": 9, "patch": "@@ -1,9 +0,0 @@"},
	}
	client, _ := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	res, err := h.executePRDiff(ctx, client, "owner/repo", head, "")
	require.NoError(t, err)
	require.Len(t, res.Content, 1)
	text := res.Content[0].Text
	require.Contains(t, text, "LAST-VISIBLE-LINE", "the tail of a large patch must survive")
	require.NotContains(t, text, "patch truncated")
	require.Contains(t, text, "files: 3 of 3")
	require.Contains(t, text, "coverage: complete")
	require.Equal(t, "complete", res.StructuredContent["coverage"])
	require.Equal(t, 3, res.StructuredContent["returned"])
	require.Equal(t, true, res.StructuredContent["evidence"])

	// The unfiltered read is review evidence: a verdict for this head is allowed.
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
		"verdict": "MERGE", "summary": "read the whole diff",
	})
	require.NoError(t, err)
}

// TestPRDiffPathFilterNarrowsWithoutEvidence pins the other half: an agent that
// narrows to one path gets that path in full, but a narrowed read is not
// evidence of having read the change it approves.
func TestPRDiffPathFilterNarrowsWithoutEvidence(t *testing.T) {
	head := "dddddddddddddddddddddddddddddddddddddddd"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": largePatch()},
		{"filename": "apps/web-ui/pages/b.templ", "status": "modified", "additions": 2, "deletions": 1, "patch": "@@ -1 +1 @@"},
	}
	client, reviews := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	res, err := h.executePRDiff(ctx, client, "owner/repo", head, "web-ui/")
	require.NoError(t, err)
	text := res.Content[0].Text
	require.Contains(t, text, "files: 1 of 2")
	require.Contains(t, text, "b.templ")
	require.NotContains(t, text, "a.go")
	require.Equal(t, false, res.StructuredContent["evidence"])
	require.Equal(t, "web-ui/", res.StructuredContent["path"])

	// A filtered read must not be enough to approve the PR.
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
		"verdict": "MERGE", "summary": "read one file of two",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	require.Equal(t, 0, *reviews)

	// Reading the unfiltered diff afterwards unlocks the same run.
	res, err = h.executePRDiff(ctx, client, "owner/repo", head, "")
	require.NoError(t, err)
	require.Equal(t, true, res.StructuredContent["evidence"])
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
		"verdict": "MERGE", "summary": "read the whole diff",
	})
	require.NoError(t, err)
	require.Equal(t, 1, *reviews)
}
