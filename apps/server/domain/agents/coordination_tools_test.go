package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanReachInternal pins the visibility gate that closes the
// external→internal A2A reachability hole (issue #939): only `external`
// callers are denied internal agents, because `external` is the sole level
// advertised in the A2A agent card. `project` and `internal` callers are not
// A2A-facing and may coordinate internal agents.
func TestCanReachInternal(t *testing.T) {
	cases := []struct {
		caller AgentVisibility
		want   bool
	}{
		{caller: VisibilityExternal, want: false},
		{caller: VisibilityProject, want: true},
		{caller: VisibilityInternal, want: true},
	}
	for _, tc := range cases {
		t.Run(string(tc.caller), func(t *testing.T) {
			require.Equal(t, tc.want, canReachInternal(tc.caller))
		})
	}
}

// TestCallerVisibility pins the default: a nil definition (legacy agent with no
// A2A card) is treated as project visibility, never external.
func TestCallerVisibility(t *testing.T) {
	require.Equal(t, VisibilityProject, callerVisibility(nil))
	require.Equal(t, VisibilityExternal, callerVisibility(&AgentDefinition{Visibility: VisibilityExternal}))
	require.Equal(t, VisibilityInternal, callerVisibility(&AgentDefinition{Visibility: VisibilityInternal}))
}

// TestBuildAgentCatalog_ExternalCallerHidesInternal is the fail-first regression
// test for issue #939: an external-facing caller's list_available_agents catalog
// must not contain internal-visible agents, even under an open (empty) spawn
// policy, while external and project agents remain reachable.
func TestBuildAgentCatalog_ExternalCallerHidesInternal(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "external-agent", Visibility: VisibilityExternal},
		{Name: "project-agent", Visibility: VisibilityProject},
		{Name: "graph-query-agent", Visibility: VisibilityInternal},
	}

	catalog := buildAgentCatalog(defs, nil, VisibilityExternal)

	var names []string
	for _, a := range catalog {
		names = append(names, a.Name)
	}
	require.ElementsMatch(t, []string{"external-agent", "project-agent"}, names,
		"an external caller must not see internal agents in list_available_agents")
}

// TestBuildAgentCatalog_InternalAndProjectCallersSeeInternal confirms legitimate
// coordination is preserved: non-external callers still list internal agents, so
// internal→internal and project→internal delegation keep working.
func TestBuildAgentCatalog_InternalAndProjectCallersSeeInternal(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "external-agent", Visibility: VisibilityExternal},
		{Name: "graph-query-agent", Visibility: VisibilityInternal},
	}
	for _, caller := range []AgentVisibility{VisibilityInternal, VisibilityProject} {
		t.Run(string(caller), func(t *testing.T) {
			catalog := buildAgentCatalog(defs, nil, caller)
			var names []string
			for _, a := range catalog {
				names = append(names, a.Name)
			}
			require.ElementsMatch(t, []string{"external-agent", "graph-query-agent"}, names,
				"a %s caller must still see internal agents", caller)
		})
	}
}

// TestBuildAgentCatalog_RespectsSpawnPolicyAllowlist guards against regressing
// the existing allowlist behaviour while adding the visibility filter.
func TestBuildAgentCatalog_RespectsSpawnPolicyAllowlist(t *testing.T) {
	defs := []*AgentDefinition{
		{Name: "allowed", Visibility: VisibilityProject},
		{Name: "other", Visibility: VisibilityProject},
	}
	catalog := buildAgentCatalog(defs, []string{"allowed"}, VisibilityExternal)
	require.Len(t, catalog, 1)
	require.Equal(t, "allowed", catalog[0].Name)
}

// TestSpawnTargetBlocked_ExternalCallerRejectsInternal is the fail-first test
// for the spawn half of issue #939: an external-facing caller must be rejected
// from spawning an internal-visible target.
func TestSpawnTargetBlocked_ExternalCallerRejectsInternal(t *testing.T) {
	def := &AgentDefinition{Name: "graph-query-agent", Visibility: VisibilityInternal}
	reason, blocked := spawnTargetBlocked(VisibilityExternal, def)
	require.True(t, blocked, "an external caller must be blocked from spawning internal")
	require.Contains(t, reason, "internal")
}

// TestSpawnTargetBlocked_InternalCallerAllowsInternal confirms internal→internal
// delegation remains possible — "callable only by other agents".
func TestSpawnTargetBlocked_InternalCallerAllowsInternal(t *testing.T) {
	def := &AgentDefinition{Name: "graph-query-agent", Visibility: VisibilityInternal}
	_, blocked := spawnTargetBlocked(VisibilityInternal, def)
	require.False(t, blocked, "an internal caller must be able to spawn internal agents")
}

// TestSpawnTargetBlocked_ProjectCallerAllowsInternal confirms project→internal
// delegation (a non-A2A-facing agent) remains possible.
func TestSpawnTargetBlocked_ProjectCallerAllowsInternal(t *testing.T) {
	def := &AgentDefinition{Name: "graph-query-agent", Visibility: VisibilityInternal}
	_, blocked := spawnTargetBlocked(VisibilityProject, def)
	require.False(t, blocked, "a project caller must be able to spawn internal agents")
}

// TestSpawnTargetBlocked_ExternalCallerAllowsProjectAndExternal confirms the fix
// does not break ordinary coordination: external callers may still spawn
// external and project targets.
func TestSpawnTargetBlocked_ExternalCallerAllowsProjectAndExternal(t *testing.T) {
	for _, vis := range []AgentVisibility{VisibilityExternal, VisibilityProject} {
		t.Run(string(vis), func(t *testing.T) {
			def := &AgentDefinition{Name: "target", Visibility: vis}
			_, blocked := spawnTargetBlocked(VisibilityExternal, def)
			require.False(t, blocked, "an external caller must be able to spawn %s targets", vis)
		})
	}
}
