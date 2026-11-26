package sources

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

type reviewLinkReader struct {
	link *SourceIssuePRLink
}

func (r reviewLinkReader) FindSourceIssuePRLink(context.Context, uuid.UUID, string, int) (*SourceIssuePRLink, error) {
	return r.link, nil
}

type reviewReworkRecorder struct {
	called      bool
	canonicalID string
	text        string
}

func (r *reviewReworkRecorder) RequestWorkItemChanges(_ context.Context, _, canonicalID, _, text string) error {
	r.called = true
	r.canonicalID = canonicalID
	r.text = text
	return nil
}

type reviewStatusRecorder struct {
	status string
}

func (r *reviewStatusRecorder) UpdateWorkObjectStatus(_ context.Context, _, _, status, _ string) (bool, error) {
	r.status = status
	return true, nil
}

func TestReviewSubmitRequestsReworkForLinkedIssue(t *testing.T) {
	api := newReviewToolsAPI(t)
	client := githubapp.NewClient(api.url, "ghs_test", api.server.Client())
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	issueID := uuid.New()
	reworker := &reviewReworkRecorder{}
	h := &ReviewToolHandler{
		links:     reviewLinkReader{link: &SourceIssuePRLink{IssueCanonicalID: issueID}},
		workItems: &reviewStatusRecorder{},
		reworker:  reworker,
	}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err := h.executePRDiff(ctx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
	})
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, uuid.NewString(), "owner/repo", nil, reviewSubmitArgs(head, "CHANGES_REQUESTED", "fix the bug"))
	require.NoError(t, err)
	require.True(t, reworker.called)
}

// TestReviewSubmitRequestsReworkFromReviewLane pins #1091: the normal review
// path is board-dispatched (a GitHubReviewLane run), and before this fix a
// blocking verdict only moved the lane to revision. The linked coding issue
// stayed in review with no run, so the PR was stranded until a human pushed the
// fix. A CHANGES_REQUESTED verdict must always enqueue the linked issue's coding
// agent with the finding, whether or not the review run had a work object.
func TestReviewSubmitRequestsReworkFromReviewLane(t *testing.T) {
	api := newReviewToolsAPI(t)
	client := githubapp.NewClient(api.url, "ghs_test", api.server.Client())
	head := strings.Repeat("a", 40)
	issueID := uuid.New()
	lane := &reviewStatusRecorder{}
	reworker := &reviewReworkRecorder{}
	h := &ReviewToolHandler{
		links:     reviewLinkReader{link: &SourceIssuePRLink{IssueCanonicalID: issueID}},
		workItems: lane,
		reworker:  reworker,
	}
	ctx := mcp.ContextWithWorkObjectID(mcp.ContextWithNewToolResultCache(context.Background()), "lane-1")
	_, err := h.executePRDiff(ctx, client, "project-1", "owner/repo", nil, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
	})
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, uuid.NewString(), "owner/repo", nil,
		reviewSubmitArgs(head, "CHANGES_REQUESTED", "untrusted document text must not pick existing_entity_id"))
	require.NoError(t, err)

	require.Equal(t, "revision", lane.status, "the review lane is finalized from the verdict")
	require.True(t, reworker.called, "a blocking verdict on a review lane must enqueue the linked coding agent")
	require.Equal(t, issueID.String(), reworker.canonicalID)
	require.Contains(t, reworker.text, "existing_entity_id", "the coding run must carry the review finding")
}
