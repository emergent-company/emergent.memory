package cmd

import (
	"slices"
	"strings"
	"testing"
)

func toolSet(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// TestMissingRequiredMCPTools_FullCatalog guards #2005: a current server
// advertises the kebab-case catalog plus dozens of tools the CLI does not know
// about, and none of that may be reported as a mismatch.
func TestMissingRequiredMCPTools_FullCatalog(t *testing.T) {
	actual := toolSet(requiredMCPTools...)
	// Tools the CLI has never tracked must not make the check fail.
	for _, extra := range []string{
		"agent-create", "agent-def-list", "blueprint-apply", "document-upload",
		"skill-list", "session-todo-list", "schema-migrate-preview", "project-briefing",
	} {
		actual[extra] = true
	}

	if missing := missingRequiredMCPTools(actual); len(missing) != 0 {
		t.Fatalf("missingRequiredMCPTools() = %v, want none", missing)
	}
}

// TestMissingRequiredMCPTools_LegacyCatalog pins the behaviour that produced
// the original report: a server on the old snake_case catalog is a version-skew
// signal, so every required tool is reported missing.
func TestMissingRequiredMCPTools_LegacyCatalog(t *testing.T) {
	actual := toolSet(
		"schema_version", "list_entity_types", "query_entities", "search_entities",
		"get_entity_edges", "list_schemas", "get_schema", "create_entity",
		"create_relationship", "update_entity", "delete_entity", "hybrid_search",
		"semantic_search", "find_similar", "traverse_graph", "list_relationships",
		"update_relationship", "delete_relationship", "restore_entity",
		"batch_create_entities", "batch_create_relationships", "list_tags",
	)

	missing := missingRequiredMCPTools(actual)
	if len(missing) != len(requiredMCPTools) {
		t.Fatalf("missingRequiredMCPTools() = %v, want all %d required tools", missing, len(requiredMCPTools))
	}
	if !slices.Equal(missing, requiredMCPTools) {
		t.Errorf("missing tools order = %v, want %v", missing, requiredMCPTools)
	}
}

func TestMissingRequiredMCPTools_Partial(t *testing.T) {
	actual := toolSet("schema-version", "entity-query", "not-a-real-tool")

	missing := missingRequiredMCPTools(actual)

	for _, present := range []string{"schema-version", "entity-query"} {
		if slices.Contains(missing, present) {
			t.Errorf("%q is advertised but reported missing: %v", present, missing)
		}
	}
	if !slices.Contains(missing, "remember") {
		t.Errorf("remember is not advertised and must be reported missing, got %v", missing)
	}
	if len(missing) != len(requiredMCPTools)-2 {
		t.Errorf("missing %d tools, want %d: %v", len(missing), len(requiredMCPTools)-2, missing)
	}
}

func TestMissingRequiredMCPTools_Empty(t *testing.T) {
	if missing := missingRequiredMCPTools(nil); len(missing) != len(requiredMCPTools) {
		t.Fatalf("missingRequiredMCPTools(nil) returned %d tools, want %d", len(missing), len(requiredMCPTools))
	}
}

// TestRequiredMCPToolsPinCore pins the exact membership: the required list is
// a promise about which tools every supported server exposes, not just a
// sample. Removing a name here must be a deliberate change to this test.
func TestRequiredMCPToolsPinCore(t *testing.T) {
	want := []string{
		"entity-create", "entity-query", "entity-search", "forget",
		"graph-traverse", "relationship-create", "remember",
		"schema-get", "schema-list", "schema-version",
		"search-hybrid", "search-semantic",
	}
	if !slices.Equal(requiredMCPTools, want) {
		t.Fatalf("requiredMCPTools = %v, want %v", requiredMCPTools, want)
	}
}

// TestRequiredMCPToolsStable keeps the reported list deterministic and free of
// duplicates, and documents the naming convention (kebab-case, server catalog).
func TestRequiredMCPToolsStable(t *testing.T) {
	if !slices.IsSorted(requiredMCPTools) {
		t.Errorf("requiredMCPTools must be sorted for deterministic doctor output: %v", requiredMCPTools)
	}
	seen := make(map[string]bool, len(requiredMCPTools))
	for _, name := range requiredMCPTools {
		if seen[name] {
			t.Errorf("duplicate required tool %q", name)
		}
		seen[name] = true
		if strings.Contains(name, "_") {
			t.Errorf("required tool %q must use the kebab-case name the server advertises", name)
		}
	}
}
