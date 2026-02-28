package syshealth

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

type sysHealthMonitor struct {
	cfg     *Config
	db      bun.IDB
	log     *slog.Logger
	metrics *HealthMetrics
	mu      sync.RWMutex

	ticker  *time.Ticker
	stopCh  chan struct{}
	running bool

	lastCPUTimes   *cpu.TimesStat
	consecFailures int

	// Collection functions for mocking
	getLoadAvg   func(context.Context) (*load.AvgStat, error)
	getCPUTimes  func(context.Context, bool) ([]cpu.TimesStat, error)
	getMemStats  func(context.Context) (*mem.VirtualMemoryStat, error)
	getCPUCores  func() int
}

// NewMonitor creates a new system health monitor.
// cfg: Configuration for the monitor (uses DefaultConfig if nil).
// db: Database connection for pool utilization metrics.
// log: Logger for health events.
func NewMonitor(cfg *Config, db bun.IDB, log *slog.Logger) Monitor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &sysHealthMonitor{
		cfg: cfg,
		db:  db,
		log: log.With(logger.Scope("syshealth.monitor")),
		metrics: &HealthMetrics{
			Score: 100,
			Zone:  HealthZoneSafe,
		},
		getLoadAvg:  load.AvgWithContext,
		getCPUTimes: cpu.TimesWithContext,
		getMemStats: mem.VirtualMemoryWithContext,
		getCPUCores: runtime.NumCPU,
	}
}

func (m *sysHealthMonitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	m.running = true
	m.stopCh = make(chan struct{})
	m.ticker = time.NewTicker(m.cfg.CollectionInterval)

	// Initial collection
	go func() {
		m.collect()
		for {
			select {
			case <-m.ticker.C:
				m.collect()
			case <-m.stopCh:
				return
			}
		}
	}()

	m.log.Info("system health monitor started", slog.Duration("interval", m.cfg.CollectionInterval))
	return nil
}

func (m *sysHealthMonitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false
	m.ticker.Stop()
	close(m.stopCh)
	m.log.Info("system health monitor stopped")
	return nil
}

func (m *sysHealthMonitor) GetHealth() *HealthMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent race conditions
	copy := *m.metrics

	// Check staleness (Task 2.11)
	if time.Since(copy.Timestamp) > m.cfg.StalenessThreshold {
		copy.Stale = true
	}

	return &copy
}

func (m *sysHealthMonitor) collect() {
	// Setup timeout
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.CollectionTimeout)
	defer cancel()

	success := true
	var (
		loadAvg    float64
		ioWait     float64
		memPercent float64
		dbPercent  float64
	)

	// 1. CPU Load Average (Task 2.4)
	if l, err := m.getLoadAvg(ctx); err == nil {
		loadAvg = l.Load1
	} else {
		success = false
		m.log.Error("failed to collect load average", slog.String("error", err.Error()))
	}

	// 2. I/O Wait (Task 2.5)
	if times, err := m.getCPUTimes(ctx, false); err == nil {
		if len(times) > 0 {
			t := times[0]
			if m.lastCPUTimes != nil {
				deltaTotal := t.Total() - m.lastCPUTimes.Total()
				deltaIOWait := t.Iowait - m.lastCPUTimes.Iowait
				if deltaTotal > 0 {
					ioWait = (deltaIOWait / deltaTotal) * 100.0
				}
			}
			m.lastCPUTimes = &t
		} else {
			success = false
			m.log.Error("failed to collect cpu times: no data returned")
		}
	} else {
		success = false
		m.log.Error("failed to collect cpu times", slog.String("error", err.Error()))
	}

	// 3. Memory Utilization (Task 2.6)
	if v, err := m.getMemStats(ctx); err == nil {
		memPercent = v.UsedPercent
	} else {
		success = false
		m.log.Error("failed to collect memory stats", slog.String("error", err.Error()))
	}

	// 4. DB Pool Utilization (Task 2.7)
	if bdb, ok := m.db.(*bun.DB); ok {
		stats := bdb.DB.Stats()
		if stats.MaxOpenConnections > 0 {
			dbPercent = float64(stats.InUse) / float64(stats.MaxOpenConnections) * 100.0
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Graceful degradation (Task 2.10)
	if !success {
		m.consecFailures++
		if m.consecFailures >= 3 {
			// Persistent failure logging (Task 3.4)
			m.log.Error("CRITICAL: persistent metric collection failures", slog.Int("failures", m.consecFailures))
		}
		// Use last known values for any failed metrics implicitly by not overriding if we couldn't fetch,
		// but since we initialize to 0 above, let's keep the existing values if we failed.
		// Actually, let's only update the fields we successfully fetched.
		// For simplicity in this implementation, we will use the old metrics if success == false entirely,
		// or selectively update.
		// We will implement selective update:
		if loadAvg == 0 {
			loadAvg = m.metrics.CPULoadAvg
		}
		if ioWait == 0 {
			ioWait = m.metrics.IOWaitPercent
		}
		if memPercent == 0 {
			memPercent = m.metrics.MemoryPercent
		}
	} else {
		m.consecFailures = 0
	}

	// Calculate scores (Task 2.8)
	cpuCores := float64(m.getCPUCores())
	if cpuCores == 0 {
		cpuCores = 1
	}

	ioScore := calculateComponentScore(ioWait, m.cfg.IOWaitWarningPercent, m.cfg.IOWaitCriticalPercent)
	cpuScore := calculateComponentScore(loadAvg/cpuCores*100.0, m.cfg.CPULoadWarningFactor*100.0, m.cfg.CPULoadCriticalFactor*100.0)
	dbScore := calculateComponentScore(dbPercent, m.cfg.DBPoolWarningPercent, m.cfg.DBPoolCriticalPercent)
	memScore := calculateComponentScore(memPercent, m.cfg.MemoryWarningPercent, m.cfg.MemoryCriticalPercent)

	// Weighted health score
	penalty := (ioScore * 0.40) + (cpuScore * 0.30) + (dbScore * 0.20) + (memScore * 0.10)
	finalScore := 100 - int(penalty)
	if finalScore < 0 {
		finalScore = 0
	}

	// Zone determination (Task 2.9)
	var newZone HealthZone
	if finalScore <= 33 {
		newZone = HealthZoneCritical
	} else if finalScore <= 66 {
		newZone = HealthZoneWarning
	} else {
		newZone = HealthZoneSafe
	}

	// Zone transition logging (Task 3.2)
	if newZone != m.metrics.Zone {
		m.log.Warn("system health zone transition",
			slog.String("old_zone", string(m.metrics.Zone)),
			slog.String("new_zone", string(newZone)),
			slog.Int("score", finalScore))
	}

	// Update metrics
	m.metrics.Score = finalScore
	m.metrics.Zone = newZone
	m.metrics.CPULoadAvg = loadAvg
	m.metrics.IOWaitPercent = ioWait
	m.metrics.MemoryPercent = memPercent
	m.metrics.DBPoolPercent = dbPercent
	m.metrics.Timestamp = time.Now()
	m.metrics.Stale = false

	// Publish Prometheus metrics (Task 9.10)
	HealthScore.WithLabelValues(string(newZone)).Set(float64(finalScore))
	IOWaitPercent.Set(ioWait)
	CPULoadAvg.WithLabelValues("1m").Set(loadAvg)
	MemoryUtilization.Set(memPercent)
	DBPoolUtilization.Set(dbPercent)

	// Periodic logging (Task 3.1)
	m.log.Info("system health metrics collected",
		slog.Int("score", finalScore),
		slog.String("zone", string(newZone)),
		slog.Float64("io_wait", ioWait),
		slog.Float64("cpu_load", loadAvg),
		slog.Float64("db_pool", dbPercent),
		slog.Float64("mem", memPercent))
}

// Helper to calculate 0-100 penalty for a component
func calculateComponentScore(value, warning, critical float64) float64 {
	if value >= critical {
		return 100.0
	}
	if value >= warning {
		return 50.0
	}
	return 0.0
}
