package syshealth

import "time"

// HealthZone represents the current health status zone (safe, warning, or critical).
type HealthZone string

const (
	// HealthZoneCritical indicates severe resource pressure (score 0-33).
	HealthZoneCritical HealthZone = "critical"
	// HealthZoneWarning indicates moderate resource pressure (score 34-66).
	HealthZoneWarning HealthZone = "warning"
	// HealthZoneSafe indicates healthy resource utilization (score 67-100).
	HealthZoneSafe HealthZone = "safe"
)

// HealthMetrics holds the current system health metrics and calculated score.
type HealthMetrics struct {
	// Score is the overall health score (0-100, higher is healthier).
	Score int

	// Zone is the current health zone derived from the score.
	Zone HealthZone

	// CPULoadAvg is the 1-minute CPU load average.
	CPULoadAvg float64
	// IOWaitPercent is the I/O wait percentage (0-100).
	IOWaitPercent float64
	// MemoryPercent is the memory utilization percentage (0-100).
	MemoryPercent float64
	// DBPoolPercent is the database connection pool utilization (0-100).
	DBPoolPercent float64

	// Timestamp is when these metrics were collected.
	Timestamp time.Time
	// Stale indicates if metrics are older than the staleness threshold.
	Stale bool
}

// Monitor is the interface for system health monitoring services.
type Monitor interface {
	// Start begins the background health monitoring loop.
	Start() error

	// Stop gracefully stops the background health monitoring loop.
	Stop() error

	// GetHealth returns the latest collected health metrics.
	GetHealth() *HealthMetrics
}
