package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/authz"
)

// TestRunDispatchContextStripsTransportEnforced is the fail-first regression for
// the #1133 privilege escalation: an HTTP transport marks its dispatch
// TransportEnforced (per-dispatch evidence that ONE ExecuteTool call was already
// authorized), and that marker must NOT be inherited by a nested agent run.
// runDispatchContext is the agent-run boundary — it must strip the marker and
// stamp the run's own trust, so a child run's tools are re-gated on its own
// trusted_internal rather than the parent's per-dispatch authorization.
func TestRunDispatchContextStripsTransportEnforced(t *testing.T) {
	// Untrusted child (external surface) crossing the boundary from an HTTP dispatch.
	ctx := runDispatchContext(mcp.ContextWithTransportEnforced(context.Background()), false)
	require.False(t, mcp.TransportEnforcedFromContext(ctx),
		"an HTTP transport's per-dispatch authorization must not survive the run boundary")
	require.False(t, mcp.TrustedInternalFromContext(ctx),
		"an untrusted child run must be stamped untrusted")

	// Trusted child (session UI / scheduler) keeps its trust, still no marker.
	ctx = runDispatchContext(mcp.ContextWithTransportEnforced(context.Background()), true)
	require.False(t, mcp.TransportEnforcedFromContext(ctx),
		"a trusted child run must also drop the transport-enforced marker")
	require.True(t, mcp.TrustedInternalFromContext(ctx),
		"a trusted child run must keep its trust")
}

// TestRunBoundaryDeniesUntrustedChildAgentOnlyAndScoped ties the strip to the
// enforcement seam: after crossing the run boundary, an untrusted child's
// principal carries neither trust nor transport-enforced evidence, so
// authz.AuthorizeTool — the single fail-closed decision point ExecuteTool uses —
// must deny BOTH an agent-only tool and a scoped tool. Without the strip this
// test fails: the stale TransportEnforced marker would short-circuit the gate.
func TestRunBoundaryDeniesUntrustedChildAgentOnlyAndScoped(t *testing.T) {
	ctx := runDispatchContext(mcp.ContextWithTransportEnforced(context.Background()), false)
	principal := authz.Principal{
		Trusted:           mcp.TrustedInternalFromContext(ctx),
		TransportEnforced: mcp.TransportEnforcedFromContext(ctx),
	}

	require.Error(t, authz.AuthorizeTool(authz.ToolAuthority{
		Name:      "mcp-server-create",
		AgentOnly: true,
	}, principal), "an untrusted child run must be denied an agent-only tool")

	require.Error(t, authz.AuthorizeTool(authz.ToolAuthority{
		Name:          "agent-create",
		RequiredScope: "agent:write",
	}, principal), "an untrusted child run must be denied a scoped tool")
}
