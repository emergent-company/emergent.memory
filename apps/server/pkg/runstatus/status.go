// Package runstatus defines the canonical agent run status values shared by the
// agents domain and the scheduler.
//
// It lives in a dependency-neutral package so that domain/scheduler can filter
// agent runs by terminal status without importing domain/agents, which would
// create an import cycle (domain/agents imports domain/scheduler to register
// its cron tasks).
package runstatus

// Status is the lifecycle status of an agent run.
type Status string

const (
	Queued     Status = "submitted"      // enqueued, waiting for a worker
	Running    Status = "working"        // actively executing
	Success    Status = "completed"      // finished successfully
	Skipped    Status = "skipped"        // skipped (e.g. agent disabled / no-op)
	Error      Status = "failed"         // finished with an error
	Paused     Status = "input-required" // suspended awaiting user input
	Cancelled  Status = "cancelled"      // cancelled by the caller
	Cancelling Status = "cancelling"     // ACP two-step cancel: intent acknowledged, awaiting execution stop
)

// Terminal returns the run statuses that mean the run has reached a terminal
// state and its session data is eligible for cleanup.
func Terminal() []Status {
	return []Status{Success, Skipped, Error, Cancelled}
}
