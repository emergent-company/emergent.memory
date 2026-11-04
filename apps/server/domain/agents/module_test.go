package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// fakeOrphanSandboxStore is an in-memory orphanedSandboxStore for exercising
// the startup recovery hook without a database.
type fakeOrphanSandboxStore struct {
	rows      []*sandbox.AgentSandbox
	liveArg   []string
	updateErr error
	updated   []string
}

func (f *fakeOrphanSandboxStore) ListOrphanedSandboxes(_ context.Context, live []string) ([]*sandbox.AgentSandbox, error) {
	f.liveArg = append([]string(nil), live...)
	return f.rows, nil
}

func (f *fakeOrphanSandboxStore) Update(_ context.Context, ws *sandbox.AgentSandbox, _ ...string) (*sandbox.AgentSandbox, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.updated = append(f.updated, ws.ID)
	return ws, nil
}

// A sandbox row whose owning run is no longer active is transitioned out of its
// non-stopped state on startup so its container becomes eligible for reclamation.
func TestRecoverOrphanedSandboxes_TransitionsNonStoppedRow(t *testing.T) {
	store := &fakeOrphanSandboxStore{rows: []*sandbox.AgentSandbox{
		{ID: "ws-1", Status: sandbox.StatusReady, ProviderWorkspaceID: "prov-1", ContainerType: sandbox.ContainerTypeAgentSandbox},
	}}

	n, err := recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"ws-1"}, store.updated)
	assert.Equal(t, sandbox.StatusStopped, store.rows[0].Status, "row must be transitioned to stopped")
}

// Rows already in a terminal state are left untouched (idempotent recovery).
func TestRecoverOrphanedSandboxes_LeavesTerminalRowsAlone(t *testing.T) {
	store := &fakeOrphanSandboxStore{rows: []*sandbox.AgentSandbox{
		{ID: "ws-stopped", Status: sandbox.StatusStopped},
		{ID: "ws-error", Status: sandbox.StatusError},
		{ID: "ws-ready", Status: sandbox.StatusReady},
	}}

	n, err := recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"ws-ready"}, store.updated, "only the non-terminal row may be updated")
}

// A repeated startup must not modify an already-transitioned row again.
func TestRecoverOrphanedSandboxes_IdempotentAcrossRestarts(t *testing.T) {
	store := &fakeOrphanSandboxStore{rows: []*sandbox.AgentSandbox{
		{ID: "ws-1", Status: sandbox.StatusReady},
	}}

	n, err := recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// The row is now stopped; a second startup sees a terminal row and leaves it.
	n, err = recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, []string{"ws-1"}, store.updated, "no additional update on the second startup")
}

// The recovery hook must tell the store which run states still own their
// sandbox, so a live run's rows are never returned.
func TestRecoverOrphanedSandboxes_SparsesLiveRunStates(t *testing.T) {
	store := &fakeOrphanSandboxStore{}

	_, err := recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{string(RunStatusQueued), string(RunStatusRunning), string(RunStatusCancelling)},
		store.liveArg,
	)
}

// A failed update is logged and does not abort the remaining rows.
func TestRecoverOrphanedSandboxes_UpdateFailureDoesNotAbort(t *testing.T) {
	// First ListOrphanedSandboxes returns two rows; the store errors on Update.
	store := &fakeOrphanSandboxStore{
		rows: []*sandbox.AgentSandbox{
			{ID: "ws-1", Status: sandbox.StatusReady},
			{ID: "ws-2", Status: sandbox.StatusReady},
		},
		updateErr: assert.AnError,
	}

	n, err := recoverOrphanedSandboxes(context.Background(), store, testAgentLogger())
	require.NoError(t, err, "best-effort recovery must not fail startup")
	assert.Equal(t, 0, n)
}

// A nil store is a safe no-op.
func TestRecoverOrphanedSandboxes_NilStore(t *testing.T) {
	n, err := recoverOrphanedSandboxes(context.Background(), nil, testAgentLogger())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
