package jobs

// StaleJobMessage is the error text stamped on every job terminal-failed by the
// stale-job sweep (domain/scheduler.StaleJobCleanupTask). It is the single
// source of truth: the sweep writes it and the reporting queries use it to tell
// historical stale-sweep reaps apart from genuine job failures.
//
// This string is part of the persisted job-ledger vocabulary — keep it verbatim.
// Rows that carry it were never attempted by a worker (or were in-flight when a
// sweep mis-classified them, see issue #705); presenting them as current
// failures makes a healthy queue look permanently broken.
const StaleJobMessage = "Job marked as stale during cleanup"
