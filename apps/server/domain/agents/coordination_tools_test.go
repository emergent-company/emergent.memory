package agents

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanReachInternal pins the fail-closed surface gate (issues #939/#954):
// only trusted surfaces may reach internal agents. The signal is the transport
// (TrustedInternal), not the agent's own visibility, and its zero value is
// false (untrusted), so an omitted/unspecified declaration resolves to the
// restrictive case — a transport that forgets to declare itself cannot reach
// internal agents.
func TestCanReachInternal(t *testing.T) {
	require.True(t, canReachInternal(true), "trusted surface must reach internal")
	require.False(t, canReachInternal(false), "external-facing surface must not reach internal")
}

// TestCanReachInternal_ZeroValueIsRestrictive is the fail-first regression for
// the fail-open polarity (issue #954 Finding 2): an unspecified trust marker
// (the bool zero value) must deny internal agents, not grant them.
func TestCanReachInternal_ZeroValueIsRestrictive(t *testing.T) {
	var unspecified bool // the omitted/zero value a future transport would carry
	require.False(t, canReachInternal(unspecified),
		"an omitted trust declaration must resolve to the restrictive (untrusted) case")
}

// TestBuildAgentCatalog_UntrustedHidesInternal is the fail-first regression for
// hole #1: an external-facing caller (TrustedInternal=false) lists agents via
// list_available_agents and must not see internal-visible agents, even under an
// open (empty) spawn policy, while external and project agents remain listed.
func TestBuildAgentCatalog_UntrustedHidesInternal(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "external-agent", Visibility: VisibilityExternal},
		{Name: "project-agent", Visibility: VisibilityProject},
		{Name: "graph-query-agent", Visibility: VisibilityInternal},
	}

	catalog := buildAgentCatalog(defs, nil, false)

	var names []string
	for _, a := range catalog {
		names = append(names, a.Name)
	}
	require.ElementsMatch(t, []string{"external-agent", "project-agent"}, names,
		"an external-facing caller must not see internal agents in list_available_agents")
}

// TestBuildAgentCatalog_TrustedSurfaceSeesInternal confirms legitimate
// coordination is preserved: a trusted surface (session UI, scheduler, MCP,
// agent→agent delegation) still lists internal agents, so internal→internal and
// project→internal delegation keep working.
func TestBuildAgentCatalog_TrustedSurfaceSeesInternal(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "external-agent", Visibility: VisibilityExternal},
		{Name: "graph-query-agent", Visibility: VisibilityInternal},
	}
	catalog := buildAgentCatalog(defs, nil, true)
	var names []string
	for _, a := range catalog {
		names = append(names, a.Name)
	}
	require.ElementsMatch(t, []string{"external-agent", "graph-query-agent"}, names,
		"a trusted surface must still see internal agents")
}

// TestBuildAgentCatalog_RespectsSpawnPolicyAllowlist guards against regressing
// the existing allowlist behaviour while adding the surface filter.
func TestBuildAgentCatalog_RespectsSpawnPolicyAllowlist(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "allowed", Visibility: VisibilityProject},
		{Name: "other", Visibility: VisibilityProject},
	}
	catalog := buildAgentCatalog(defs, []string{"allowed"}, true)
	require.Len(t, catalog, 1)
	require.Equal(t, "allowed", catalog[0].Name)
}

// TestSpawnTargetBlocked_UntrustedRejectsInternal is the fail-first test for the
// spawn half of the hole: an external-facing caller must be rejected from
// spawning an internal-visible target, even one inside its allowlist.
func TestSpawnTargetBlocked_UntrustedRejectsInternal(t *testing.T) {
	def := &AgentDefinition{Name: "graph-query-agent", Visibility: VisibilityInternal}
	reason, blocked := spawnTargetBlocked(false, def)
	require.True(t, blocked, "an external-facing caller must be blocked from spawning internal")
	require.Contains(t, reason, "internal")
}

// TestSpawnTargetBlocked_TrustedAllowsInternal confirms internal→internal and
// project→internal delegation remain possible — "callable only by other agents".
func TestSpawnTargetBlocked_TrustedAllowsInternal(t *testing.T) {
	def := &AgentDefinition{Name: "graph-query-agent", Visibility: VisibilityInternal}
	_, blocked := spawnTargetBlocked(true, def)
	require.False(t, blocked, "a trusted surface must be able to spawn internal agents")
}

// TestSpawnTargetBlocked_UntrustedAllowsNonInternal confirms external-facing
// callers still spawn external and project targets (no over-blocking).
func TestSpawnTargetBlocked_UntrustedAllowsNonInternal(t *testing.T) {
	for _, vis := range []AgentVisibility{VisibilityExternal, VisibilityProject} {
		t.Run(string(vis), func(t *testing.T) {
			def := &AgentDefinition{Name: "target", Visibility: vis}
			_, blocked := spawnTargetBlocked(false, def)
			require.False(t, blocked, "an external-facing caller must spawn %s targets", vis)
		})
	}
}

// TestBuildCoordinationTools_TrustedInternalSurvivesNilDefinition is the
// fail-first test for hole #2: on the A2A resume path the definition can be nil,
// but the transport still marks the run, so the gate stays closed. The
// coordination-tool deps carry the transport signal directly and never fall back
// to a permissive default derived from a nil definition. A nil definition on an
// untrusted transport (TrustedInternal=false) stays untrusted (fail closed).
func TestBuildCoordinationTools_TrustedInternalSurvivesNilDefinition(t *testing.T) {
	ae := &AgentExecutor{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	_, deps, err := ae.buildCoordinationTools(ExecuteRequest{
		ProjectID:       "p",
		AgentDefinition: nil,
		TrustedInternal: true,
	}, "run-1")
	require.NoError(t, err)
	require.NotNil(t, deps, "coordination tools must still be built for a legacy/nil-definition run")
	require.True(t, deps.TrustedInternal,
		"a trusted transport with a nil definition must stay trusted")

	// Untrusted surface defaults false (no inference from a missing definition).
	_, deps2, err := ae.buildCoordinationTools(ExecuteRequest{
		ProjectID:       "p",
		AgentDefinition: nil,
		TrustedInternal: false,
	}, "run-2")
	require.NoError(t, err)
	require.NotNil(t, deps2)
	require.False(t, deps2.TrustedInternal,
		"an untrusted transport with a nil definition must stay untrusted (fail closed)")
}
