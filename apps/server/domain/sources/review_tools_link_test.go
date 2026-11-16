package sources

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

type reviewLinkStore struct {
	link *graph.GraphObject
}

func (s reviewLinkStore) CreateOrUpdate(context.Context, uuid.UUID, *graph.CreateGraphObjectRequest, *uuid.UUID) (*graph.GraphObjectResponse, bool, error) {
	return nil, false, nil
}
func (s reviewLinkStore) FindHeadObjectByTypeAndKey(context.Context, string, string, string) (*graph.GraphObject, error) {
	return s.link, nil
}

type reviewReworkRecorder struct {
	called bool
}

func (r *reviewReworkRecorder) RequestWorkItemChanges(context.Context, string, string, string, string) error {
	r.called = true
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
	issueID := uuid.NewString()
	h := &ReviewToolHandler{
		workObjects: reviewLinkStore{link: &graph.GraphObject{Properties: map[string]any{
			"issue_canonical_id": issueID,
		}}},
		workItems: &reviewStatusRecorder{},
		reworker:  &reviewReworkRecorder{},
	}
	ctx := mcp.ContextWithNewToolResultCache(context.Background())
	_, err := h.executePRDiff(ctx, client, "owner/repo", head)
	require.NoError(t, err)
	_, err = h.executeReviewSubmit(ctx, client, uuid.NewString(), "owner/repo", reviewSubmitArgs(head, "CHANGES_REQUESTED", "fix the bug"))
	require.NoError(t, err)
	require.True(t, h.reworker.(*reviewReworkRecorder).called)
}
