package agents

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// delegationDeniedByTypeScope reports whether delegating to def would widen the
// caller's graph type scope. The caller's scope is read from ctx (set once per
// run by runPipeline from the caller's own definition); a nil/unrestricted
// caller permits any target. A nil def is treated as unrestricted, so a scoped
// caller refuses delegating to an agent with no allowlist.
//
// This is the single guard for every sub-agent hand-off (trigger_agent,
// spawn_agents, call_agent). A scoped parent may only delegate to a target whose
// declared allowlists are a subset of its own, so a child run — which derives its
// scope from its own definition — can never reach a type the parent cannot
// (issue #1695).
func delegationDeniedByTypeScope(ctx context.Context, def *AgentDefinition) bool {
	scope := mcp.TypeScopeFromContext(ctx)
	if scope == nil {
		return false
	}
	var objectTypes, relationshipTypes []string
	if def != nil {
		objectTypes = def.ObjectTypes()
		relationshipTypes = def.RelationshipTypes()
	}
	return !scope.PermitsDelegation(objectTypes, relationshipTypes)
}

// delegationScopeError is the uniform rejection message for a delegation that
// would widen the caller's graph type scope (#1695).
func delegationScopeError() string {
	return "delegation denied: target agent's graph type scope is broader than the caller's"
}
