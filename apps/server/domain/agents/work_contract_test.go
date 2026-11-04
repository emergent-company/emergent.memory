package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// contractStore records the completion transition and resolves declared
// deliverables by (type, key). It embeds fakeWorkObjectStore for the unused
// WorkObjectStore surface.
type contractStore struct {
	fakeWorkObjectStore
	heads     map[string]*graph.WorkObjectHead // key: objType+"|"+key
	completed bool
}

func (f *contractStore) FindHeadByTypeAndKey(ctx context.Context, projectID, objType, key string) (*graph.WorkObjectHead, error) {
	if f.heads == nil {
		return nil, nil
	}
	return f.heads[objType+"|"+key], nil
}

func (f *contractStore) CompleteWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, doneStatus, reviewStatus string, requiresReview bool) (bool, error) {
	f.completed = true
	return true, nil
}

func contractDeps(store WorkObjectStore, contract AgentWorkContract) WorkToolDeps {
	return WorkToolDeps{
		Store:       store,
		Repo:        nil,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		ProjectID:   "p-1",
		CanonicalID: uuid.NewString(),
		WorkConfig:  AgentWorkConfig{WorkContract: contract},
		Terminator:  &WorkTerminatorState{},
	}
}

func deliverable(t, key string) map[string]any {
	return map[string]any{"type": t, "key": key}
}

// --- unit: validateWorkContract ---

func TestValidateWorkContract_EmptyContractReturnsEmpty(t *testing.T) {
	store := &contractStore{}
	require.Equal(t, "", validateWorkContract(context.Background(), store, "p-1",
		AgentWorkContract{}, "", nil, nil))
}

func TestValidateWorkContract_RequireArtifacts(t *testing.T) {
	store := &contractStore{}
	contract := AgentWorkContract{RequireArtifacts: true}

	// No summary, no artifacts → rejected.
	require.NotEmpty(t, validateWorkContract(context.Background(), store, "p-1", contract, "", nil, nil))
	// A summary satisfies requireArtifacts.
	require.Equal(t, "", validateWorkContract(context.Background(), store, "p-1", contract, "did the work", nil, nil))
	// Artifacts satisfy requireArtifacts.
	require.Equal(t, "", validateWorkContract(context.Background(), store, "p-1", contract, "", []string{"k1"}, nil))
}

func TestValidateWorkContract_RequiredDeliverableTypes(t *testing.T) {
	store := &contractStore{heads: map[string]*graph.WorkObjectHead{
		"ResearchReport|r1": {CanonicalID: uuid.NewString(), Type: "ResearchReport", Key: "r1"},
	}}
	contract := AgentWorkContract{RequiredDeliverableTypes: []string{"ResearchReport"}}

	// No deliverables declared → rejected.
	require.NotEmpty(t, validateWorkContract(context.Background(), store, "p-1", contract, "s", nil, nil))
	// Wrong type declared → rejected.
	require.NotEmpty(t, validateWorkContract(context.Background(), store, "p-1", contract, "s", nil,
		[]workDeliverable{{Type: "Note", Key: "n1"}}))
	// Declared but does not resolve to an existing object → rejected.
	require.NotEmpty(t, validateWorkContract(context.Background(), store, "p-1", contract, "s", nil,
		[]workDeliverable{{Type: "ResearchReport", Key: "missing"}}))
	// Declared and resolves → satisfied.
	require.Equal(t, "", validateWorkContract(context.Background(), store, "p-1", contract, "s", nil,
		[]workDeliverable{{Type: "ResearchReport", Key: "r1"}}))
}

// --- unit: completeWork contract gating ---

func TestCompleteWork_ContractSatisfiedCompletes(t *testing.T) {
	store := &contractStore{heads: map[string]*graph.WorkObjectHead{
		"ResearchReport|r1": {CanonicalID: uuid.NewString(), Type: "ResearchReport", Key: "r1"},
	}}
	contract := AgentWorkContract{RequiredDeliverableTypes: []string{"ResearchReport"}}
	deps := contractDeps(store, contract)

	result, err := completeWork(context.Background(), deps, map[string]any{
		"summary":      "done",
		"deliverables": []any{deliverable("ResearchReport", "r1")},
	})
	require.NoError(t, err)
	require.Equal(t, "completed", result["status"])
	require.True(t, store.completed)
	require.True(t, deps.Terminator.ShouldFinalize())
	require.Equal(t, WorkTerminatorComplete, deps.Terminator.Kind())
}

func TestCompleteWork_MissingDeliverableRejectedNotFinalized(t *testing.T) {
	store := &contractStore{}
	contract := AgentWorkContract{RequiredDeliverableTypes: []string{"ResearchReport"}}
	deps := contractDeps(store, contract)

	result, err := completeWork(context.Background(), deps, map[string]any{
		"summary": "done but no deliverable",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result["error"])
	require.False(t, store.completed)
	require.False(t, deps.Terminator.ShouldFinalize())
}

func TestCompleteWork_RequireArtifactsWithNoneRejected(t *testing.T) {
	store := &contractStore{}
	contract := AgentWorkContract{RequireArtifacts: true}
	deps := contractDeps(store, contract)

	result, err := completeWork(context.Background(), deps, map[string]any{})
	require.NoError(t, err)
	require.NotEmpty(t, result["error"])
	require.False(t, store.completed)
	require.False(t, deps.Terminator.ShouldFinalize())
}

func TestCompleteWork_EmptyContractUnchanged(t *testing.T) {
	store := &contractStore{}
	deps := contractDeps(store, AgentWorkContract{})

	result, err := completeWork(context.Background(), deps, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "completed", result["status"])
	require.True(t, store.completed)
	require.True(t, deps.Terminator.ShouldFinalize())
}
