package syshealth

import "time"

// Config holds configuration for the system health monitor
type Config struct {
	// CollectionInterval is how often to collect system metrics
	CollectionInterval time.Duration

	// Thresholds for health scoring
	IOWaitCriticalPercent float64 // Default: 40%
	IOWaitWarningPercent  float64 // Default: 30%
	CPULoadCriticalFactor float64 // Default: 3x CPU count
	CPULoadWarningFactor  float64 // Default: 2x CPU count
	MemoryCriticalPercent float64 // Default: 95%
	MemoryWarningPercent  float64 // Default: 85%
	DBPoolCriticalPercent float64 // Default: 90%
	DBPoolWarningPercent  float64 // Default: 75%

	// Staleness threshold - metrics older than this are considered stale
	StalenessThreshold time.Duration // Default: 2 minutes

	// Metric collection timeout
	CollectionTimeout time.Duration // Default: 5 seconds
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
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
