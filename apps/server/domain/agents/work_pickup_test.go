package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// ---------- ValidateWorkPickup ----------

func TestValidateWorkPickup(t *testing.T) {
	oneType := func() *ReactionConfig {
		return &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeCreated}, ConcurrencyStrategy: ConcurrencySkip}
	}
	tests := []struct {
		name          string
		hasWorkConfig bool
		rc            *ReactionConfig
		wantErr       bool
	}{
		{"inline agent bypasses validation", false, &ReactionConfig{ObjectTypes: []string{"a", "b"}, Events: []ReactionEventType{EventTypeCreated, EventTypeDeleted}}, false},
		{"work agent nil config rejected", true, nil, true},
		{"work agent zero object types rejected", true, &ReactionConfig{Events: []ReactionEventType{EventTypeCreated}}, true},
		{"work agent multiple object types rejected", true, &ReactionConfig{ObjectTypes: []string{"a", "b"}, Events: []ReactionEventType{EventTypeCreated}}, true},
		{"work agent zero signals rejected", true, &ReactionConfig{ObjectTypes: []string{"Task"}}, true},
		{"work agent multiple signals rejected", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeCreated, EventTypeUpdated}}, true},
		{"work agent deleted signal rejected", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeDeleted}}, true},
		{"work agent empty concurrency rejected", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeCreated}}, true},
		{"work agent parallel concurrency rejected", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeCreated}, ConcurrencyStrategy: ConcurrencyParallel}, true},
		{"work agent created ok", true, oneType(), false},
		{"work agent updated ok", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeUpdated}, ConcurrencyStrategy: ConcurrencySkip}, false},
		{"work agent status_changed ok", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeStatusChanged}, ConcurrencyStrategy: ConcurrencySkip}, false},
		{"work agent assigned ok", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeAssigned}, ConcurrencyStrategy: ConcurrencySkip}, false},
		{"work agent skip concurrency ok", true, &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeCreated}, ConcurrencyStrategy: ConcurrencySkip}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkPickup(tt.hasWorkConfig, tt.rc)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------- TriggerStatusValues ----------

func TestTriggerStatusValues_Default(t *testing.T) {
	assert.Equal(t, []string{"ready"}, (AgentWorkConfig{}).TriggerStatusValues())
	assert.Equal(t, []string{"todo"}, (AgentWorkConfig{TriggerStatuses: []string{"todo"}}).TriggerStatusValues())
}

func TestAgentWorkConfigIsZero_IncludesTriggerStatuses(t *testing.T) {
	assert.True(t, (AgentWorkConfig{}).IsZero())
	assert.False(t, (AgentWorkConfig{TriggerStatuses: []string{"todo"}}).IsZero())
}

// ---------- workPickupMatch (4-way) ----------

func TestWorkPickupMatch(t *testing.T) {
	agent := makeTestAgent("a1", "researcher", "p1", &ReactionConfig{Properties: []string{"/title"}})
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{TriggerStatuses: []string{"todo", "review"}}}

	tests := []struct {
		name      string
		eventType ReactionEventType
		data      map[string]any
		want      bool
	}{
		{"created always matches", EventTypeCreated, map[string]any{}, true},
		{"updated matches intersecting property", EventTypeUpdated, map[string]any{"properties": []string{"/title", "/body"}}, true},
		{"updated no intersection", EventTypeUpdated, map[string]any{"properties": []string{"/body"}}, false},
		{"updated no changed paths", EventTypeUpdated, map[string]any{}, false},
		{"status_changed matches trigger status", EventTypeStatusChanged, map[string]any{"status": "todo"}, true},
		{"status_changed not in trigger statuses", EventTypeStatusChanged, map[string]any{"status": "done"}, false},
		{"assigned matches agent name", EventTypeAssigned, map[string]any{"assignee": "researcher"}, true},
		{"assigned other name", EventTypeAssigned, map[string]any{"assignee": "someone-else"}, false},
		{"assigned empty assignee", EventTypeAssigned, map[string]any{}, false},
		{"deleted never matches", EventTypeDeleted, map[string]any{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, workPickupMatch(tt.eventType, agent, def, tt.data))
		})
	}

	// updated with no declared properties on the agent matches any emitted
	// change; updated with no changed paths still never matches.
	plain := makeTestAgent("a2", "researcher", "p1", &ReactionConfig{ObjectTypes: []string{"Task"}, Events: []ReactionEventType{EventTypeUpdated}})
	assert.True(t, workPickupMatch(EventTypeUpdated, plain, def, map[string]any{"properties": []string{"/title"}}))
	assert.False(t, workPickupMatch(EventTypeUpdated, plain, def, map[string]any{}))

	// updated with declared properties matches only on intersection.
	declared := makeTestAgent("a3", "researcher", "p1", &ReactionConfig{Properties: []string{"/title"}})
	assert.True(t, workPickupMatch(EventTypeUpdated, declared, def, map[string]any{"properties": []string{"/title", "/body"}}))
	assert.False(t, workPickupMatch(EventTypeUpdated, declared, def, map[string]any{"properties": []string{"/body"}}))
}

func TestWorkPickupTriggerMetadata(t *testing.T) {
	assert.Equal(t, map[string]any{"work_trigger_event": "created"}, workPickupTriggerMetadata(EventTypeCreated, map[string]any{}))
	assert.Equal(t, map[string]any{"work_trigger_event": "status_changed", "work_claim_status": "todo"}, workPickupTriggerMetadata(EventTypeStatusChanged, map[string]any{"status": "todo"}))
	assert.Equal(t, map[string]any{"work_trigger_event": "assigned"}, workPickupTriggerMetadata(EventTypeAssigned, map[string]any{"assignee": "researcher"}))
}

// ---------- claimWorkObject claim-source dispatch ----------

type claimTransition struct{ from, to string }
type claimReady struct{ ready, inProgress string }

type recordingWorkObjectStore struct {
	head            *graph.WorkObjectHead
	readyClaims     []claimReady
	transitionCalls []claimTransition
}

func (r *recordingWorkObjectStore) GetHeadObject(ctx context.Context, projectID, canonicalID string) (*graph.WorkObjectHead, error) {
	if r.head != nil {
		return r.head, nil
	}
	return &graph.WorkObjectHead{CanonicalID: canonicalID, Type: "Task", Version: 1, Status: "todo"}, nil
}
func (r *recordingWorkObjectStore) FindHeadByTypeAndKey(ctx context.Context, projectID, objType, key string) (*graph.WorkObjectHead, error) {
	return nil, nil
}
func (r *recordingWorkObjectStore) ClaimWorkObject(ctx context.Context, projectID, canonicalID, readyStatus, inProgressStatus string) (bool, error) {
	r.readyClaims = append(r.readyClaims, claimReady{readyStatus, inProgressStatus})
	return true, nil
}
func (r *recordingWorkObjectStore) TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t graph.WorkObjectTransition) (bool, error) {
	r.transitionCalls = append(r.transitionCalls, claimTransition{t.FromStatus, t.ToStatus})
	return true, nil
}
func (r *recordingWorkObjectStore) CompleteWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, doneStatus, reviewStatus string, requiresReview bool) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) BlockWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, blockedStatus string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) UnassignWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, readyStatus string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) ApproveWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, doneStatus, reviewerID string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) RequestChangesWorkObject(ctx context.Context, projectID, canonicalID, reviewStatus, revisionStatus string, reworkAgentID *uuid.UUID) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) ReassignWorkObject(ctx context.Context, projectID, canonicalID string, assignee *string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) UpdateWorkObjectStatus(ctx context.Context, projectID, canonicalID, status, reviewStatus string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) CancelWorkObject(ctx context.Context, projectID, canonicalID, blockedStatus string) (bool, error) {
	return true, nil
}
func (r *recordingWorkObjectStore) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*graph.WorkObjectHead, error) {
	return nil, nil
}
func (r *recordingWorkObjectStore) ListWorkItems(ctx context.Context, projectID, status, typeName string, limit int) ([]*graph.WorkItem, error) {
	return nil, nil
}
func (r *recordingWorkObjectStore) GetObjectTypeWorkConfig(ctx context.Context, projectID, typeName string) (*agents.ObjectTypeWorkConfig, error) {
	return nil, nil
}

func newClaimPool(store WorkObjectStore) *WorkerPool {
	pool := NewWorkerPool(nil, nil, testAgentLogger(), 0, time.Second)
	pool.SetWorkObjectStore(store)
	return pool
}

func runWithMetadata(event ReactionEventType, claimStatus string) *AgentRun {
	run := &AgentRun{SubjectObjectID: strPtr("obj-1")}
	if event != "" {
		meta := map[string]any{"work_trigger_event": string(event)}
		if claimStatus != "" {
			meta["work_claim_status"] = claimStatus
		}
		run.TriggerMetadata = meta
	}
	return run
}

func TestClaimWorkObject_PickupSources(t *testing.T) {
	agent := makeTestAgent("a1", "researcher", "p1", nil)
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{TriggerStatuses: []string{"todo"}}}

	t.Run("status_changed CASes from matched status", func(t *testing.T) {
		store := &recordingWorkObjectStore{}
		pool := newClaimPool(store)
		claimed, err := pool.claimWorkObject(context.Background(), runWithMetadata(EventTypeStatusChanged, "todo"), agent, def)
		require.NoError(t, err)
		assert.True(t, claimed)
		require.Len(t, store.transitionCalls, 1)
		assert.Equal(t, claimTransition{from: "todo", to: "in_progress"}, store.transitionCalls[0])
		assert.Empty(t, store.readyClaims)
	})

	t.Run("assigned CASes from HEAD current status", func(t *testing.T) {
		store := &recordingWorkObjectStore{}
		pool := newClaimPool(store)
		claimed, err := pool.claimWorkObject(context.Background(), runWithMetadata(EventTypeAssigned, ""), agent, def)
		require.NoError(t, err)
		assert.True(t, claimed)
		require.Len(t, store.transitionCalls, 1)
		assert.Equal(t, claimTransition{from: "todo", to: "in_progress"}, store.transitionCalls[0])
	})

	t.Run("created claims from ready", func(t *testing.T) {
		store := &recordingWorkObjectStore{}
		pool := newClaimPool(store)
		claimed, err := pool.claimWorkObject(context.Background(), runWithMetadata(EventTypeCreated, ""), agent, def)
		require.NoError(t, err)
		assert.True(t, claimed)
		require.Len(t, store.readyClaims, 1)
		assert.Equal(t, claimReady{ready: "ready", inProgress: "in_progress"}, store.readyClaims[0])
		assert.Empty(t, store.transitionCalls)
	})

	t.Run("legacy dispatch without metadata claims from ready", func(t *testing.T) {
		store := &recordingWorkObjectStore{}
		pool := newClaimPool(store)
		claimed, err := pool.claimWorkObject(context.Background(), runWithMetadata("", ""), agent, def)
		require.NoError(t, err)
		assert.True(t, claimed)
		require.Len(t, store.readyClaims, 1)
	})
}

// ---------- reactionSubscribesToWork ----------

func TestReactionSubscribesToWork(t *testing.T) {
	assert.True(t, reactionSubscribesToWork(&ReactionConfig{Events: []ReactionEventType{EventTypeCreated}}))
	assert.True(t, reactionSubscribesToWork(&ReactionConfig{Events: []ReactionEventType{EventTypeStatusChanged}}))
	assert.False(t, reactionSubscribesToWork(&ReactionConfig{Events: []ReactionEventType{EventTypeUpdated}}))
	assert.False(t, reactionSubscribesToWork(&ReactionConfig{Events: []ReactionEventType{EventTypeAssigned}}))
}
