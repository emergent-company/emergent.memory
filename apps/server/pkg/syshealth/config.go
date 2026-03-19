package syshealth

import "time"

// Config holds configuration for the system health monitor.
type Config struct {
	// CollectionInterval is how often to collect system metrics (default: 30s).
	CollectionInterval time.Duration

	// IOWaitCriticalPercent is the I/O wait threshold for critical penalty (default: 40%).
	IOWaitCriticalPercent float64
	// IOWaitWarningPercent is the I/O wait threshold for warning penalty (default: 30%).
	IOWaitWarningPercent float64
	// CPULoadCriticalFactor is the load avg multiplier (vs CPU count) for critical penalty (default: 3x).
	CPULoadCriticalFactor float64
	// CPULoadWarningFactor is the load avg multiplier (vs CPU count) for warning penalty (default: 2x).
	CPULoadWarningFactor float64
	// MemoryCriticalPercent is the memory usage threshold for critical penalty (default: 95%).
	MemoryCriticalPercent float64
	// MemoryWarningPercent is the memory usage threshold for warning penalty (default: 85%).
	MemoryWarningPercent float64
	// DBPoolCriticalPercent is the DB pool usage threshold for critical penalty (default: 90%).
	DBPoolCriticalPercent float64
	// DBPoolWarningPercent is the DB pool usage threshold for warning penalty (default: 75%).
	DBPoolWarningPercent float64

	// StalenessThreshold is the time after which metrics are considered stale (default: 2m).
	StalenessThreshold time.Duration

	// CollectionTimeout is the timeout for a single metric collection cycle (default: 5s).
	CollectionTimeout time.Duration
}

// DefaultConfig returns a Config with sensible default values for production use.
func DefaultConfig() *Config {	return &Config{
		CollectionInterval:    30 * time.Second,
		IOWaitCriticalPercent: 40.0,
		IOWaitWarningPercent:  30.0,
		CPULoadCriticalFactor: 3.0,
		CPULoadWarningFactor:  2.0,
		MemoryCriticalPercent: 95.0,
		MemoryWarningPercent:  85.0,
		DBPoolCriticalPercent: 90.0,
		DBPoolWarningPercent:  75.0,
		StalenessThreshold:    2 * time.Minute,
		CollectionTimeout:     5 * time.Second,
	}
}
