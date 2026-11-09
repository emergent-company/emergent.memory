package sandbox

// WorkspaceLease is a borrowed handle on a sandbox owned by another live run.
//
// The owning run provisioned the workspace (agent_sandboxes.agent_session_id
// holds its run ID) and is responsible for tearing it down. A spawned child may
// attach to that container instead of provisioning its own: it borrows the lease
// for its own run lifetime, must never destroy the container, and must never
// relink it (the owner stays the workspace's agent_session_id so reconciliation
// keeps keying liveness on the run that actually holds the container).
type WorkspaceLease struct {
	// Workspace is the owner's live workspace row.
	Workspace *AgentSandbox
	// OwnerRunID is the run that provisioned the workspace and owns teardown.
	OwnerRunID string
}
