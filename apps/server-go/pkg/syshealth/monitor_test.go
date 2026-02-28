package syshealth

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitor_HealthScoreCalculation(t *testing.T) {
	cfg := DefaultConfig()
	// Adjust thresholds for predictable testing
	cfg.IOWaitWarningPercent = 30.0
	cfg.IOWaitCriticalPercent = 40.0
	cfg.CPULoadWarningFactor = 2.0
	cfg.CPULoadCriticalFactor = 3.0

	log := slog.Default()
	m := NewMonitor(cfg, nil, log).(*sysHealthMonitor)

	// Mock collectors
	m.getCPUCores = func() int { return 4 }
	m.getLoadAvg = func(ctx context.Context) (*load.AvgStat, error) {
		return &load.AvgStat{Load1: 1.0}, nil // 1.0 / 4 = 25% (Safe)
	}
	m.getMemStats = func(ctx context.Context) (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{UsedPercent: 50.0}, nil // 50% (Safe)
	}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		return []cpu.TimesStat{{User: 100, System: 50, Idle: 850, Iowait: 0}}, nil
	}

	// 1. All Safe (Score should be 100)
	m.collect()
	assert.Equal(t, 100, m.metrics.Score)
	assert.Equal(t, HealthZoneSafe, m.metrics.Zone)

	// 2. I/O Warning (35% > 30%)
	// Penalty: 50 * 0.40 = 20. Score: 100 - 20 = 80.
	m.lastCPUTimes = &cpu.TimesStat{}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		// total = 100, iowait = 35 -> 35%
		return []cpu.TimesStat{{User: 50, System: 15, Idle: 0, Iowait: 35}}, nil
	}
	m.collect()
	assert.Equal(t, 80, m.metrics.Score)
	assert.Equal(t, HealthZoneSafe, m.metrics.Zone) // 80 is Safe (67-100)

	// 3. I/O Critical (45% > 40%)
	// Penalty: 100 * 0.40 = 40. Score: 100 - 40 = 60.
	m.lastCPUTimes = &cpu.TimesStat{}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		return []cpu.TimesStat{{User: 50, System: 5, Idle: 0, Iowait: 45}}, nil
	}
	m.collect()
	assert.Equal(t, 60, m.metrics.Score)
	assert.Equal(t, HealthZoneWarning, m.metrics.Zone) // 60 is Warning (34-66)

	// 4. Multiple issues
	// I/O Critical (100 * 0.4 = 40)
	// CPU Warning (Load 9.0 / 4 = 2.25 > 2.0) (50 * 0.3 = 15)
	// Penalty: 40 + 15 = 55. Score: 100 - 55 = 45.
	m.lastCPUTimes = &cpu.TimesStat{}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		return []cpu.TimesStat{{User: 50, System: 5, Idle: 0, Iowait: 45}}, nil
	}
	m.getLoadAvg = func(ctx context.Context) (*load.AvgStat, error) {
		return &load.AvgStat{Load1: 9.0}, nil
	}
	m.collect()
	assert.Equal(t, 45, m.metrics.Score)
	assert.Equal(t, HealthZoneWarning, m.metrics.Zone)

	// 5. Critical Zone (Score <= 33)
	// I/O Critical (40)
	// CPU Critical (Load 13.0 / 4 = 3.25 > 3.0) (100 * 0.3 = 30)
	// Penalty: 40 + 30 = 70. Score: 100 - 70 = 30.
	m.lastCPUTimes = &cpu.TimesStat{}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		return []cpu.TimesStat{{User: 50, System: 5, Idle: 0, Iowait: 45}}, nil
	}
	m.getLoadAvg = func(ctx context.Context) (*load.AvgStat, error) {
		return &load.AvgStat{Load1: 13.0}, nil
	}
	m.collect()
	assert.Equal(t, 30, m.metrics.Score)
	assert.Equal(t, HealthZoneCritical, m.metrics.Zone)
}

func TestMonitor_GracefulDegradation(t *testing.T) {
	cfg := DefaultConfig()
	m := NewMonitor(cfg, nil, slog.Default()).(*sysHealthMonitor)

	// Set initial healthy state
	m.metrics.CPULoadAvg = 1.0
	m.metrics.IOWaitPercent = 5.0
	m.metrics.MemoryPercent = 40.0
	m.metrics.Score = 100

	// Mock failures
	m.getLoadAvg = func(ctx context.Context) (*load.AvgStat, error) {
		return nil, errors.New("failed")
	}
	m.getMemStats = func(ctx context.Context) (*mem.VirtualMemoryStat, error) {
		return nil, errors.New("failed")
	}
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) {
		return nil, errors.New("failed")
	}

	m.collect()

	// Should keep previous values (Task 2.10)
	assert.Equal(t, 1.0, m.metrics.CPULoadAvg)
	assert.Equal(t, 5.0, m.metrics.IOWaitPercent)
	assert.Equal(t, 40.0, m.metrics.MemoryPercent)
	assert.Equal(t, 1, m.consecFailures)

	m.collect()
	m.collect()
	assert.Equal(t, 3, m.consecFailures)
}

func TestMonitor_Staleness(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StalenessThreshold = 100 * time.Millisecond
	m := NewMonitor(cfg, nil, slog.Default()).(*sysHealthMonitor)

	m.metrics.Timestamp = time.Now()
	assert.False(t, m.GetHealth().Stale)

	time.Sleep(150 * time.Millisecond)
	assert.True(t, m.GetHealth().Stale) // Task 2.11
}

func TestMonitor_Lifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CollectionInterval = 10 * time.Millisecond
	m := NewMonitor(cfg, nil, slog.Default()).(*sysHealthMonitor)

	// Mock collection to avoid real calls
	m.getLoadAvg = func(ctx context.Context) (*load.AvgStat, error) { return &load.AvgStat{}, nil }
	m.getCPUTimes = func(ctx context.Context, b bool) ([]cpu.TimesStat, error) { return []cpu.TimesStat{}, nil }
	m.getMemStats = func(ctx context.Context) (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil }

	err := m.Start()
	require.NoError(t, err)
	assert.True(t, m.running)

	// Should be able to call Start again safely
	err = m.Start()
	require.NoError(t, err)

	err = m.Stop()
	require.NoError(t, err)
	assert.False(t, m.running)

	// Should be able to call Stop again safely
	err = m.Stop()
	require.NoError(t, err)
}
