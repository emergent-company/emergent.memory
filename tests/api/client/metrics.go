package client

import (
	"sort"
	"sync"
	"time"
)

// RequestMetric holds timing data for a single request.
type RequestMetric struct {
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	Timestamp  time.Time
}

// MetricsCollector collects and aggregates request metrics.
type MetricsCollector struct {
	mu       sync.Mutex
	requests []RequestMetric
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		requests: make([]RequestMetric, 0, 1000),
	}
}

// Record adds a request metric.
func (m *MetricsCollector) Record(metric RequestMetric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, metric)
}

// Reset clears all metrics.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = m.requests[:0]
}

// Count returns the number of recorded requests.
func (m *MetricsCollector) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// All returns a copy of all recorded metrics.
func (m *MetricsCollector) All() []RequestMetric {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]RequestMetric, len(m.requests))
	copy(result, m.requests)
	return result
}

// Durations returns all request durations sorted.
func (m *MetricsCollector) Durations() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()

	durations := make([]time.Duration, len(m.requests))
	for i, r := range m.requests {
		durations[i] = r.Duration
	}
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})
	return durations
}

// Percentile returns the p-th percentile of request durations.
// p should be between 0 and 100.
func (m *MetricsCollector) Percentile(p float64) time.Duration {
	durations := m.Durations()
	if len(durations) == 0 {
		return 0
	}

	idx := int(float64(len(durations)-1) * p / 100.0)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	return durations[idx]
}

// P50 returns the 50th percentile (median).
func (m *MetricsCollector) P50() time.Duration {
	return m.Percentile(50)
}

// P95 returns the 95th percentile.
func (m *MetricsCollector) P95() time.Duration {
	return m.Percentile(95)
}

// P99 returns the 99th percentile.
func (m *MetricsCollector) P99() time.Duration {
	return m.Percentile(99)
}

// Min returns the minimum request duration.
func (m *MetricsCollector) Min() time.Duration {
	durations := m.Durations()
	if len(durations) == 0 {
		return 0
	}
	return durations[0]
}

// Max returns the maximum request duration.
func (m *MetricsCollector) Max() time.Duration {
	durations := m.Durations()
	if len(durations) == 0 {
		return 0
	}
	return durations[len(durations)-1]
}

// Avg returns the average request duration.
func (m *MetricsCollector) Avg() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.requests) == 0 {
		return 0
	}

	var total time.Duration
	for _, r := range m.requests {
		total += r.Duration
	}
	return total / time.Duration(len(m.requests))
}

// Summary returns a summary of all collected metrics.
type MetricsSummary struct {
	TotalRequests int           `json:"total_requests"`
	MinDuration   time.Duration `json:"min_duration"`
	MaxDuration   time.Duration `json:"max_duration"`
	AvgDuration   time.Duration `json:"avg_duration"`
	P50Duration   time.Duration `json:"p50_duration"`
	P95Duration   time.Duration `json:"p95_duration"`
	P99Duration   time.Duration `json:"p99_duration"`
	ByStatus      map[int]int   `json:"by_status"`
}

// Summary returns aggregated metrics.
func (m *MetricsCollector) Summary() MetricsSummary {
	m.mu.Lock()
	requests := make([]RequestMetric, len(m.requests))
	copy(requests, m.requests)
	m.mu.Unlock()

	summary := MetricsSummary{
		TotalRequests: len(requests),
		ByStatus:      make(map[int]int),
	}

	if len(requests) == 0 {
		return summary
	}

	summary.MinDuration = m.Min()
	summary.MaxDuration = m.Max()
	summary.AvgDuration = m.Avg()
	summary.P50Duration = m.P50()
	summary.P95Duration = m.P95()
	summary.P99Duration = m.P99()

	for _, r := range requests {
		summary.ByStatus[r.StatusCode]++
	}

	return summary
}
