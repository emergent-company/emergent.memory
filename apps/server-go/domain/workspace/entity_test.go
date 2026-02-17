package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		ct       ContainerType
		expected string
	}{
		{"agent workspace", ContainerTypeAgentWorkspace, "agent_workspace"},
		{"mcp server", ContainerTypeMCPServer, "mcp_server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.ct))
		})
	}
}

func TestProviderTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		pt       ProviderType
		expected string
	}{
		{"firecracker", ProviderFirecracker, "firecracker"},
		{"e2b", ProviderE2B, "e2b"},
		{"gvisor", ProviderGVisor, "gvisor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.pt))
		})
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		s        Status
		expected string
	}{
		{"creating", StatusCreating, "creating"},
		{"ready", StatusReady, "ready"},
		{"stopping", StatusStopping, "stopping"},
		{"stopped", StatusStopped, "stopped"},
		{"error", StatusError, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.s))
		})
	}
}

func TestToResponse(t *testing.T) {
	now := time.Now()
	repoURL := "https://github.com/org/repo"
	branch := "main"
	sessionID := "session-123"
	expiresAt := now.Add(24 * time.Hour)

	ws := &AgentWorkspace{
		ID:                  "ws-123",
		AgentSessionID:      &sessionID,
		ContainerType:       ContainerTypeAgentWorkspace,
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-abc",
		RepositoryURL:       &repoURL,
		Branch:              &branch,
		DeploymentMode:      DeploymentSelfHosted,
		Lifecycle:           LifecycleEphemeral,
		Status:              StatusReady,
		CreatedAt:           now,
		LastUsedAt:          now,
		ExpiresAt:           &expiresAt,
		ResourceLimits:      &ResourceLimits{CPU: "2", Memory: "4G", Disk: "10G"},
		MCPConfig:           nil,
		Metadata:            map[string]any{"key": "value"},
	}

	resp := ToResponse(ws)

	require.NotNil(t, resp)
	assert.Equal(t, "ws-123", resp.ID)
	assert.Equal(t, &sessionID, resp.AgentSessionID)
	assert.Equal(t, ContainerTypeAgentWorkspace, resp.ContainerType)
	assert.Equal(t, ProviderGVisor, resp.Provider)
	assert.Equal(t, "container-abc", resp.ProviderWorkspaceID)
	assert.Equal(t, &repoURL, resp.RepositoryURL)
	assert.Equal(t, &branch, resp.Branch)
	assert.Equal(t, DeploymentSelfHosted, resp.DeploymentMode)
	assert.Equal(t, LifecycleEphemeral, resp.Lifecycle)
	assert.Equal(t, StatusReady, resp.Status)
	assert.NotEmpty(t, resp.CreatedAt)
	assert.NotEmpty(t, resp.LastUsedAt)
	require.NotNil(t, resp.ExpiresAt)
	assert.NotEmpty(t, *resp.ExpiresAt)
	require.NotNil(t, resp.ResourceLimits)
	assert.Equal(t, "2", resp.ResourceLimits.CPU)
	assert.Equal(t, "4G", resp.ResourceLimits.Memory)
	assert.Equal(t, "10G", resp.ResourceLimits.Disk)
	assert.Nil(t, resp.MCPConfig)
	assert.Equal(t, map[string]any{"key": "value"}, resp.Metadata)
}

func TestToResponse_NilExpiresAt(t *testing.T) {
	ws := &AgentWorkspace{
		ID:            "ws-456",
		ContainerType: ContainerTypeMCPServer,
		Provider:      ProviderGVisor,
		Lifecycle:     LifecyclePersistent,
		Status:        StatusReady,
		CreatedAt:     time.Now(),
		LastUsedAt:    time.Now(),
		ExpiresAt:     nil,
	}

	resp := ToResponse(ws)

	require.NotNil(t, resp)
	assert.Nil(t, resp.ExpiresAt)
}

func TestToResponseList(t *testing.T) {
	now := time.Now()
	workspaces := []*AgentWorkspace{
		{ID: "ws-1", ContainerType: ContainerTypeAgentWorkspace, Provider: ProviderGVisor, Status: StatusReady, CreatedAt: now, LastUsedAt: now},
		{ID: "ws-2", ContainerType: ContainerTypeMCPServer, Provider: ProviderGVisor, Status: StatusCreating, CreatedAt: now, LastUsedAt: now},
	}

	resp := ToResponseList(workspaces)

	require.Len(t, resp, 2)
	assert.Equal(t, "ws-1", resp[0].ID)
	assert.Equal(t, "ws-2", resp[1].ID)
}

func TestToResponseList_Empty(t *testing.T) {
	resp := ToResponseList([]*AgentWorkspace{})
	require.Len(t, resp, 0)
}

func TestResolveProvider(t *testing.T) {
	svc := &Service{
		config: ServiceConfig{
			DefaultProvider: ProviderGVisor,
		},
	}

	tests := []struct {
		name     string
		input    string
		expected ProviderType
	}{
		{"explicit firecracker", "firecracker", ProviderFirecracker},
		{"explicit e2b", "e2b", ProviderE2B},
		{"explicit gvisor", "gvisor", ProviderGVisor},
		{"auto", "auto", ProviderGVisor},
		{"empty defaults to gvisor", "", ProviderGVisor},
		{"unknown defaults to gvisor", "invalid", ProviderGVisor},
		{"case insensitive", "Firecracker", ProviderFirecracker},
		{"case insensitive e2b", "E2B", ProviderE2B},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.resolveProvider(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMCPConfig_Fields(t *testing.T) {
	config := MCPConfig{
		Name:          "langfuse-mcp",
		Image:         "emergent/mcp-langfuse:latest",
		StdioBridge:   true,
		RestartPolicy: "always",
		Environment:   map[string]string{"API_KEY": "test-key"},
		Volumes:       []string{"/data"},
	}

	assert.Equal(t, "langfuse-mcp", config.Name)
	assert.Equal(t, "emergent/mcp-langfuse:latest", config.Image)
	assert.True(t, config.StdioBridge)
	assert.Equal(t, "always", config.RestartPolicy)
	assert.Equal(t, "test-key", config.Environment["API_KEY"])
	assert.Contains(t, config.Volumes, "/data")
}

func TestResourceLimits(t *testing.T) {
	limits := ResourceLimits{CPU: "2", Memory: "4G", Disk: "10G"}
	assert.Equal(t, "2", limits.CPU)
	assert.Equal(t, "4G", limits.Memory)
	assert.Equal(t, "10G", limits.Disk)
}
