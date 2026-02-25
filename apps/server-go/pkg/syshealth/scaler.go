package syshealth

import (
	"math"
	"sync"
	"time"
)

// ConcurrencyScaler adjusts worker concurrency based on system health
type ConcurrencyScaler struct {
	monitor        Monitor
	minConcurrency int
	maxConcurrency int
	enabled        bool
	workerType     string

	// State tracking
	mu                 sync.Mutex
	currentConcurrency int
	lastAdjustment     time.Time
}

// NewConcurrencyScaler creates a new ConcurrencyScaler
func NewConcurrencyScaler(monitor Monitor, workerType string, enabled bool, min, max int) *ConcurrencyScaler {
	// Bounds validation (Task 4.7)
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	// Remove hardcoded max=50 cap to allow higher concurrency for embedding workers
	// Each worker type should specify appropriate max values in their config

	return &ConcurrencyScaler{
		monitor:            monitor,
		workerType:         workerType,
		enabled:            enabled,
		minConcurrency:     min,
		maxConcurrency:     max,
		currentConcurrency: max, // start at max, will scale down if needed
		lastAdjustment:     time.Now(),
	}
}

// GetConcurrency returns the currently allowed concurrency based on health
func (s *ConcurrencyScaler) GetConcurrency(staticValue int) int {
	if !s.enabled {
		return staticValue
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	health := s.monitor.GetHealth()
	now := time.Now()
	timeSinceLastAdj := now.Sub(s.lastAdjustment)

	// Stale health data handling (Task 4.8)
	zone := health.Zone
	if health.Stale {
		zone = HealthZoneWarning
	}

	targetConcurrency := s.currentConcurrency

	// Three-zone scaling rules (Task 4.3)
	switch zone {
	case HealthZoneCritical:
		targetConcurrency = s.minConcurrency
	case HealthZoneWarning:
		// 50% of max
		targetConcurrency = int(math.Max(float64(s.minConcurrency), float64(s.maxConcurrency)*0.5))
	case HealthZoneSafe:
		targetConcurrency = s.maxConcurrency
	}

	// Determine direction and apply cooldowns/gradual limits (Task 4.4, 4.5, 4.6)
	if targetConcurrency < s.currentConcurrency {
		// Decreasing: apply faster (1 min cooldown), bypass if critical
		if zone == HealthZoneCritical {
			// Cooldown bypass (Task 4.6)
			s.currentConcurrency = targetConcurrency
			s.lastAdjustment = now
		} else if timeSinceLastAdj >= 1*time.Minute {
			s.currentConcurrency = targetConcurrency
			s.lastAdjustment = now
		}
	} else if targetConcurrency > s.currentConcurrency {
		// Increasing: wait 5 minutes (Task 4.5), increase by max 50% (Task 4.4)
		if timeSinceLastAdj >= 5*time.Minute {
			maxIncrease := int(math.Max(1.0, float64(s.currentConcurrency)*0.5))
			s.currentConcurrency = int(math.Min(float64(targetConcurrency), float64(s.currentConcurrency+maxIncrease)))
			s.lastAdjustment = now
		}
	}

	// Final safety bounds check
	if s.currentConcurrency < s.minConcurrency {
		s.currentConcurrency = s.minConcurrency
	}
	if s.currentConcurrency > s.maxConcurrency {
		s.currentConcurrency = s.maxConcurrency
	}

	return s.currentConcurrency
}
