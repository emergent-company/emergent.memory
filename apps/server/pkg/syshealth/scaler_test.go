package syshealth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockMonitor struct {
	health *HealthMetrics
}

func (m *mockMonitor) Start() error { return nil }
func (m *mockMonitor) Stop() error  { return nil }
func (m *mockMonitor) GetHealth() *HealthMetrics {
	return m.health
}

func TestScaler_Disabled(t *testing.T) {
	monitor := &mockMonitor{health: &HealthMetrics{Zone: HealthZoneCritical}}
	// min=1, max=10, enabled=false
	scaler := NewConcurrencyScaler(monitor, "test", false, 1, 10)

	assert.Equal(t, 5, scaler.GetConcurrency(5))
	assert.Equal(t, 10, scaler.GetConcurrency(10))
}

func TestScaler_Zones(t *testing.T) {
	monitor := &mockMonitor{health: &HealthMetrics{Zone: HealthZoneSafe}}
	scaler := NewConcurrencyScaler(monitor, "test", true, 1, 10)

	// Safe zone -> max (10)
	assert.Equal(t, 10, scaler.GetConcurrency(0))

	// Warning zone -> 50% of max (5)
	monitor.health.Zone = HealthZoneWarning
	// Force adjustment by bypassing cooldown (manually setting lastAdjustment)
	scaler.lastAdjustment = time.Now().Add(-2 * time.Minute)
	assert.Equal(t, 5, scaler.GetConcurrency(0))

	// Critical zone -> min (1)
	monitor.health.Zone = HealthZoneCritical
	assert.Equal(t, 1, scaler.GetConcurrency(0)) // bypasses cooldown
}

func TestScaler_GradualScalingAndCooldown(t *testing.T) {
	monitor := &mockMonitor{health: &HealthMetrics{Zone: HealthZoneSafe}}
	scaler := NewConcurrencyScaler(monitor, "test", true, 2, 20)
	// Initial state: current=20 (max), lastAdj=now

	// 1. Decrease to Warning (20 -> 10)
	monitor.health.Zone = HealthZoneWarning
	// Wait 10s (should NOT adjust, cooldown is 1 min)
	scaler.lastAdjustment = time.Now().Add(-10 * time.Second)
	assert.Equal(t, 20, scaler.GetConcurrency(0))

	// Wait 61s (should adjust)
	scaler.lastAdjustment = time.Now().Add(-61 * time.Second)
	assert.Equal(t, 10, scaler.GetConcurrency(0))

	// 2. Increase back to Safe (10 -> 20)
	monitor.health.Zone = HealthZoneSafe
	// Wait 2 minutes (should NOT adjust, cooldown is 5 min)
	scaler.lastAdjustment = time.Now().Add(-2 * time.Minute)
	assert.Equal(t, 10, scaler.GetConcurrency(0))

	// Wait 5 minutes
	scaler.lastAdjustment = time.Now().Add(-5 * time.Minute)
	// Gradual scaling: max 50% increase. 10 * 1.5 = 15.
	assert.Equal(t, 15, scaler.GetConcurrency(0))

	// Wait another 5 minutes
	scaler.lastAdjustment = time.Now().Add(-5 * time.Minute)
	// 15 * 1.5 = 22.5, but capped at max=20.
	assert.Equal(t, 20, scaler.GetConcurrency(0))
}

func TestScaler_CriticalBypass(t *testing.T) {
	monitor := &mockMonitor{health: &HealthMetrics{Zone: HealthZoneSafe}}
	scaler := NewConcurrencyScaler(monitor, "test", true, 1, 10)

	monitor.health.Zone = HealthZoneCritical
	// Should adjust IMMEDIATELY even if lastAdjustment was 1 second ago
	scaler.lastAdjustment = time.Now().Add(-1 * time.Second)
	assert.Equal(t, 1, scaler.GetConcurrency(0))
}

func TestScaler_StaleHealth(t *testing.T) {
	monitor := &mockMonitor{health: &HealthMetrics{Zone: HealthZoneSafe, Stale: true}}
	scaler := NewConcurrencyScaler(monitor, "test", true, 2, 20)

	// Stale health should treat as Warning (50% of max = 10)
	scaler.lastAdjustment = time.Now().Add(-2 * time.Minute)
	assert.Equal(t, 10, scaler.GetConcurrency(0))
}

func TestScaler_Bounds(t *testing.T) {
	// NewConcurrencyScaler should handle invalid bounds
	scaler := NewConcurrencyScaler(nil, "test", true, 0, 5) // min should become 1
	assert.Equal(t, 1, scaler.minConcurrency)

	scaler = NewConcurrencyScaler(nil, "test", true, 10, 5) // max should become min (10)
	assert.Equal(t, 10, scaler.maxConcurrency)
}
