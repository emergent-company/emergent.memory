package mcp_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agentcompat"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// fullCatalog returns the catalog with the handler-contributed tools registered,
// which is the catalogue every transport actually serves.
func fullCatalog(t *testing.T) *mcp.Service {
	t.Helper()
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	defs := svc.GetToolDefinitions()
	require.NotEmpty(t, defs)
	return svc
}

// TestCatalogArrayPropertiesDeclareItems pins #2231 item 2: an array property
// without `items` tells a strict MCP client nothing about its element type, so
// the client cannot construct a valid call. Thirteen properties had this shape
// when the gap was found (eleven in service.go, two in domain_tools.go); the
// invariant is asserted over the whole catalog so the next array property cannot
// be added without one.
func TestCatalogArrayPropertiesDeclareItems(t *testing.T) {
	svc := fullCatalog(t)

	var arrays int
	for _, def := range svc.GetToolDefinitions() {
		for property, schema := range def.InputSchema.Properties {
			if schema.Type != "array" {
				continue
			}
			arrays++
			require.NotNil(t, schema.Items,
				"tool %q property %q is an array without items: MCP clients cannot tell what to pass",
				def.Name, property)
			require.NotEmpty(t, schema.Items.Type,
				"tool %q property %q declares items with no type", def.Name, property)
		}
	}
	require.NotZero(t, arrays, "no array properties found — the catalog shape changed")
}

// hiddenBuiltinTools are dispatched by ExecuteTool but deliberately absent from
// the catalog, so an external client cannot discover or call them. They are still
// built-ins: their "memory_<name>" form must stay reserved, and they must not be
// reported as stale entries.
var hiddenBuiltinTools = map[string]bool{
	// Hidden by design: it patches graph-object session titles from inside an
	// agent run (service.go rejects it as a discovered tool).
	"set_session_title": true,
}

// TestInternalToolNamesMatchCatalog pins #2231 item 4. agentcompat's list claims
// to be the canonical set of built-in tool names ("keep in sync with the case
// statements") and nothing checked it: twenty-one entries were missing.
//
// Be precise about the impact. Nothing at runtime consults this map today —
// agentcompat.ValidateClientTools rejects any "memory_"-prefixed client tool
// regardless of the list, so a missing entry cannot let a client shadow a
// built-in. The harm is that a registry documented as canonical is wrong, which
// is what the next caller to trust it inherits. The test makes the claim true
// instead of aspirational, and fails in both directions: a catalogue tool absent
// from the list, and a list entry the catalogue no longer defines.
func TestInternalToolNamesMatchCatalog(t *testing.T) {
	svc := fullCatalog(t)

	catalog := make(map[string]struct{}, len(svc.GetToolDefinitions()))
	// Report every gap in one run: fixing them one failure at a time is how the
	// list drifted in the first place.
	for _, def := range svc.GetToolDefinitions() {
		catalog[def.Name] = struct{}{}
		if !agentcompat.IsInternalTool(def.Name) {
			t.Errorf("catalog tool %q is absent from agentcompat's internalToolNames, which documents itself as the canonical built-in set",
				def.Name)
		}
	}

	for _, name := range agentcompat.InternalToolNames() {
		if _, ok := catalog[name]; ok {
			continue
		}
		if hiddenBuiltinTools[name] {
			continue
		}
		t.Errorf("agentcompat lists internal tool %q, which the MCP catalog does not define; if it was retired, drop it from internalToolNames — if it is still dispatched, add it to hiddenBuiltinTools with a reason",
			name)
	}
}

// TestMergePolicyIsAdvertisedAndEnforced keeps the catalogue honest about what
// graph-branch-merge accepts (#2231 item 3). The enum is derived from the same
// table the service validates against, so a hand-written replacement that drifts
// from the enforced set fails here instead of at a caller's typo.
func TestMergePolicyIsAdvertisedAndEnforced(t *testing.T) {
	svc := fullCatalog(t)

	def := svc.GetToolByName("graph-branch-merge")
	require.NotNil(t, def, "graph-branch-merge must be in the catalog")

	policy, ok := def.InputSchema.Properties["policy"]
	require.True(t, ok, "graph-branch-merge must document a policy parameter")
	require.NotEmpty(t, policy.Enum, "strict MCP clients need the accepted policies as an enum")
	require.Equal(t, graph.MergePolicyNames(), policy.Enum,
		"the advertised policy enum must be the set the service validates against")
}

// TestAssignSchemaToolDeclaresOnlyWiredArgs pins #2375. `schema-assign`
// advertised `enabled_types`/`disabled_types`, which no code path ever read: a
// call that passed them believed it had scoped the assignment, while
// executeAssignSchema enabled every type in the pack and said so nowhere. It
// also hid `force`, which the handler does read — so a caller could not discover
// how to proceed past a dangerous-migration refusal.
//
// The declared set is now exactly what the handler consumes. Adding a knob here
// means wiring it in executeAssignSchema (and in AssignPackRequest, so the REST
// handler shares it) and updating this test; offering a property nothing reads
// is the defect this test exists to catch.
func TestAssignSchemaToolDeclaresOnlyWiredArgs(t *testing.T) {
	svc := fullCatalog(t)

	def := svc.GetToolByName("schema-assign")
	require.NotNil(t, def, "schema-assign must be in the catalog")

	declared := make([]string, 0, len(def.InputSchema.Properties))
	for name := range def.InputSchema.Properties {
		declared = append(declared, name)
	}
	sort.Strings(declared)

	require.Equal(t, []string{"force", "schema_id"}, declared,
		"schema-assign declares %v; executeAssignSchema reads only schema_id and force", declared)
	require.Equal(t, []string{"schema_id"}, def.InputSchema.Required,
		"only schema_id is mandatory; force is the optional escape hatch")
}

// TestSchemaToolDeclaredArgsAreWired pins #2377: every property these two tools
// advertise now has a reader (or a writer, for schema-create), so the catalogue
// stops promising knobs nothing consumes.
//
// schema-list advertised `page` (no reader anywhere) and `include_deprecated`
// (declaration-only) while hiding `offset`, which the handler does read and the
// response echoes. schema-create documented `author`, `ui_configs` and
// `extraction_prompts` that the INSERT never wrote.
//
// Note the direction: advertised must be consumed. `schema-create` also accepts
// the legacy string aliases `object_types`/`relationship_types`, which stay
// accepted for callers that already send them and stay unadvertised — the map
// forms are the documented ones.
func TestSchemaToolDeclaredArgsAreWired(t *testing.T) {
	svc := fullCatalog(t)

	props := func(tool string) []string {
		def := svc.GetToolByName(tool)
		require.NotNil(t, def, "%s must be in the catalog", tool)
		out := make([]string, 0, len(def.InputSchema.Properties))
		for name := range def.InputSchema.Properties {
			out = append(out, name)
		}
		sort.Strings(out)
		return out
	}

	require.Equal(t, []string{"include_deprecated", "limit", "offset", "search"}, props("schema-list"),
		"schema-list must advertise the pagination argument it reads (offset), not page")
	require.Equal(t, []string{
		"author", "description", "extraction_prompts", "name",
		"object_type_schemas", "relationship_type_schemas", "ui_configs", "version",
	}, props("schema-create"),
		"every advertised schema-create property must be written by executeCreateSchema")
}
