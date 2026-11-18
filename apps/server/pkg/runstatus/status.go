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

// Live returns the non-terminal run statuses that still count as live: a run in
// one of them owns its subject work object's single claim slot. Input-required
// is live on purpose — a human answering a question must not have the item
// reclaimed and executed twice (see workLiveRunStatuses in domain/agents).
//
// Use this (or IsLive) instead of comparing status literals: the canonical
// values changed once already (00113), and predicates written against the old
// names silently matched nothing.
func Live() []Status {
	return []Status{Queued, Running, Paused}
}

// LiveStrings returns Live() as plain strings, for query builders (bun.In) and
// comparison against wire values that are not typed as Status.
func LiveStrings() []string {
	live := Live()
	out := make([]string, len(live))
	for i, s := range live {
		out[i] = string(s)
	}
	return out
}

// IsLive reports whether s is one of the Live statuses.
func IsLive(s Status) bool {
	switch s {
	case Queued, Running, Paused:
		return true
	default:
		return false
	}
}
