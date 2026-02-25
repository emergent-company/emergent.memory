package syshealth

import "time"

// HealthZone represents the current health status zone
type HealthZone string

const (
	// HealthZoneCritical indicates severe resource pressure (score 0-33)
	HealthZoneCritical HealthZone = "critical"
	// HealthZoneWarning indicates moderate resource pressure (score 34-66)
	HealthZoneWarning HealthZone = "warning"
	// HealthZoneSafe indicates healthy resource utilization (score 67-100)
	HealthZoneSafe HealthZone = "safe"
)

// HealthMetrics holds the current system health metrics and calculated score
type HealthMetrics struct {
	// Overall health score (0-100, higher is healthier)
	Score int

	// Current health zone
	Zone HealthZone

	// Individual metric values
	CPULoadAvg    float64 // 1-minute load average
	IOWaitPercent float64 // I/O wait percentage (0-100)
	MemoryPercent float64 // Memory utilization percentage (0-100)
	DBPoolPercent float64 // Database connection pool utilization (0-100)

	// Metadata
	Timestamp time.Time // When these metrics were collected
	Stale     bool      // True if metrics are older than staleness threshold
}

// Monitor is the interface for system health monitoring
type Monitor interface {
	// Start begins the health monitoring loop
	Start() error

	// Stop gracefully stops the health monitoring loop
	Stop() error

	// GetHealth returns the current health metrics
	GetHealth() *HealthMetrics
}
