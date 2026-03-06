package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name              string
	providerType      ProviderType
	healthy           bool
	createErr         error
	destroyErr        error
	snapshotErr       error
	fromSnapshotErr   error
	snapshotID        string // returned by Snapshot()
	supportsSnapshots bool
	createCount       atomic.Int64
	destroyCount      atomic.Int64
}

func (m *mockProvider) Create(_ context.Context, _ *CreateContainerRequest) (*CreateContainerResult, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	id := fmt.Sprintf("mock-%s-%d", m.name, m.createCount.Add(1))
	return &CreateContainerResult{ProviderID: id}, nil
}
func (m *mockProvider) Destroy(_ context.Context, _ string) error {
	m.destroyCount.Add(1)
	return m.destroyErr
}
func (m *mockProvider) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockProvider) Resume(_ context.Context, _ string) error { return nil }
func (m *mockProvider) Exec(_ context.Context, _ string, _ *ExecRequest) (*ExecResult, error) {
	return &ExecResult{ExitCode: 0}, nil
}
func (m *mockProvider) ReadFile(_ context.Context, _ string, _ *FileReadRequest) (*FileReadResult, error) {
	return &FileReadResult{}, nil
}
func (m *mockProvider) WriteFile(_ context.Context, _ string, _ *FileWriteRequest) error {
	return nil
}
func (m *mockProvider) ListFiles(_ context.Context, _ string, _ *FileListRequest) (*FileListResult, error) {
	return &FileListResult{}, nil
}
func (m *mockProvider) Snapshot(_ context.Context, _ string) (string, error) {
	if m.snapshotErr != nil {
		return "", m.snapshotErr
	}
	if !m.supportsSnapshots {
		return "", ErrSnapshotNotSupported
	}
	if m.snapshotID != "" {
		return m.snapshotID, nil
	}
	return fmt.Sprintf("snap-%s-%d", m.name, m.createCount.Load()), nil
}
func (m *mockProvider) CreateFromSnapshot(_ context.Context, snapshotID string, _ *CreateContainerRequest) (*CreateContainerResult, error) {
	if m.fromSnapshotErr != nil {
		return nil, m.fromSnapshotErr
	}
	if !m.supportsSnapshots {
		return nil, ErrSnapshotNotSupported
	}
	id := fmt.Sprintf("mock-%s-from-%s-%d", m.name, snapshotID, m.createCount.Add(1))
	return &CreateContainerResult{ProviderID: id}, nil
}
func (m *mockProvider) Health(_ context.Context) (*HealthStatus, error) {
	return &HealthStatus{Healthy: m.healthy, Message: m.name + " health"}, nil
}
func (m *mockProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{
		Name:              m.name,
		ProviderType:      m.providerType,
		SupportsSnapshots: m.supportsSnapshots,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestOrchestratorRegisterDeregister(t *testing.T) {
	o := NewOrchestrator(testLogger())

	mock := &mockProvider{name: "test-gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, mock)

	providers := o.ListProviders()
	require.Len(t, providers, 1)
	assert.Equal(t, ProviderGVisor, providers[0].Type)
	assert.Equal(t, "test-gvisor", providers[0].Name)

	o.DeregisterProvider(ProviderGVisor)
	providers = o.ListProviders()
	assert.Len(t, providers, 0)
}

func TestOrchestratorSelectExplicit(t *testing.T) {
	o := NewOrchestrator(testLogger())
	gvisor := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gvisor)

	p, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderGVisor)
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorSelectExplicitNotFound(t *testing.T) {
	o := NewOrchestrator(testLogger())

	_, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderFirecracker)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestOrchestratorSelectExplicitUnhealthy(t *testing.T) {
	o := NewOrchestrator(testLogger())
	gvisor := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: false}
	o.RegisterProvider(ProviderGVisor, gvisor)

	// Run health check to update status
	o.checkAllHealth(context.Background())

	_, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderGVisor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unhealthy")
}

func TestOrchestratorAutoSelectSelfHostedWorkspace(t *testing.T) {
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "fc", providerType: ProviderFirecracker, healthy: true}
	gv := &mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	// Self-hosted workspace prefers Firecracker
	p, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderFirecracker, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorAutoSelectSelfHostedMCP(t *testing.T) {
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "fc", providerType: ProviderFirecracker, healthy: true}
	gv := &mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	// Self-hosted MCP prefers gVisor
	p, pt, err := o.SelectProvider(ContainerTypeMCPServer, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorAutoSelectManaged(t *testing.T) {
	o := NewOrchestrator(testLogger())

	e2b := &mockProvider{name: "e2b", providerType: ProviderE2B, healthy: true}
	gv := &mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderE2B, e2b)
	o.RegisterProvider(ProviderGVisor, gv)

	// Managed workspace prefers E2B
	p, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentManaged, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderE2B, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorFallbackOnUnhealthy(t *testing.T) {
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "fc", providerType: ProviderFirecracker, healthy: false}
	gv := &mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true}

	o.RegisterProvider(ProviderFirecracker, fc)
	o.RegisterProvider(ProviderGVisor, gv)

	// Update health status
	o.checkAllHealth(context.Background())

	// Self-hosted workspace prefers Firecracker, but it's unhealthy → fallback to gVisor
	p, pt, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorFallbackNoExplicit(t *testing.T) {
	o := NewOrchestrator(testLogger())

	fc := &mockProvider{name: "fc", providerType: ProviderFirecracker, healthy: false}
	o.RegisterProvider(ProviderFirecracker, fc)
	o.checkAllHealth(context.Background())

	// Explicit provider request — no fallback even if unhealthy
	_, _, err := o.SelectProviderWithFallback(ContainerTypeAgentWorkspace, DeploymentSelfHosted, ProviderFirecracker)
	assert.Error(t, err)
}

func TestOrchestratorFallbackAuto(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)

	// Auto selection — only gVisor available, should work
	p, pt, err := o.SelectProviderWithFallback(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "auto")
	require.NoError(t, err)
	assert.Equal(t, ProviderGVisor, pt)
	assert.NotNil(t, p)
}

func TestOrchestratorNoProviders(t *testing.T) {
	o := NewOrchestrator(testLogger())

	_, _, err := o.SelectProvider(ContainerTypeAgentWorkspace, DeploymentSelfHosted, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy providers")
}

func TestBuildSelectionChain(t *testing.T) {
	o := NewOrchestrator(testLogger())

	tests := []struct {
		name           string
		containerType  ContainerType
		deploymentMode DeploymentMode
		expected       []ProviderType
	}{
		{
			"self-hosted workspace",
			ContainerTypeAgentWorkspace,
			DeploymentSelfHosted,
			[]ProviderType{ProviderFirecracker, ProviderGVisor, ProviderE2B},
		},
		{
			"self-hosted MCP",
			ContainerTypeMCPServer,
			DeploymentSelfHosted,
			[]ProviderType{ProviderGVisor, ProviderFirecracker, ProviderE2B},
		},
		{
			"managed workspace",
			ContainerTypeAgentWorkspace,
			DeploymentManaged,
			[]ProviderType{ProviderE2B, ProviderFirecracker, ProviderGVisor},
		},
		{
			"managed MCP",
			ContainerTypeMCPServer,
			DeploymentManaged,
			[]ProviderType{ProviderGVisor, ProviderE2B, ProviderFirecracker},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := o.buildSelectionChain(tt.containerType, tt.deploymentMode)
			assert.Equal(t, tt.expected, chain)
		})
	}
}

func TestOrchestratorGetProvider(t *testing.T) {
	o := NewOrchestrator(testLogger())
	gv := &mockProvider{name: "gv", providerType: ProviderGVisor}
	o.RegisterProvider(ProviderGVisor, gv)

	p, err := o.GetProvider(ProviderGVisor)
	require.NoError(t, err)
	assert.NotNil(t, p)

	_, err = o.GetProvider(ProviderFirecracker)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestOrchestratorHealthCheck(t *testing.T) {
	o := NewOrchestrator(testLogger())

	healthy := &mockProvider{name: "healthy", providerType: ProviderGVisor, healthy: true}
	unhealthy := &mockProvider{name: "unhealthy", providerType: ProviderFirecracker, healthy: false}

	o.RegisterProvider(ProviderGVisor, healthy)
	o.RegisterProvider(ProviderFirecracker, unhealthy)

	o.checkAllHealth(context.Background())

	o.mu.RLock()
	defer o.mu.RUnlock()

	assert.True(t, o.health[ProviderGVisor].Healthy)
	assert.False(t, o.health[ProviderFirecracker].Healthy)
}

// Verify Provider interface is implemented by mockProvider
var _ Provider = (*mockProvider)(nil)

// Suppress unused import warning for fmt
var _ = fmt.Sprint
