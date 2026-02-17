package workspace

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WarmPool Config ---

func TestDefaultWarmPoolConfig(t *testing.T) {
	cfg := DefaultWarmPoolConfig()
	assert.Equal(t, 0, cfg.Size)
}

func TestWarmPool_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected bool
	}{
		{"disabled when size=0", 0, false},
		{"enabled when size=1", 1, true},
		{"enabled when size=5", 5, true},
		{"disabled when negative", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: tt.size})
			assert.Equal(t, tt.expected, wp.IsEnabled())
		})
	}
}

// --- Pool disabled behavior ---

func TestWarmPool_DisabledPool_AcquireReturnsNil(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 0})

	result := wp.Acquire(ProviderGVisor)
	assert.Nil(t, result)

	metrics := wp.Metrics()
	assert.Equal(t, int64(0), metrics.Hits)
	assert.Equal(t, int64(1), metrics.Misses)
}

func TestWarmPool_DisabledPool_StartIsNoop(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 0})

	err := wp.Start(t.Context())
	assert.NoError(t, err)
}

func TestWarmPool_DisabledPool_StopIsNoop(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 0})

	err := wp.Stop(t.Context())
	assert.NoError(t, err)
}

// --- Acquire behavior ---

func TestWarmPool_Acquire_Hit(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	// Manually add containers to the pool (bypass Start which needs real providers)
	wp.containers = append(wp.containers, &warmContainer{
		providerID:   "warm-1",
		providerType: ProviderGVisor,
		createdAt:    time.Now(),
	})

	result := wp.Acquire(ProviderGVisor)
	require.NotNil(t, result)
	assert.Equal(t, "warm-1", result.ProviderID())
	assert.Equal(t, ProviderGVisor, result.Provider())

	// Pool should be empty now
	assert.Equal(t, 0, len(wp.containers))

	metrics := wp.Metrics()
	assert.Equal(t, int64(1), metrics.Hits)
	assert.Equal(t, int64(0), metrics.Misses)
}

func TestWarmPool_Acquire_Miss_WrongProvider(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	// Add a gVisor container
	wp.containers = append(wp.containers, &warmContainer{
		providerID:   "warm-gvisor",
		providerType: ProviderGVisor,
		createdAt:    time.Now(),
	})

	// Request a Firecracker container — should miss
	result := wp.Acquire(ProviderFirecracker)
	assert.Nil(t, result)

	// gVisor container should still be in pool
	assert.Equal(t, 1, len(wp.containers))

	metrics := wp.Metrics()
	assert.Equal(t, int64(0), metrics.Hits)
	assert.Equal(t, int64(1), metrics.Misses)
}

func TestWarmPool_Acquire_Miss_EmptyPool(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})
	// Pool is empty — no containers added

	result := wp.Acquire(ProviderGVisor)
	assert.Nil(t, result)

	metrics := wp.Metrics()
	assert.Equal(t, int64(0), metrics.Hits)
	assert.Equal(t, int64(1), metrics.Misses)
}

func TestWarmPool_Acquire_SelectsCorrectProvider(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 3})

	wp.containers = append(wp.containers,
		&warmContainer{providerID: "fc-1", providerType: ProviderFirecracker, createdAt: time.Now()},
		&warmContainer{providerID: "gv-1", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "fc-2", providerType: ProviderFirecracker, createdAt: time.Now()},
	)

	// Request gVisor — should get gv-1 (only gVisor container)
	result := wp.Acquire(ProviderGVisor)
	require.NotNil(t, result)
	assert.Equal(t, "gv-1", result.ProviderID())

	// Should still have 2 Firecracker containers
	assert.Equal(t, 2, len(wp.containers))
}

func TestWarmPool_Acquire_AfterStop(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	wp.containers = append(wp.containers, &warmContainer{
		providerID:   "warm-1",
		providerType: ProviderGVisor,
		createdAt:    time.Now(),
	})

	_ = wp.Stop(t.Context())

	// After stop, acquire should return nil
	result := wp.Acquire(ProviderGVisor)
	assert.Nil(t, result)
}

// --- Metrics ---

func TestWarmPool_Metrics_Initial(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 3})

	metrics := wp.Metrics()
	assert.Equal(t, int64(0), metrics.Hits)
	assert.Equal(t, int64(0), metrics.Misses)
	assert.Equal(t, 0, metrics.PoolSize)
	assert.Equal(t, 3, metrics.TargetSize)
}

func TestWarmPool_Metrics_AfterOperations(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	// Add a container
	wp.containers = append(wp.containers, &warmContainer{
		providerID:   "warm-1",
		providerType: ProviderGVisor,
		createdAt:    time.Now(),
	})

	// Hit
	_ = wp.Acquire(ProviderGVisor)
	// Miss
	_ = wp.Acquire(ProviderGVisor)

	metrics := wp.Metrics()
	assert.Equal(t, int64(1), metrics.Hits)
	assert.Equal(t, int64(1), metrics.Misses)
	assert.Equal(t, 0, metrics.PoolSize)
	assert.Equal(t, 2, metrics.TargetSize)
}

// --- Stop behavior ---

func TestWarmPool_Stop_DoubleStop(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 0})

	err1 := wp.Stop(t.Context())
	assert.NoError(t, err1)

	err2 := wp.Stop(t.Context())
	assert.NoError(t, err2)
}

func TestWarmPool_Stop_ClearsContainers(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	wp.containers = append(wp.containers,
		&warmContainer{providerID: "c1", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "c2", providerType: ProviderGVisor, createdAt: time.Now()},
	)

	// Stop without a real provider — destroyContainer will log a warning but not fail
	_ = wp.Stop(t.Context())
	assert.Equal(t, 0, len(wp.containers))
}

// --- Resize behavior ---

func TestWarmPool_Resize_InvalidSize(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	err := wp.Resize(t.Context(), -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be negative")
}

func TestWarmPool_Resize_DrainToZero(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 3})

	wp.containers = append(wp.containers,
		&warmContainer{providerID: "c1", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "c2", providerType: ProviderGVisor, createdAt: time.Now()},
	)

	err := wp.Resize(t.Context(), 0)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(wp.containers))
	assert.Equal(t, 0, wp.config.Size)
}

func TestWarmPool_Resize_DrainExcess(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 5})

	wp.containers = append(wp.containers,
		&warmContainer{providerID: "c1", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "c2", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "c3", providerType: ProviderGVisor, createdAt: time.Now()},
	)

	err := wp.Resize(t.Context(), 1)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(wp.containers))
	// First container should remain
	assert.Equal(t, "c1", wp.containers[0].providerID)
}

func TestWarmPool_Resize_NoChange(t *testing.T) {
	wp := NewWarmPool(NewOrchestrator(testLogger()), testLogger(), WarmPoolConfig{Size: 2})

	wp.containers = append(wp.containers,
		&warmContainer{providerID: "c1", providerType: ProviderGVisor, createdAt: time.Now()},
		&warmContainer{providerID: "c2", providerType: ProviderGVisor, createdAt: time.Now()},
	)

	err := wp.Resize(t.Context(), 2)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(wp.containers))
}

// --- warmContainer accessors ---

func TestWarmContainer_Accessors(t *testing.T) {
	wc := &warmContainer{
		providerID:   "test-id-123",
		providerType: ProviderFirecracker,
		createdAt:    time.Now(),
	}

	assert.Equal(t, "test-id-123", wc.ProviderID())
	assert.Equal(t, ProviderFirecracker, wc.Provider())
}

// --- Tests with mock provider (uses mockProvider from orchestrator_test.go) ---

func TestWarmPool_Start_WithMockProvider(t *testing.T) {
	orch := NewOrchestrator(testLogger())
	mock := &mockProvider{name: "wp-test", providerType: ProviderGVisor, healthy: true}
	orch.RegisterProvider(ProviderGVisor, mock)

	wp := NewWarmPool(orch, testLogger(), WarmPoolConfig{Size: 3})

	err := wp.Start(t.Context())
	require.NoError(t, err)

	assert.Equal(t, 3, len(wp.containers))
	assert.Equal(t, int64(3), mock.createCount.Load())

	// All containers should be gVisor type
	for _, wc := range wp.containers {
		assert.Equal(t, ProviderGVisor, wc.providerType)
	}

	// Cleanup
	_ = wp.Stop(t.Context())
	assert.Equal(t, int64(3), mock.destroyCount.Load())
}

// failAfterNProvider fails after N successful creates.
type failAfterNProvider struct {
	mockProvider
	maxSuccess int
	count      atomic.Int64
}

func (f *failAfterNProvider) Create(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResult, error) {
	n := f.count.Add(1)
	if int(n) > f.maxSuccess {
		return nil, fmt.Errorf("simulated creation failure")
	}
	return f.mockProvider.Create(ctx, req)
}

func TestWarmPool_Start_PartialFailure(t *testing.T) {
	orch := NewOrchestrator(testLogger())
	failingMock := &failAfterNProvider{
		mockProvider: mockProvider{name: "failing", providerType: ProviderGVisor, healthy: true},
		maxSuccess:   2,
	}
	orch.RegisterProvider(ProviderGVisor, failingMock)

	wp := NewWarmPool(orch, testLogger(), WarmPoolConfig{Size: 3})

	err := wp.Start(t.Context())
	require.NoError(t, err) // Start doesn't return error for partial failure

	assert.Equal(t, 2, len(wp.containers))

	_ = wp.Stop(t.Context())
}

func TestWarmPool_Acquire_TriggersReplenishment(t *testing.T) {
	orch := NewOrchestrator(testLogger())
	mock := &mockProvider{name: "replenish", providerType: ProviderGVisor, healthy: true}
	orch.RegisterProvider(ProviderGVisor, mock)

	wp := NewWarmPool(orch, testLogger(), WarmPoolConfig{Size: 2})

	err := wp.Start(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, len(wp.containers))
	assert.Equal(t, int64(2), mock.createCount.Load())

	// Acquire one
	wc := wp.Acquire(ProviderGVisor)
	require.NotNil(t, wc)

	// Wait for async replenishment (it creates in background)
	time.Sleep(200 * time.Millisecond)

	// Should be back to 2 containers (replenished)
	wp.mu.Lock()
	poolSize := len(wp.containers)
	wp.mu.Unlock()
	assert.Equal(t, 2, poolSize, "pool should replenish back to target size")
	assert.Equal(t, int64(3), mock.createCount.Load(), "should have created 3 total (2 initial + 1 replenish)")

	_ = wp.Stop(t.Context())
}

func TestWarmPool_Resize_GrowWithProvider(t *testing.T) {
	orch := NewOrchestrator(testLogger())
	mock := &mockProvider{name: "grow", providerType: ProviderGVisor, healthy: true}
	orch.RegisterProvider(ProviderGVisor, mock)

	wp := NewWarmPool(orch, testLogger(), WarmPoolConfig{Size: 1})

	err := wp.Start(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, len(wp.containers))

	// Grow to 3
	err = wp.Resize(t.Context(), 3)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(wp.containers))
	assert.Equal(t, int64(3), mock.createCount.Load())

	_ = wp.Stop(t.Context())
}

func TestWarmPool_Resize_ShrinkWithProvider(t *testing.T) {
	orch := NewOrchestrator(testLogger())
	mock := &mockProvider{name: "shrink", providerType: ProviderGVisor, healthy: true}
	orch.RegisterProvider(ProviderGVisor, mock)

	wp := NewWarmPool(orch, testLogger(), WarmPoolConfig{Size: 4})

	err := wp.Start(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, len(wp.containers))

	// Shrink to 1
	err = wp.Resize(t.Context(), 1)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(wp.containers))
	assert.Equal(t, int64(3), mock.destroyCount.Load(), "should destroy 3 excess containers")

	_ = wp.Stop(t.Context())
}

// --- Constants ---

func TestWarmPoolConstants(t *testing.T) {
	assert.Equal(t, 0, defaultWarmPoolSize)
	assert.Equal(t, 60*time.Second, poolResizeTimeout)
	assert.Equal(t, 30*time.Second, replenishTimeout)
}
