package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// staleJobsReapedTotal counts jobs terminal-failed by StaleJobCleanupTask,
// labelled by table. A sudden rise is the dashboard signal that the sweep is
// reaping far more than the occasional genuinely-stuck in-flight job would
// justify (see issue #705); the matching mass-reap log line carries the alert
// marker for log-based alerting.
var staleJobsReapedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scheduler_stale_jobs_reaped_total",
	Help: "Total jobs terminal-failed by the stale job cleanup sweep, by table.",
}, []string{"table"})
