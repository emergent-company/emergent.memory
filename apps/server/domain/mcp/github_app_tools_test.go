package mcp

import (
	"context"
	"testing"
)

// singleReviewProvider is an inert GitHubAppToolHandler for wiring tests.
type singleReviewProvider struct{}

func (singleReviewProvider) HandlesGitHubAppTool(string) bool { return false }

func (singleReviewProvider) ExecuteGitHubAppTool(context.Context, string, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}

// TestGitHubAppReviewHandlerRegistrationRejectsSecondProvider pins the single
// owner rule: moving the executor to sources must not leave two providers able
// to claim github-pr-diff / github-review-submit. A second registration panics at
// startup instead of silently replacing the first (#579).
func TestGitHubAppReviewHandlerRegistrationRejectsSecondProvider(t *testing.T) {
	svc := &Service{}
	svc.RegisterGitHubAppToolHandler(singleReviewProvider{})

	defer func() {
		if recover() == nil {
			t.Fatal("second GitHubAppToolHandler registration must fail loudly")
		}
	}()
	svc.RegisterGitHubAppToolHandler(singleReviewProvider{})
}

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
