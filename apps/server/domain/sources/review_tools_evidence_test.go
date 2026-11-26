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
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestReviewSubmitRequiresPullRequestDiffInSameRun pins the evidence rule: a
// verdict may only be posted for a pull request head whose non-empty pull
// request diff this run read.
//
// The key regression is the merge-commit case (#1022): its own commit diff is
// empty (the tree equals the first parent), so keying evidence on the commit
// left the gate unapprovable. The pull request diff is non-empty and is what
// must grant evidence.
func TestReviewSubmitRequiresPullRequestDiffInSameRun(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	prFiles := []map[string]any{{
		"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@",
	}}
	commitFiles := []map[string]any{{
		"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@",
	}}
	reviews, checks, comments := 0, 0, 0
	var checkBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open",
				"head": map[string]any{"sha": head, "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7/files":
			_ = json.NewEncoder(w).Encode(prFiles)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/commits/"):
			sha := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/commits/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sha": sha, "commit": map[string]any{"message": "fix"}, "files": commitFiles,
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7/reviews":
			reviews++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/repos/owner/repo/check-runs":
			checks++
			_ = json.NewDecoder(r.Body).Decode(&checkBody)
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
	diffArgs := func(sha string) map[string]any {
		return map[string]any{"repo": "owner/repo", "number": float64(7), "head_sha": sha}
	}
	submitArgs := func(sha, verdict, summary string) map[string]any {
		return map[string]any{
			"repo": "owner/repo", "number": float64(7), "head_sha": sha,
			"verdict": verdict, "summary": summary,
		}
	}

	// No run cache at all: no evidence can exist, so the submit fails closed.
	_, err := h.executeReviewSubmit(context.Background(), client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "looks good"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")

	// A run that read nothing is refused.
	ctx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "looks good"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "a refused submit must be a caller-visible validation error, not a 500")
	require.Equal(t, apperror.ErrValidation.Code, appErr.Code)
	require.Equal(t, 0, reviews, "no review may be posted without reading the diff")

	// Reading the pull request diff records evidence; the same run may submit.
	_, err = h.executePRDiff(ctx, client, "project-1", "owner/repo", nil, diffArgs(head))
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "looks good"))
	require.NoError(t, err)
	require.Equal(t, 1, reviews)
	require.Equal(t, 1, checks)
	require.Equal(t, 1, comments)

	// Regression for #1022: a merge commit whose first-parent diff is empty
	// still has a non-empty pull request diff, so the head is reviewable and
	// the gate must be approvable.
	head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	commitFiles = nil
	mergeCtx := mcp.ContextWithNewToolResultCache(context.Background())
	res, err := h.executePRDiff(mergeCtx, client, "project-1", "owner/repo", nil, diffArgs(head))
	require.NoError(t, err, "an empty first-parent commit diff must not make the pull request unreviewable")
	require.Contains(t, res.Content[0].Text, "a.go")
	_, err = h.executeReviewSubmit(mergeCtx, client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "merge commit reviewed"))
	require.NoError(t, err)
	require.Equal(t, 2, reviews)

	// A commit-only diff is context, not evidence: even a non-empty commit diff
	// cannot justify a verdict, because the reviewed object is the pull request.
	head = "cccccccccccccccccccccccccccccccccccccccc"
	commitFiles = []map[string]any{{"filename": "ctx.go", "status": "modified", "patch": "@@ -1 +1 @@"}}
	commitOnlyCtx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err = h.executePRDiff(commitOnlyCtx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "head_sha": head,
	})
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(commitOnlyCtx, client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "commit diff only"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	require.Equal(t, 2, reviews, "a commit-only diff must not authorize a verdict")

	// A genuinely empty pull request is not reviewable. It returns a validation
	// error (never a 500) and the failure is terminal: a check is published.
	head = "dddddddddddddddddddddddddddddddddddddddd"
	prFiles = nil
	commitFiles = []map[string]any{{"filename": "ignored.go", "status": "modified", "patch": "@@ -1 +1 @@"}}
	emptyCtx := mcp.ContextWithNewToolResultCache(context.Background())
	checksBefore := checks
	_, err = h.executePRDiff(emptyCtx, client, "project-1", "owner/repo", nil, diffArgs(head))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no changed files")
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.ErrValidation.Code, appErr.Code)
	require.Equal(t, checksBefore+1, checks, "an unapprovable head must publish a terminal reviewed check")
	require.Contains(t, checkBody["output"].(map[string]any)["title"], "unapprovable")

	// The submit path agrees, even if the agent skipped the diff call.
	_, err = h.executeReviewSubmit(emptyCtx, client, "project-1", "owner/repo", nil, submitArgs(head, "MERGE", "approved without reading anything"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no changed files")
	require.Equal(t, 2, reviews)

	// An empty commit diff (no pull request number) is also a validation error
	// rather than a successful empty tool result.
	commitFiles = nil
	_, err = h.executePRDiff(context.Background(), client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "head_sha": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty diff")

	// A branch name is not a commit: the diff tool refuses it before any request.
	_, err = h.executePRDiff(context.Background(), client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "head_sha": "main",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a commit SHA")
}

type fakeLaneWriter struct {
	status string
	actor  string
}

func (f *fakeLaneWriter) UpdateWorkObjectStatus(ctx context.Context, projectID, canonicalID, status, reviewStatus string) (bool, error) {
	f.status = status
	f.actor, _, _ = auth.ActorFromContext(ctx)
	return true, nil
}

func TestReviewSubmitFinalizesWorkLane(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "open", "head": map[string]any{"sha": head}, "base": map[string]any{"ref": "dev"}})
		case "/repos/owner/repo/pulls/7/files":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "a.go", "patch": "@@ -1 +1 @@"}})
		case "/repos/owner/repo/pulls/7/reviews", "/repos/owner/repo/check-runs", "/repos/owner/repo/issues/7/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := githubapp.NewClient(srv.URL, "ghs_test", srv.Client())
	writer := &fakeLaneWriter{}
	h := &ReviewToolHandler{workItems: writer}
	ctx := mcp.ContextWithWorkObjectID(mcp.ContextWithNewToolResultCache(context.Background()), "lane-1")
	_, err := h.executePRDiff(ctx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
	})
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head, "verdict": "CHANGES_REQUESTED", "summary": "fix it",
	})
	require.NoError(t, err)
	require.Equal(t, "revision", writer.status)
	require.Equal(t, graph.ActorSystem, writer.actor)
}

// TestPRDiffDerivesPullRequestNumberFromRunSubject pins the lane-run path: a
// review lane passes only repo and head_sha (the blueprint's historical
// instruction), so the pull request number must come from the run's verified
// subject and evidence must key on repo#number@sha. Without this the fix would
// only work for agents that happened to pass a number (#1022).
func TestPRDiffDerivesPullRequestNumberFromRunSubject(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitCalls, fileCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open",
				"head": map[string]any{"sha": head}, "base": map[string]any{"ref": "dev"},
			})
		case "/repos/owner/repo/pulls/7/files":
			fileCalls++
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "a.go", "patch": "@@ -1 +1 @@"}})
		case "/repos/owner/repo/commits/" + head:
			commitCalls++
			// The merge commit's first-parent diff is empty; a commit-keyed
			// implementation would record no evidence here.
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": head, "commit": map[string]any{"message": "merge"}, "files": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := githubapp.NewClient(srv.URL, "ghs_test", srv.Client())
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithRunSubject(
		mcp.ContextWithNewToolResultCache(context.Background()),
		mcp.RunSubject{SourceID: "src-1", Repo: "owner/repo", PRNumber: 7, HeadSHA: head},
	)
	_, err := h.executePRDiff(ctx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "head_sha": head,
	})
	require.NoError(t, err)
	require.Equal(t, 1, fileCalls)
	require.Zero(t, commitCalls, "the pull request diff must be read instead of the commit's first-parent diff")
	require.True(t, mcp.RunHasEvidence(ctx, diffEvidenceKey("owner/repo", 7, head)),
		"evidence must key on repo#number@sha so the submit can find it")
}
