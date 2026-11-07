package mcp

import (
	"strings"
	"testing"
)

// catalogService is a Service whose agent handler contributes the full agent
// tool set, which is what every transport serves.
func catalogService() *Service {
	return &Service{agentToolHandler: &scopedAgentToolHandler{defs: agentToolDefs()}}
}

// TestConsolidatedCatalogShape pins the invariants of the consolidated catalog:
// every family has at least two actions (a one-action family is churn, not a
// merge), names are unique, and the action enum matches the declared actions.
func TestConsolidatedCatalogShape(t *testing.T) {
	seen := make(map[string]bool, len(consolidatedFamilies))
	handlers := make(map[string]string, len(toolAliases))
	for _, fam := range consolidatedFamilies {
		if seen[fam.Name] {
			t.Errorf("duplicate consolidated tool %q", fam.Name)
		}
		seen[fam.Name] = true
		if fam.Description == "" {
			t.Errorf("%s: empty description", fam.Name)
		}
		// No family may be named after one of its own handlers. Such a name is
		// ambiguous: a share allowlist entry would have to mean either the
		// pre-merge per-verb tool or the whole family, and a new share could not
		// grant the family's other actions by selecting the advertised name.
		for _, act := range fam.Actions {
			if act.Handler == fam.Name {
				t.Errorf("%s: family is named after its own handler %q; keep that tool standalone", fam.Name, act.Handler)
			}
		}
		// A family either exposes several actions or folds an alternate entry
		// point into one action (agent-question list by run vs by project). A
		// family with neither is churn, not a merge.
		folded := 0
		for _, act := range fam.Actions {
			folded += len(act.Fold)
		}
		if len(fam.Actions) < 2 && folded == 0 {
			t.Errorf("%s: consolidated family must have at least two actions (or fold an alternate name), has %d", fam.Name, len(fam.Actions))
		}
		actionSeen := make(map[string]bool, len(fam.Actions))
		for _, act := range fam.Actions {
			if actionSeen[act.Action] {
				t.Errorf("%s: duplicate action %q", fam.Name, act.Action)
			}
			actionSeen[act.Action] = true
			if act.Handler == "" {
				t.Errorf("%s: action %q has no handler", fam.Name, act.Action)
			}
			if act.Description == "" {
				t.Errorf("%s.%s: empty action description", fam.Name, act.Action)
			}
			if prev, ok := handlers[act.Handler]; ok {
				t.Errorf("handler %q is claimed by both %s and %s", act.Handler, prev, fam.Name)
			}
			handlers[act.Handler] = fam.Name
		}
	}
}

// TestEveryLegacyAliasResolves proves the alias table is complete and
// consistent: each legacy name maps to a real consolidated tool, to a real
// action of that tool, and to a handler that the legacy catalog still defines
// (and therefore a dispatch case the switch serves).
func TestEveryLegacyAliasResolves(t *testing.T) {
	svc := catalogService()
	legacy := svc.legacyToolDefinitions()
	legacyNames := make(map[string]bool, len(legacy))
	for _, def := range legacy {
		legacyNames[def.Name] = true
	}
	advertised := svc.GetToolDefinitions()
	advertisedNames := make(map[string]bool, len(advertised))
	for _, def := range advertised {
		advertisedNames[def.Name] = true
	}

	for name, alias := range toolAliases {
		if alias.Tool == name {
			t.Errorf("%s is an alias of its own family; the shape test forbids that", name)
			continue
		}
		if !advertisedNames[alias.Tool] {
			t.Errorf("alias %q maps to %q, which the catalog does not advertise", name, alias.Tool)
		}
		if !legacyNames[name] {
			t.Errorf("alias %q is not a tool the legacy catalog defines", name)
		}

		handler, ok, err := resolveConsolidatedHandler(alias.Tool, map[string]any{"action": alias.Action})
		if err != nil || !ok {
			t.Errorf("%s action %q does not resolve: ok=%v err=%v", alias.Tool, alias.Action, ok, err)
			continue
		}
		if !legacyNames[handler] {
			t.Errorf("%s action %q dispatches to %q, which is not a legacy catalog tool", alias.Tool, alias.Action, handler)
		}
		if got := legacyDispatchName(name, nil); got != handler {
			t.Errorf("legacyDispatchName(%q) = %q, want %q", name, got, handler)
		}
	}
}

// TestAdvertisedCatalogHasNoFoldedNames proves the merge actually happens: no
// name that the alias table folds into another tool is advertised.
func TestAdvertisedCatalogHasNoFoldedNames(t *testing.T) {
	svc := catalogService()
	for _, def := range svc.GetToolDefinitions() {
		if alias, ok := toolAliases[def.Name]; ok && alias.Tool != def.Name {
			t.Errorf("advertised tool %q is a deprecated alias of %q and must not be listed", def.Name, alias.Tool)
		}
	}
}

// TestConsolidatedToolUnionsLegacyParams proves no parameter is lost in a
// merge: every property a legacy member declared is present on the consolidated
// tool, and `action` is the only required parameter (each action's own
// requirements are enforced by its handler).
func TestConsolidatedToolUnionsLegacyParams(t *testing.T) {
	svc := catalogService()
	legacy := make(map[string]ToolDefinition)
	for _, def := range svc.legacyToolDefinitions() {
		legacy[def.Name] = def
	}

	advertised := make(map[string]ToolDefinition, len(consolidatedFamilies))
	for _, def := range svc.GetToolDefinitions() {
		advertised[def.Name] = def
	}

	for _, fam := range consolidatedFamilies {
		def, ok := advertised[fam.Name]
		if !ok {
			t.Errorf("%s is not advertised", fam.Name)
			continue
		}
		if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "action" {
			t.Errorf("%s required = %v, want [action]", fam.Name, def.InputSchema.Required)
		}
		action, ok := def.InputSchema.Properties["action"]
		if !ok {
			t.Errorf("%s has no action property", fam.Name)
			continue
		}
		wantActions := make([]string, 0, len(fam.Actions))
		for _, act := range fam.Actions {
			wantActions = append(wantActions, act.Action)
			for _, handler := range append([]string{act.Handler}, act.Fold...) {
				src, ok := legacy[handler]
				if !ok {
					continue
				}
				for prop := range src.InputSchema.Properties {
					if prop == "action" {
						continue
					}
					if _, ok := def.InputSchema.Properties[prop]; !ok {
						t.Errorf("%s lost parameter %q declared by %s", fam.Name, prop, handler)
					}
				}
			}
		}
		if strings.Join(action.Enum, ",") != strings.Join(wantActions, ",") {
			t.Errorf("%s action enum = %v, want %v", fam.Name, action.Enum, wantActions)
		}
	}
}

// TestFamilyInheritsStrictestAuthority pins the rule that makes a merge safe: a
// family carries the strictest authority of the tools it folds, so folding an
// operator-only or agent-only tool can never widen who may call the family.
func TestFamilyInheritsStrictestAuthority(t *testing.T) {
	fam := &consolidatedFamily{
		Name:        "synthetic",
		Description: "synthetic family",
		Actions: []consolidatedAction{
			{Action: "read", Handler: "synthetic-read", Description: "read"},
			{Action: "admin", Handler: "synthetic-admin", Description: "admin"},
		},
	}
	byName := map[string]*ToolDefinition{
		"synthetic-read":  {Name: "synthetic-read"},
		"synthetic-admin": {Name: "synthetic-admin", SuperadminOnly: true, AgentOnly: true},
	}
	def, ok := buildConsolidatedDefinition(fam, byName)
	if !ok {
		t.Fatal("synthetic family did not build")
	}
	if !def.SuperadminOnly {
		t.Error("family must inherit SuperadminOnly from a folded operator tool")
	}
	if !def.AgentOnly {
		t.Error("family must inherit AgentOnly from a folded agent tool")
	}
}

// TestConsolidatedDispatchContract pins the two error shapes a consolidated
// call can hit before any handler runs, so a typo is a clear error rather than a
// wrong action.
func TestConsolidatedDispatchContract(t *testing.T) {
	if _, ok, err := resolveConsolidatedHandler("entity", map[string]any{}); err == nil || ok == false {
		t.Errorf("missing action must fail: ok=%v err=%v", ok, err)
	} else if !strings.Contains(err.Error(), "'action' is required") {
		t.Errorf("missing action error = %v", err)
	}

	if _, ok, err := resolveConsolidatedHandler("entity", map[string]any{"action": "nope"}); err == nil || !ok {
		t.Errorf("unknown action must fail: ok=%v err=%v", ok, err)
	} else if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("unknown action error = %v", err)
	}

	// Only `action` is the discriminator. `view` and `strategy` are parameters
	// of legacy tools (forget, entity-query, search-*), so reading them as an
	// action synonym would misroute a legacy call that sets one.
	for _, key := range []string{"view", "strategy"} {
		if _, ok, err := resolveConsolidatedHandler("schema", map[string]any{key: "icons"}); err == nil || !ok {
			t.Errorf("%s=icons must not be read as an action: ok=%v err=%v", key, ok, err)
		}
	}
	// The legacy call that carries such a parameter still resolves through its
	// own name, and its per-verb dispatch name is unchanged.
	if got := legacyDispatchName("forget", map[string]any{"strategy": "auto"}); got != "forget" {
		t.Errorf("legacyDispatchName(forget, strategy=auto) = %q, want forget", got)
	}

	// A name that is not consolidated passes through untouched.
	if handler, ok, err := resolveConsolidatedHandler("entity-search", nil); err != nil || ok || handler != "entity-search" {
		t.Errorf("non-consolidated name must pass through: handler=%q ok=%v err=%v", handler, ok, err)
	}

	// `remember` is deliberately not a family. Its name is also the name of one
	// of its handlers, so a family would make a share allowlist entry ambiguous:
	// `["remember"]` would either grant the whole family (including the
	// soft-delete) or only the ingest action, and no allowlist could express the
	// difference. It stays a standalone tool, so `remember {message}` keeps its
	// old call shape and `forget` stays separately grantable.
	if _, ok, err := resolveConsolidatedHandler("remember", map[string]any{"message": "x"}); ok || err != nil {
		t.Errorf("remember must not be advertised as a family: ok=%v err=%v", ok, err)
	}
	if got := legacyDispatchName("remember", map[string]any{"message": "x"}); got != "remember" {
		t.Errorf("legacyDispatchName(remember) = %q, want remember", got)
	}
	if got := legacyDispatchName("forget", map[string]any{"action": "forget"}); got != "forget" {
		t.Errorf("legacyDispatchName(forget) = %q, want forget", got)
	}
}

// TestInternalCatalogKeepsPerVerbNames proves the two surfaces stay separate:
// the advertised catalog is consolidated, while the in-process agent catalog
// keeps the per-verb names its whitelists, bans and enable flags are keyed on.
func TestInternalCatalogKeepsPerVerbNames(t *testing.T) {
	svc := catalogService()
	internal := svc.GetInternalToolDefinitions()
	internalNames := make(map[string]bool, len(internal))
	for _, def := range internal {
		internalNames[def.Name] = true
	}
	if !internalNames["entity-search"] {
		t.Error("internal catalog must keep entity-search")
	}
	if internalNames["entity"] {
		t.Error("internal catalog must not contain the consolidated entity tool")
	}

	advertised := svc.GetToolDefinitions()
	for _, def := range advertised {
		if def.Name == "entity-search" {
			t.Error("advertised catalog must not contain entity-search")
		}
	}
}
