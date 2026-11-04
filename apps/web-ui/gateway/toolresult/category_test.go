package toolresult

import (
	"strings"
	"testing"
)

// TestClassifyExact pins the names no prefix describes. These are the tools whose
// family a reader would otherwise have to guess.
func TestClassifyExact(t *testing.T) {
	tests := map[string]Family{
		"remember":                FamilyMemory,
		"forget":                  FamilyMemory,
		"memory-guide":            FamilyMemory,
		"lookup":                  FamilySearch,
		"search_entities":         FamilySearch,
		"set_session_title":       FamilySession,
		"classify-document":       FamilySchemas,
		"finalize-discovery":      FamilySchemas,
		"list-installed-schemas":  FamilySchemas,
		"call_llm":                FamilyAgents,
		"execute_tool":            FamilyAgents,
		"trigger_agent":           FamilyAgents,
		"update_agent":            FamilyAgents,
		"update_agent_definition": FamilyAgents,
		"search_mcp_registry":     FamilyConnector,
		"sync_mcp_server_tools":   FamilyConnector,
		"toggle_mcp_server_tool":  FamilyConnector,
		"update_mcp_server":       FamilyConnector,
		"run_python":              FamilyWorkspace,
		"run_go":                  FamilyWorkspace,
	}
	for name, want := range tests {
		if got := FamilyOf(name); got != want {
			t.Errorf("FamilyOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestClassifyPrefixes covers a representative tool of every family the prefix
// table produces, including the specificity that makes a more specific family win
// over the one it sits inside.
func TestClassifyPrefixes(t *testing.T) {
	tests := map[string]Family{
		"project-record-list": FamilyMemory,
		"graph-traverse":      FamilyMemory,
		"entity-search":       FamilyMemory,
		"tag-list":            FamilyMemory,
		"journal-read":        FamilyMemory,
		"relationship-list":   FamilyMemory,
		"search-hybrid":       FamilySearch,
		"search-semantic":     FamilySearch,
		"schema-list":         FamilySchemas,
		"migration-status":    FamilySchemas,
		"document-get":        FamilyDocuments,
		"embedding-list":      FamilyDocuments,
		"extraction-run":      FamilyDocuments,
		"queue-reextraction":  FamilyDocuments,
		"web-search":          FamilyWeb,
		"ts_web_fetch_page":   FamilyWeb,
		"adk-session-get":     FamilySession,
		"session-list":        FamilySession,
		"agent-run":           FamilyAgents,
		"adk-create":          FamilyAgents,
		"skill-list":          FamilySkills,
		"blueprint-get":       FamilyBlueprints,
		"project-list":        FamilyProjects,
		"org-get":             FamilyProjects,
		"user-profile":        FamilyProjects,
		"trace-get":           FamilyTrace,
		"workspace_write":     FamilyWorkspace,
		"account-key-create":  FamilyConnector,
		"service-token-list":  FamilyConnector,
		"token-create":        FamilyConnector,
		"mcp-server-list":     FamilyConnector,
		"mcp_registry_search": FamilyConnector,
		"provider-list":       FamilyConnector,
		"a2a-send":            FamilyConnector,
	}
	for name, want := range tests {
		if got := FamilyOf(name); got != want {
			t.Errorf("FamilyOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestClassifyUnknown: a tool the table does not know degrades to the generic
// category instead of an identifier a person has to decode.
func TestClassifyUnknown(t *testing.T) {
	for _, name := range []string{"frobnicate", "", "   ", "zz-unknown-tool"} {
		got := Classify(name)
		if got.Family != FamilyTool {
			t.Errorf("Classify(%q).Family = %q, want %q", name, got.Family, FamilyTool)
		}
		if got.Verb != "Used a tool" {
			t.Errorf("Classify(%q).Verb = %q, want %q", name, got.Verb, "Used a tool")
		}
	}
}

// TestClassifyNormalizes: the wire name arrives in any case, with surrounding
// whitespace, and still classifies.
func TestClassifyNormalizes(t *testing.T) {
	got := Classify("  Document-Get\n")
	if got.Family != FamilyDocuments {
		t.Errorf("Family = %q, want %q", got.Family, FamilyDocuments)
	}
}

// TestEveryFamilyHasCopy is the table's integrity check: a family cannot exist
// without both tenses of its verb and an icon, and colour is reserved for state,
// so the icon has to carry the family.
func TestEveryFamilyHasCopy(t *testing.T) {
	families := []Family{
		FamilyMemory, FamilySearch, FamilySchemas, FamilyDocuments, FamilyWeb,
		FamilyAgents, FamilySkills, FamilyBlueprints, FamilyProjects, FamilySession,
		FamilyTrace, FamilyWorkspace, FamilyConnector, FamilyTool,
	}
	for _, family := range families {
		copy, ok := copyFor[family]
		if !ok {
			t.Errorf("family %q has no copy", family)
			continue
		}
		if copy.verb == "" || copy.running == "" {
			t.Errorf("family %q missing a verb: %+v", family, copy)
		}
		if !strings.HasPrefix(copy.icon, "lucide--") {
			t.Errorf("family %q icon = %q, want a lucide class", family, copy.icon)
		}
	}
	// A copy row for a family that does not exist is dead weight.
	for family := range copyFor {
		found := false
		for _, known := range families {
			if family == known {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("copyFor has a row for unknown family %q", family)
		}
	}
}
