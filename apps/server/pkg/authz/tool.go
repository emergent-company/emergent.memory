// Package authz provides the transport-neutral, fail-closed decision point for
// tool-level authorization. It holds the typed authority vocabulary (a tool's
// declared SuperadminOnly / AgentOnly / RequiredScope markers) and the derived
// caller Principal, and decides allow/deny in exactly one function:
// AuthorizeTool.
//
// The package has no Echo, no DB, and no domain imports: callers adapt their
// tool definition and context principal into the neutral value types below, so
// every transport (HTTP RPC, streamable HTTP, SSE, and the in-process agent-run
// dispatch) can flow through the same decision and never drift.
package authz

import "fmt"

// ToolAuthority is the transport-neutral authority declaration of a tool. It is
// the projection of the three markers an MCP tool definition carries
// (RequiredScope, AgentOnly, SuperadminOnly).
type ToolAuthority struct {
	Name           string
	RequiredScope  string
	AgentOnly      bool
	SuperadminOnly bool
}

// Principal is the derived caller for a tool-authorization decision. Every field
// defaults to its zero value, which resolves fail-closed: an absent trust marker
// is untrusted, an absent platform grant is not-superadmin, and an absent
// transport-enforced marker means the dispatch was not pre-authorized.
type Principal struct {
	// Trusted reports whether the caller is a trusted in-process agent run
	// (session UI / scheduler / MCP-triggered). A trusted run carries the
	// session user's project authority and may exercise scoped + agent-only
	// tools. Token scopes do not exist on the agent-run path, so Trusted is the
	// in-process trust primitive (kb.agent_runs.trusted_internal).
	Trusted bool

	// TransportEnforced reports whether an HTTP transport already enforced the
	// tool's authority before dispatch. Such a dispatch is treated as already
	// authorized and is not re-gated here.
	TransportEnforced bool

	// Platform reports whether the caller holds an active superadmin_full grant.
	// It is the terminal authority tier (a platform principal may do anything).
	Platform bool
}

// AuthorizeTool is the single fail-closed decision point for tool-level
// authorization. It enforces a tool's declared authority in precedence order:
//
//   - SuperadminOnly requires the platform grant (terminal; also short-circuits
//     the agent-only / scope gates, since a platform principal may do anything).
//   - A transport-enforced dispatch was already authorized by the HTTP transport
//     and is not re-gated.
//   - AgentOnly requires a trusted caller.
//   - RequiredScope requires a trusted caller (the in-process trust primitive:
//     token scopes do not exist on the agent-run path, so a trusted run carries
//     the session user's project authority and an untrusted run holds nothing).
//
// It returns nil only when the caller is authorized; any unknown, missing, or
// insufficient authority yields a non-nil error.
func AuthorizeTool(a ToolAuthority, p Principal) error {
	if a.SuperadminOnly {
		if !p.Platform {
			return fmt.Errorf("tool %q requires superadmin privileges", a.Name)
		}
		return nil
	}
	if p.TransportEnforced {
		return nil
	}
	if a.AgentOnly && !p.Trusted {
		return fmt.Errorf("tool %q is agent-only and not reachable from an untrusted surface", a.Name)
	}
	if a.RequiredScope != "" && !p.Trusted {
		if a.RequiredScope == "admin" {
			return fmt.Errorf("tool %q requires admin authority and is not reachable from an untrusted surface", a.Name)
		}
		return fmt.Errorf("tool %q requires scope %q and is not reachable from an untrusted surface", a.Name, a.RequiredScope)
	}
	return nil
}
