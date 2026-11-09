package agents

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// workspaceLeaseKey is the context key carrying the sandbox lease (if any) that
// the current run holds. runPipeline installs it after provisioning so that
// coordination tools spawned by this run can hand the same workspace to their
// children instead of each provisioning a container.
type workspaceLeaseKey struct{}

// ContextWithWorkspaceLease attaches a sandbox lease to the context. A nil lease
// is stored explicitly (rather than skipped) so a run with no workspace clears
// any lease inherited from the delegating run; otherwise a sandbox-less child
// would re-publish its parent's lease to its own children.
func ContextWithWorkspaceLease(ctx context.Context, lease *sandbox.WorkspaceLease) context.Context {
	return context.WithValue(ctx, workspaceLeaseKey{}, lease)
}

// WorkspaceLeaseFromContext returns the sandbox lease carried by the context, or
// nil when the run has no live workspace to share.
func WorkspaceLeaseFromContext(ctx context.Context) *sandbox.WorkspaceLease {
	lease, _ := ctx.Value(workspaceLeaseKey{}).(*sandbox.WorkspaceLease)
	return lease
}
