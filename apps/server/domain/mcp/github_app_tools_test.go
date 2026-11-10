package mcp

import "testing"

// TestGitHubAppToolsAreInBuiltinCatalog pins the tool-pool fix: the builtin MCP
// server (which backs the in-process agent tool pool) seeds from the internal
// catalog, so the review tools must be static entries there — a
// project-conditional definition never reaches the pool.
func TestGitHubAppToolsAreInBuiltinCatalog(t *testing.T) {
	svc := &Service{}
	byName := map[string]ToolDefinition{}
	for _, def := range svc.GetInternalToolDefinitions() {
		byName[def.Name] = def
	}
	for _, name := range []string{"github-pr-diff", "github-review-submit"} {
		def, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from the builtin internal catalog", name)
		}
		if !def.AgentOnly {
			t.Fatalf("%s must be AgentOnly", name)
		}
		if def.OutputSchema == nil {
			t.Fatalf("%s must declare an output schema", name)
		}
	}
}
