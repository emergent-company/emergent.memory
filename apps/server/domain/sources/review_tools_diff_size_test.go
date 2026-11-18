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

// bigPatch is a patch body several times the old 6000-byte per-file bound. A
// release promotion is one commit carrying every change since the last one, and
// the review agent refused to approve v0.103.0's promotion because every file
// above that bound was cut mid-hunk — leaving ~43% of the change unreadable and
// the gate unable to reach a verdict (#1022).
func bigPatch(lines int) string {
	var b strings.Builder
	b.WriteString("@@ -1,400 +1,400 @@\n")
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "+line %d of a rewritten file that a reviewer must be able to read in full\n", i)
	}
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

func diffText(t *testing.T, res *mcp.ToolResult) string {
	t.Helper()
	require.Len(t, res.Content, 1)
	return res.Content[0].Text
}

// patchSlice returns the patch bytes a page actually carried: everything after
// the page header and before the trailer.
func patchSlice(t *testing.T, text string) string {
	t.Helper()
	const hdr = "--- patches ["
	i := strings.Index(text, hdr)
	require.GreaterOrEqual(t, i, 0, "page header missing:\n%s", text)
	body := text[i+strings.Index(text[i:], "\n")+1:]
	for _, tail := range []string{"\n\n[page: bytes ", "\n\n[end of diff: "} {
		if j := strings.LastIndex(body, tail); j >= 0 {
			return body[:j]
		}
	}
	t.Fatalf("page trailer missing:\n%s", text)
	return ""
}

// submitVerdict posts a MERGE verdict for head through the review tool.
func submitVerdict(h *ReviewToolHandler, ctx context.Context, client *githubapp.Client, head string) error {
	_, err := h.executeReviewSubmit(ctx, client, "project-1", "owner/repo", map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
		"verdict": "MERGE", "summary": "read the diff",
	})
	return err
}

// TestPRDiffServesWholePatches pins the truncation fix: a patch three times the
// old 6000-byte bound arrives complete, the whole file list is indexed, and the
// page is marked as reaching the end of the diff.
func TestPRDiffServesWholePatches(t *testing.T) {
	head := "cccccccccccccccccccccccccccccccccccccccc"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": bigPatch(400) + "+LAST-VISIBLE-LINE\n"},
		{"filename": "apps/connector.mac/b.swift", "status": "added", "additions": 3, "deletions": 0, "patch": "@@ -0,0 +1,3 @@\n+one\n+two\n+three\n"},
		{"filename": "docs/c.md", "status": "removed", "additions": 0, "deletions": 9, "patch": "@@ -1,9 +0,0 @@\n-gone\n"},
	}
	client, _ := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	res, err := h.executePRDiff(ctx, client, "owner/repo", head, "", 0, 0)
	require.NoError(t, err)
	text := diffText(t, res)

	require.Contains(t, text, "LAST-VISIBLE-LINE", "the tail of a large patch must survive")
	require.NotContains(t, text, "patch truncated")
	require.Contains(t, text, "files: 3")
	// The index names every changed file even on a page that does not carry them.
	for _, name := range []string{"apps/server/a.go", "apps/connector.mac/b.swift", "docs/c.md"} {
		require.Contains(t, text, name)
	}
	require.Contains(t, text, "[end of diff: ")
	require.Equal(t, 0, res.StructuredContent["next_offset"])
	require.Equal(t, 3, res.StructuredContent["returned"])
	require.Equal(t, true, res.StructuredContent["evidence"])

	// A read that reached the end of the diff is evidence: the verdict is allowed.
	require.NoError(t, submitVerdict(h, ctx, client, head))
}

// TestPRDiffPagesByOffset pins the interface the review agent asked for when it
// could not hold a promotion-sized diff: read on from where the last page
// stopped. Pages must tile the patch text exactly — no gap, no repeat.
func TestPRDiffPagesByOffset(t *testing.T) {
	head := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": bigPatch(400) + "+TAIL-A\n"},
		{"filename": "apps/server/b.go", "status": "modified", "additions": 200, "deletions": 0, "patch": bigPatch(200) + "+TAIL-B\n"},
	}
	client, _ := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	offset, pages, collected := 0, 0, ""
	for {
		res, err := h.executePRDiff(ctx, client, "owner/repo", head, "", offset, 1000)
		require.NoError(t, err)
		pages++
		require.LessOrEqual(t, pages, 50, "paging must terminate")

		total := res.StructuredContent["total_bytes"].(int)
		next := res.StructuredContent["next_offset"].(int)
		require.Equal(t, offset, res.StructuredContent["offset"])
		collected += patchSlice(t, diffText(t, res))

		if next == 0 {
			require.Equal(t, total, len(collected), "pages must cover the whole patch text exactly")
			break
		}
		require.Greater(t, next, offset, "next_offset must advance")
		offset = next
	}
	require.Greater(t, pages, 1, "a diff larger than the page limit must be paged")
	// Each file appears once, in order, and each tail is readable.
	require.Equal(t, 1, strings.Count(collected, "+TAIL-A"))
	require.Equal(t, 1, strings.Count(collected, "+TAIL-B"))
	require.Less(t, strings.Index(collected, "+TAIL-A"), strings.Index(collected, "+TAIL-B"))

	// Any page of an unfiltered read is evidence, so the run may submit.
	require.NoError(t, submitVerdict(h, ctx, client, head))
}

// TestPRDiffOffsetPastEndIsValidation pins that an out-of-range offset is a
// validation error naming the real size, not an empty page the agent could
// mistake for "nothing left to read".
func TestPRDiffOffsetPastEndIsValidation(t *testing.T) {
	head := "ffffffffffffffffffffffffffffffffffffffff"
	files := []map[string]any{
		{"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@\n+x\n"},
	}
	client, _ := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	_, err := h.executePRDiff(context.Background(), client, "owner/repo", head, "", 99999, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "past the end of the diff")
}

// TestPRDiffPartialReadIsNotEvidence pins the other side of the evidence rule: a
// run that read the first page of a multi-page diff — or only the last page —
// has not read the change, and must be refused with a validation error it can
// act on (paging instructions), not an internal error.
func TestPRDiffPartialReadIsNotEvidence(t *testing.T) {
	head := "1122334455667788990011223344556677889900"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": bigPatch(400)},
		{"filename": "apps/server/b.go", "status": "modified", "additions": 400, "deletions": 0, "patch": bigPatch(400)},
	}
	client, reviews := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	first, err := h.executePRDiff(ctx, client, "owner/repo", head, "", 0, 1000)
	require.NoError(t, err)
	require.Equal(t, false, first.StructuredContent["evidence"], "one page of many is not evidence")
	total := first.StructuredContent["total_bytes"].(int)
	require.Equal(t, 1000, first.StructuredContent["next_offset"], "the page tells the agent where to read on from")

	err = submitVerdict(h, ctx, client, head)
	require.Error(t, err)
	require.Contains(t, err.Error(), "offset=", "the refusal must tell the agent how to finish reading")
	require.Equal(t, 0, *reviews)

	// Jumping to the last page must not unlock the verdict either.
	last, err := h.executePRDiff(ctx, client, "owner/repo", head, "", total-500, 1000)
	require.NoError(t, err)
	require.Equal(t, 0, last.StructuredContent["next_offset"])
	require.Equal(t, false, last.StructuredContent["evidence"], "the tail of a diff is not the diff")
	require.Error(t, submitVerdict(h, ctx, client, head))
	require.Equal(t, 0, *reviews)

	// Reading every page in order does unlock it.
	for offset := 0; ; {
		res, err := h.executePRDiff(ctx, client, "owner/repo", head, "", offset, 1000)
		require.NoError(t, err)
		next := res.StructuredContent["next_offset"].(int)
		if next == 0 {
			break
		}
		require.Greater(t, next, offset)
		offset = next
	}
	require.NoError(t, submitVerdict(h, ctx, client, head))
	require.Equal(t, 1, *reviews)
}

// TestPRDiffPathFilterNarrowsWithoutEvidence pins the other half: an agent that
// narrows to one path gets that path in full, with only that path indexed, and a
// narrowed read is not evidence of having read the change it approves.
func TestPRDiffPathFilterNarrowsWithoutEvidence(t *testing.T) {
	head := "dddddddddddddddddddddddddddddddddddddddd"
	files := []map[string]any{
		{"filename": "apps/server/a.go", "status": "modified", "additions": 400, "deletions": 0, "patch": bigPatch(400)},
		{"filename": "apps/web-ui/pages/b.templ", "status": "modified", "additions": 2, "deletions": 1, "patch": "@@ -1 +1 @@\n+WEBUI-ONLY\n"},
	}
	client, reviews := diffServer(t, head, files)
	h := &ReviewToolHandler{}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())

	res, err := h.executePRDiff(ctx, client, "owner/repo", head, "web-ui/", 0, 0)
	require.NoError(t, err)
	text := diffText(t, res)
	require.Contains(t, text, "files: 1 of 2")
	require.Contains(t, text, "WEBUI-ONLY")
	require.NotContains(t, text, "apps/server/a.go")
	require.Equal(t, false, res.StructuredContent["evidence"])
	require.Equal(t, "web-ui/", res.StructuredContent["path"])

	// A filtered read must not be enough to approve the PR.
	err = submitVerdict(h, ctx, client, head)
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	require.Equal(t, 0, *reviews)

	// Reading the unfiltered diff afterwards unlocks the same run.
	res, err = h.executePRDiff(ctx, client, "owner/repo", head, "", 0, 0)
	require.NoError(t, err)
	require.Equal(t, true, res.StructuredContent["evidence"])
	require.NoError(t, submitVerdict(h, ctx, client, head))
	require.Equal(t, 1, *reviews)
}
