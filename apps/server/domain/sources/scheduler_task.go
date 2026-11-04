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
