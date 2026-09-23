package mcp

import (
	"sort"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// toSet builds a scope set from a slice (for building `want` from pinned lists).
func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// sortedSetKeys returns the keys of a scope set as a sorted slice so two sets
// can be compared order-insensitively (and a same-cardinality swap fails).
func sortedSetKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// assertScopeSetsEqual asserts got is EXACTLY want as a set of scopes.
func assertScopeSetsEqual(t *testing.T, got, want map[string]bool) {
	t.Helper()
	g := sortedSetKeys(got)
	w := sortedSetKeys(want)
	if len(g) != len(w) {
		t.Fatalf("scope set = %v, want exactly %v", g, w)
	}
	for i := range g {
		if g[i] != w[i] {
			t.Fatalf("scope set = %v, want exactly %v", g, w)
		}
	}
}

// expansionTestInputs is the shared input battery for the expansion tests.
func expansionTestInputs() [][]string {
	return [][]string{
		nil,
		{"data:read"},
		{"data:write"},
		{"data:read", "agents:read"},
		{"agents:read"},
		{"agents:write"},
		{"admin:all"},
		{"schema:write"},
		{"projects:write"},
		{"graph:write"},
		{"journal:write"},
		{"skills:write"},
		{"documents:write"},
		{"unknown:scope"},
	}
}

// isExcludedMCPFamily reports whether s belongs to an account/organisation/
// global-admin scope family that the MCP tool surface must never reach.
// Plain "admin" is a legitimate tool RequiredScope and is allowed; "admin:all"
// is the umbrella scope itself (an explicit grant that no implication reaches),
// also allowed. Everything else with an admin:/org:/account: prefix, plus the
// reserved mcp:admin and project:invite:create, is excluded.
func isExcludedMCPFamily(s string) bool {
	switch s {
	case "mcp:admin", "project:invite:create":
		return true
	case "admin", "admin:all":
		return false
	}
	return strings.HasPrefix(s, "admin:") ||
		strings.HasPrefix(s, "org:") ||
		strings.HasPrefix(s, "account:")
}

// TestMCPExpansionIsCanonicalProjectedOntoToolScopes is the derivation-identity
// assertion: expandScopesSet must equal the formula
//
//	{each s in input} ∪ {s : auth.ExpandScopes(input)[s] && mcpToolScopeVocabulary[s]}
//
// for every input. It proves the two views (canonical auth expansion, and the
// MCP vocabulary projection) agree without pinning any concrete value.
func TestMCPExpansionIsCanonicalProjectedOntoToolScopes(t *testing.T) {
	for _, in := range expansionTestInputs() {
		name := "nil"
		if in != nil {
			name = strings.Join(in, ",")
		}
		t.Run(name, func(t *testing.T) {
			want := make(map[string]bool, len(in))
			for _, s := range in {
				want[s] = true
			}
			for s := range auth.ExpandScopes(in) {
				if mcpToolScopeVocabulary[s] {
					want[s] = true
				}
			}
			assertScopeSetsEqual(t, expandScopesSet(in), want)
		})
	}
}

// TestMCPExpansionPinnedToolVisibleSets pins the exact expanded (tool-visible)
// set for each umbrella scope so a change to ScopeImplies or the vocabulary that
// silently widens or narrows the MCP surface must update this test and be
// re-reviewed. Every expected set is a literal — none is derived from the
// implementation formula — so a change that removes or adds an admin-visible
// tool scope cannot update both sides and still pass.
func TestMCPExpansionPinnedToolVisibleSets(t *testing.T) {
	pinned := []struct {
		name  string
		input []string
		want  []string
	}{
		{"data:read", []string{"data:read"}, []string{"data:read", "documents:read", "graph:read", "journal:read", "schema:read", "search"}},
		{"data:write", []string{"data:write"}, []string{"data:write", "documents:write", "graph:write", "journal:write", "schema:write"}},
		{"agents:read", []string{"agents:read"}, []string{"agents:read", "skills:read"}},
		{"agents:write", []string{"agents:write"}, []string{"agents:write", "skills:write"}},
		{"schema:write", []string{"schema:write"}, []string{"schema:write", "schema:migrate"}},
		// admin:all tool-visible surface. Note it does not include chat:use /
		// chat:admin (no MCP tool requires them) or any admin:/org:/account: scope.
		{"admin:all", []string{"admin:all"}, []string{
			"admin", "admin:all", "agents:read", "agents:write",
			"branches:read", "branches:write",
			"documents:read", "documents:write",
			"graph:read", "graph:write",
			"journal:read", "journal:write",
			"schema:migrate", "schema:read", "schema:write",
			"search", "skills:read", "skills:write",
		}},
	}

	for _, tt := range pinned {
		t.Run(tt.name, func(t *testing.T) {
			assertScopeSetsEqual(t, expandScopesSet(tt.input), toSet(tt.want))
		})
	}
}

// TestMCPExpansionNeverReachesExcludedFamilies asserts that no expansion —
// whether from a single umbrella scope key or any battery input — ever surfaces
// an account/organisation/global-admin scope on the MCP tool surface.
func TestMCPExpansionNeverReachesExcludedFamilies(t *testing.T) {
	var inputs [][]string
	for k := range auth.ScopeImplies {
		inputs = append(inputs, []string{k})
	}
	inputs = append(inputs, expansionTestInputs()...)

	for _, in := range inputs {
		name := "nil"
		if in != nil {
			name = strings.Join(in, ",")
		}
		t.Run(name, func(t *testing.T) {
			for s := range expandScopesSet(in) {
				if isExcludedMCPFamily(s) {
					t.Errorf("expansion of %v reached excluded scope %q", in, s)
				}
			}
		})
	}
}

// TestMCPExpansionReconcilesDataWriteToSchemaWrite documents the widened
// reconciliation: data:write surfaces schema:write (and data:read surfaces
// schema:read) on the MCP tool surface. It also pins that expansion is
// single-level — data:write does not transitively reach schema:migrate.
func TestMCPExpansionReconcilesDataWriteToSchemaWrite(t *testing.T) {
	if !expandScopesSet([]string{"data:write"})["schema:write"] {
		t.Fatal("data:write must reconcile to schema:write")
	}
	if !expandScopesSet([]string{"data:read"})["schema:read"] {
		t.Fatal("data:read must reconcile to schema:read")
	}
	if expandScopesSet([]string{"data:write"})["schema:migrate"] {
		t.Fatal("data:write must NOT reach schema:migrate (expansion is single-level)")
	}
}

// TestMCPToolScopeVocabularyCoversCatalog asserts every scope that can gate a
// tool in the package-level catalog (static scope map + dynamicToolBuilders) is
// present in mcpToolScopeVocabulary, so the projection in expandScopesSet never
// drops a scope such a tool requires. The Service here has no agent/registry
// handler wired, so handler-provided tools are not part of this catalog; their
// scopes are covered by the guard tests in domain/agents and domain/mcpregistry,
// which assert every RequiredScope they declare is in the vocabulary via
// mcp.IsToolScope.
func TestMCPToolScopeVocabularyCoversCatalog(t *testing.T) {
	tools := (&Service{}).GetToolDefinitions()
	for _, tool := range tools {
		if tool.RequiredScope == "" {
			continue
		}
		if !mcpToolScopeVocabulary[tool.RequiredScope] {
			t.Errorf("tool %q requires scope %q not present in mcpToolScopeVocabulary", tool.Name, tool.RequiredScope)
		}
	}
}
