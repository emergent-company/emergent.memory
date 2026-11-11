package sources

import (
	"time"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
)

// syncAdmitInterval is how often the scheduler sweeps for due sources. It is
// intentionally coarse: the sweep is a cheap query over sources with a sync
// interval set, and admission is idempotent (an active job suppresses a
// duplicate).
const syncAdmitInterval = time.Minute

// RegisterSyncSchedulerTask registers a cron task that admits due sources
// through the same single-active-job admission rule as manual triggers.
func RegisterSyncSchedulerTask(sched *scheduler.Scheduler, svc *Service) error {
	if sched == nil {
		return nil
	}
	return sched.AddIntervalTask("source_sync_admit", syncAdmitInterval, svc.AdmitDueSources)
}

// RegisterProbeSchedulerTask registers the proactive grant probe. It is a
// separate task from admission on purpose: admitting a sync and asking whether
// the grant is alive are different decisions with different cadences, and a
// probe must run even when the source is nowhere near due (#23).
func RegisterProbeSchedulerTask(sched *scheduler.Scheduler, svc *Service) error {
	if sched == nil {
		return nil
	}
	return sched.AddIntervalTask("source_grant_probe", probeSweepInterval, svc.ProbeDueSources)
}
