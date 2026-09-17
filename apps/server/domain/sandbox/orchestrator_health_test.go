package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHealthMonitoringSurvivesParentCancellation verifies that the health monitor
// keeps running for the whole process lifetime, independent of the caller's context.
// It must fail on the pre-fix code where the monitor's select loop watches ctx.Done()
// and would return as soon as the parent is canceled (freezing health at boot).
func TestHealthMonitoringSurvivesParentCancellation(t *testing.T) {
	o := NewOrchestrator(testLogger())
	p := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, p)

	ctx, cancel := context.WithCancel(context.Background())
	o.startHealthMonitoring(ctx, 10*time.Millisecond)
	cancel() // cancel the parent immediately

	// The monitor must keep checking health even though the parent is canceled.
	require.Eventually(t, func() bool {
		return p.healthCount.Load() >= 3
	}, 2*time.Second, 10*time.Millisecond, "health monitor should keep checking after parent context cancellation")

	// Stop must terminate the monitor and be idempotent (no double-close panic).
	o.StopHealthMonitoring()
	o.StopHealthMonitoring()

	// Allow any in-flight check to settle, then confirm no further checks run.
	time.Sleep(50 * time.Millisecond)
	final := p.healthCount.Load()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, final, p.healthCount.Load(), "health monitor should stop after StopHealthMonitoring")
}

// TestListProvidersReportsUnavailableProviders verifies that every known provider is
// reported in a stable order with availability info, including unregistered ones.
func TestListProvidersReportsUnavailableProviders(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, gv)
	o.MarkUnavailable(ProviderFirecracker, "Firecracker", "KVM not available (/dev/kvm missing)")
	o.MarkUnavailable(ProviderE2B, "E2B", "E2B_API_KEY not set")

	providers := o.ListProviders()
	require.Len(t, providers, 3)

	assert.Equal(t, ProviderGVisor, providers[0].Type)
	assert.True(t, providers[0].Registered)
	assert.Equal(t, "gvisor", providers[0].Name)
	assert.NotNil(t, providers[0].Capabilities)

	assert.Equal(t, ProviderFirecracker, providers[1].Type)
	assert.False(t, providers[1].Registered)
	assert.Equal(t, "Firecracker", providers[1].Name)
	assert.Nil(t, providers[1].Capabilities)
	assert.False(t, providers[1].Healthy)
	assert.Equal(t, "KVM not available (/dev/kvm missing)", providers[1].Message)

	assert.Equal(t, ProviderE2B, providers[2].Type)
	assert.False(t, providers[2].Registered)
	assert.Equal(t, "E2B", providers[2].Name)
	assert.Nil(t, providers[2].Capabilities)
	assert.False(t, providers[2].Healthy)
	assert.Equal(t, "E2B_API_KEY not set", providers[2].Message)
}

// TestMarkUnavailableDoesNotRegister verifies that MarkUnavailable only records
// availability state and never adds a provider to the selection pool.
func TestMarkUnavailableDoesNotRegister(t *testing.T) {
	o := NewOrchestrator(testLogger())

	o.MarkUnavailable(ProviderFirecracker, "Firecracker", "KVM not available (/dev/kvm missing)")

	_, err := o.GetProvider(ProviderFirecracker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")

	// A later RegisterProvider wins and is reported as registered + healthy.
	fc := &mockProvider{name: "fc", providerType: ProviderFirecracker, healthy: true}
	o.RegisterProvider(ProviderFirecracker, fc)

	p, err := o.GetProvider(ProviderFirecracker)
	require.NoError(t, err)
	assert.NotNil(t, p)

	// MarkUnavailable must never clobber a registered provider.
	o.MarkUnavailable(ProviderFirecracker, "Firecracker", "should be ignored")

	var fcStatus *ProviderStatusResponse
	for _, status := range o.ListProviders() {
		if status.Type == ProviderFirecracker {
			s := status
			fcStatus = &s
		}
	}
	require.NotNil(t, fcStatus)
	assert.True(t, fcStatus.Registered)
	assert.True(t, fcStatus.Healthy)
}

// TestSelectionErrorEnumeratesReasons verifies that selection errors enumerate the
// per-candidate rejection reasons in deterministic order.
func TestSelectionErrorEnumeratesReasons(t *testing.T) {
	o := NewOrchestrator(testLogger())

	gv := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: false}
	o.RegisterProvider(ProviderGVisor, gv)
	o.checkAllHealth(context.Background())

	o.MarkUnavailable(ProviderFirecracker, "Firecracker", "KVM not available (/dev/kvm missing)")
	o.MarkUnavailable(ProviderE2B, "E2B", "E2B_API_KEY not set")

	_, _, err := o.SelectProviderWithFallback(ContainerTypeAgentSandbox, DeploymentSelfHosted, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy providers available (fallback exhausted)")
	assert.Contains(t, err.Error(), "gvisor: unhealthy: gvisor health")
	assert.Contains(t, err.Error(), "firecracker: not registered: KVM not available (/dev/kvm missing)")
	assert.Contains(t, err.Error(), "e2b: not registered: E2B_API_KEY not set")
}
