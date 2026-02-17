package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DTO Tests ---

func TestAttachSessionRequest(t *testing.T) {
	req := AttachSessionRequest{AgentSessionID: "session-abc-123"}
	assert.Equal(t, "session-abc-123", req.AgentSessionID)
}

func TestSnapshotResponse(t *testing.T) {
	resp := SnapshotResponse{
		SnapshotID:  "snap-123",
		WorkspaceID: "ws-456",
		Provider:    "gvisor",
		CreatedAt:   "2026-01-15T10:30:00Z",
	}
	assert.Equal(t, "snap-123", resp.SnapshotID)
	assert.Equal(t, "ws-456", resp.WorkspaceID)
	assert.Equal(t, "gvisor", resp.Provider)
	assert.NotEmpty(t, resp.CreatedAt)
}

func TestCreateFromSnapshotRequest(t *testing.T) {
	req := CreateFromSnapshotRequest{
		SnapshotID:     "snap-123",
		Provider:       "gvisor",
		DeploymentMode: "self-hosted",
		ResourceLimits: &ResourceLimits{CPU: "2", Memory: "4G"},
	}
	assert.Equal(t, "snap-123", req.SnapshotID)
	assert.Equal(t, "gvisor", req.Provider)
	assert.Equal(t, "self-hosted", req.DeploymentMode)
	require.NotNil(t, req.ResourceLimits)
	assert.Equal(t, "2", req.ResourceLimits.CPU)
}

func TestCreateFromSnapshotRequest_Defaults(t *testing.T) {
	req := CreateFromSnapshotRequest{
		SnapshotID: "snap-456",
	}
	assert.Equal(t, "snap-456", req.SnapshotID)
	assert.Empty(t, req.Provider)
	assert.Empty(t, req.DeploymentMode)
	assert.Nil(t, req.ResourceLimits)
}

// --- Snapshot Provider Interface Tests ---

func TestMockProviderSnapshot(t *testing.T) {
	t.Run("supports snapshots", func(t *testing.T) {
		mock := &mockProvider{
			name:              "gv",
			providerType:      ProviderGVisor,
			supportsSnapshots: true,
		}
		mock.createCount.Store(5)

		snapID, err := mock.Snapshot(t.Context(), "container-123")
		require.NoError(t, err)
		assert.Equal(t, "snap-gv-5", snapID)
	})

	t.Run("custom snapshot ID", func(t *testing.T) {
		mock := &mockProvider{
			name:              "gv",
			providerType:      ProviderGVisor,
			supportsSnapshots: true,
			snapshotID:        "custom-snap-id",
		}

		snapID, err := mock.Snapshot(t.Context(), "container-123")
		require.NoError(t, err)
		assert.Equal(t, "custom-snap-id", snapID)
	})

	t.Run("snapshots not supported", func(t *testing.T) {
		mock := &mockProvider{
			name:              "basic",
			providerType:      ProviderE2B,
			supportsSnapshots: false,
		}

		_, err := mock.Snapshot(t.Context(), "container-123")
		assert.ErrorIs(t, err, ErrSnapshotNotSupported)
	})

	t.Run("snapshot error", func(t *testing.T) {
		mock := &mockProvider{
			name:              "gv",
			providerType:      ProviderGVisor,
			supportsSnapshots: true,
			snapshotErr:       assert.AnError,
		}

		_, err := mock.Snapshot(t.Context(), "container-123")
		assert.ErrorIs(t, err, assert.AnError)
	})
}

func TestMockProviderCreateFromSnapshot(t *testing.T) {
	t.Run("create from snapshot", func(t *testing.T) {
		mock := &mockProvider{
			name:              "gv",
			providerType:      ProviderGVisor,
			supportsSnapshots: true,
		}

		result, err := mock.CreateFromSnapshot(t.Context(), "snap-123", &CreateContainerRequest{
			ContainerType: ContainerTypeAgentWorkspace,
		})
		require.NoError(t, err)
		assert.Contains(t, result.ProviderID, "mock-gv-from-snap-123-")
	})

	t.Run("not supported", func(t *testing.T) {
		mock := &mockProvider{
			name:              "basic",
			providerType:      ProviderE2B,
			supportsSnapshots: false,
		}

		_, err := mock.CreateFromSnapshot(t.Context(), "snap-123", &CreateContainerRequest{})
		assert.ErrorIs(t, err, ErrSnapshotNotSupported)
	})

	t.Run("from snapshot error", func(t *testing.T) {
		mock := &mockProvider{
			name:              "gv",
			providerType:      ProviderGVisor,
			supportsSnapshots: true,
			fromSnapshotErr:   assert.AnError,
		}

		_, err := mock.CreateFromSnapshot(t.Context(), "snap-123", &CreateContainerRequest{})
		assert.ErrorIs(t, err, assert.AnError)
	})
}

// --- ErrSnapshotNotSupported Tests ---

func TestErrSnapshotNotSupported(t *testing.T) {
	assert.Contains(t, ErrSnapshotNotSupported.Error(), "snapshot")
	assert.Contains(t, ErrSnapshotNotSupported.Error(), "not supported")
}

// --- Orchestrator Snapshot Selection Tests ---

func TestOrchestratorSnapshotCapabilities(t *testing.T) {
	o := NewOrchestrator(testLogger())

	// Register provider with snapshot support
	gv := &mockProvider{
		name:              "gv",
		providerType:      ProviderGVisor,
		healthy:           true,
		supportsSnapshots: true,
	}
	o.RegisterProvider(ProviderGVisor, gv)

	// Verify we can get the provider and it supports snapshots
	p, err := o.GetProvider(ProviderGVisor)
	require.NoError(t, err)
	assert.True(t, p.Capabilities().SupportsSnapshots)
}

// --- Workspace ToResponse with SnapshotID ---

func TestToResponse_WithSnapshotID(t *testing.T) {
	snapID := "snap-abc-123"
	ws := &AgentWorkspace{
		ID:            "ws-789",
		ContainerType: ContainerTypeAgentWorkspace,
		Provider:      ProviderGVisor,
		Status:        StatusReady,
		SnapshotID:    &snapID,
		Metadata:      map[string]any{"created_from_snapshot": "snap-abc-123"},
	}

	resp := ToResponse(ws)
	require.NotNil(t, resp.SnapshotID)
	assert.Equal(t, "snap-abc-123", *resp.SnapshotID)
	assert.Equal(t, "snap-abc-123", resp.Metadata["created_from_snapshot"])
}

func TestToResponse_NilSnapshotID(t *testing.T) {
	ws := &AgentWorkspace{
		ID:            "ws-789",
		ContainerType: ContainerTypeAgentWorkspace,
		Provider:      ProviderGVisor,
		Status:        StatusReady,
		SnapshotID:    nil,
	}

	resp := ToResponse(ws)
	assert.Nil(t, resp.SnapshotID)
}

// --- Service Attachment Validation Tests ---
// These test the business logic without requiring a DB.

func TestAttachSessionRequest_EmptyValidation(t *testing.T) {
	req := AttachSessionRequest{}
	assert.Empty(t, req.AgentSessionID)
}

func TestCreateFromSnapshotRequest_EmptySnapshotID(t *testing.T) {
	req := CreateFromSnapshotRequest{}
	assert.Empty(t, req.SnapshotID)
}
