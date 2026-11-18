package sources

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

type reviewerLaneStore struct {
	objects map[string]*graph.GraphObject
}

func (s *reviewerLaneStore) FindHeadObjectByTypeAndKey(_ context.Context, _ string, typ, key string) (*graph.GraphObject, error) {
	if obj := s.objects[key]; obj != nil && obj.Type == typ {
		return obj, nil
	}
	return nil, nil
}

func (s *reviewerLaneStore) CreateOrUpdate(_ context.Context, _ uuid.UUID, req *graph.CreateGraphObjectRequest, _ *uuid.UUID) (*graph.GraphObjectResponse, bool, error) {
	key := ""
	if req.Key != nil {
		key = *req.Key
	}
	obj := s.objects[key]
	if obj == nil {
		obj = &graph.GraphObject{ID: uuid.New(), CanonicalID: uuid.New(), Type: req.Type, Key: req.Key}
		s.objects[key] = obj
	}
	obj.Type = req.Type
	obj.Name = req.Name
	obj.Status = req.Status
	obj.Assignee = req.Assignee
	obj.Properties = req.Properties
	return &graph.GraphObjectResponse{ID: obj.ID, CanonicalID: obj.CanonicalID, Type: obj.Type, Key: obj.Key, Status: obj.Status}, false, nil
}

func TestEnsureReviewLanesSupersedesPreviousHead(t *testing.T) {
	ctx := context.Background()
	src := &Source{ID: uuid.New(), ProjectID: uuid.New()}
	agentID := uuid.NewString()
	repo := "acme/repo"
	number := 7
	oldHead, newHead := "oldsha", "newsha"
	parentKey := fmt.Sprintf("github:%s:%s#%d", src.ID.String(), repo, number)
	oldKey := parentKey + ":review:" + agentID + ":" + oldHead
	newKey := parentKey + ":review:" + agentID + ":" + newHead
	oldName := "PR · security"
	oldStatus := "ready"
	store := &reviewerLaneStore{objects: map[string]*graph.GraphObject{
		oldKey: {
			ID: uuid.New(), CanonicalID: uuid.New(), Type: "GitHubReviewLane",
			Key: &oldKey, Name: &oldName, Status: &oldStatus,
			Properties: map[string]any{"head_sha": oldHead, "status": oldStatus},
		},
	}}
	d := &SourceEventDispatcher{workObjects: store}
	p := &githubapp.WebhookPayload{Repository: githubapp.WebhookRepo{FullName: repo}}
	p.PullRequest.Number = number
	p.PullRequest.Title = "PR"
	p.PullRequest.Head.SHA = newHead
	gate := &ReviewGate{RequiredReviewers: []ReviewReviewer{{AgentID: agentID, Name: "security"}}}

	require.NoError(t, d.ensureReviewLanes(ctx, src, p, nil, "acme/repo#7", oldHead, newHead, gate))
	require.Equal(t, "cancelled", *store.objects[oldKey].Status)
	require.NotNil(t, store.objects[newKey])
	require.Equal(t, "ready", *store.objects[newKey].Status)
	require.Equal(t, newHead, store.objects[newKey].Properties["head_sha"])
}

func TestEnsureReviewLanesPreservesSameHeadStatus(t *testing.T) {
	ctx := context.Background()
	src := &Source{ID: uuid.New(), ProjectID: uuid.New()}
	agentID := uuid.NewString()
	repo := "acme/repo"
	number := 7
	head := "sha"
	parentKey := fmt.Sprintf("github:%s:%s#%d", src.ID.String(), repo, number)
	key := parentKey + ":review:" + agentID + ":" + head
	name := "PR · security"
	status := "done"
	store := &reviewerLaneStore{objects: map[string]*graph.GraphObject{
		key: {ID: uuid.New(), CanonicalID: uuid.New(), Type: "GitHubReviewLane", Key: &key, Name: &name, Status: &status, Properties: map[string]any{"head_sha": head}},
	}}
	d := &SourceEventDispatcher{workObjects: store}
	p := &githubapp.WebhookPayload{Repository: githubapp.WebhookRepo{FullName: repo}}
	p.PullRequest.Number = number
	p.PullRequest.Title = "PR"
	p.PullRequest.Head.SHA = head
	gate := &ReviewGate{RequiredReviewers: []ReviewReviewer{{AgentID: agentID, Name: "security"}}}

	require.NoError(t, d.ensureReviewLanes(ctx, src, p, nil, "acme/repo#7", head, head, gate))
	require.Equal(t, "done", *store.objects[key].Status)
}
