package syshealth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Health monitoring metrics
	HealthScore = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "system_health_score",
		Help: "Overall system health score (0-100)",
	}, []string{"zone"})

	IOWaitPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_io_wait_percent",
		Help: "System I/O wait percentage",
	})

	CPULoadAvg = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "system_cpu_load_avg",
		Help: "System CPU load average",
	}, []string{"period"})

	MemoryUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_memory_utilization_percent",
		Help: "System memory utilization percentage",
	})

	DBPoolUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_db_pool_utilization_percent",
		Help: "Database connection pool utilization percentage",
	})

	// Worker concurrency metrics
	WorkerConcurrency = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "extraction_worker_current_concurrency",
		Help: "Current concurrency level for an extraction worker",
	}, []string{"worker_type"})

	WorkerAdjustments = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "extraction_worker_concurrency_adjustments_total",
		Help: "Total number of concurrency adjustments performed",
	}, []string{"worker_type", "direction", "reason"})

	JobsThrottled = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "extraction_jobs_throttled_total",
		Help: "Total number of jobs throttled due to system health",
	}, []string{"worker_type"})
)
