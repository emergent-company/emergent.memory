package agents

import (
	"context"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// hookInternalBindingErr returns a non-nil error when binding a webhook hook to
// the given agent would expose an internal-visibility agent on the public
// webhook receiver. POST /api/webhooks/agents/:hookId has no RequireAuth — only
// a per-hook bearer token that is shared with third parties — so a hook bound
// to an internal-visibility agent lets a holder of that secret invoke an agent
// meant to be reachable only from within the platform (issue #1004).
//
// Binding is fail-closed: an internal-visibility target is refused unless the
// caller explicitly opts in (allowInternal). A target whose definition cannot
// be resolved is not internal by definition, so it is allowed.
func hookInternalBindingErr(ctx context.Context, repo *Repository, agent *Agent, allowInternal bool) error {
	if allowInternal {
		return nil
	}
	def, err := repo.ResolveDefinitionForAgent(ctx, agent)
	if err != nil {
		return apperror.NewInternal("failed to resolve agent definition for hook binding", err)
	}
	if def != nil && def.Visibility == VisibilityInternal {
		return apperror.NewForbidden("cannot bind a webhook hook to an internal-visibility agent; the webhook receiver is a public surface")
	}
	return nil
}
