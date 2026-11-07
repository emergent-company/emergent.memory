package agents

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/v2/tool"
)

// scopeOnlyToolPool supplies only the tool-scope catalog, which is all the
// capability disclosure reads from the tool pool.
type scopeOnlyToolPool struct {
	scopes map[string]string
}

func (p *scopeOnlyToolPool) ResolveTools(string, *AgentDefinition, int, int) ([]tool.Tool, error) {
	return nil, nil
}
func (p *scopeOnlyToolPool) StripOperatorTools(_ context.Context, tools []tool.Tool) []tool.Tool {
	return tools
}
func (p *scopeOnlyToolPool) ToolScopes() map[string]string { return p.scopes }
func (p *scopeOnlyToolPool) CallTool(context.Context, string, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestResolveInstructionCapabilityDisclosure pins the #236 contract: the agent
// must be told, before it answers, which capability groups it actually has, and
// must never claim a save it cannot make. The report: an agent with every tool
// group disabled answered that it had saved a fact; the fact was gone in a new
// chat.
func TestResolveInstructionCapabilityDisclosure(t *testing.T) {
	ae := &AgentExecutor{}
	empty := ""

	t.Run("all tool groups disabled says no write capability", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &empty,
				Tools:        nil,
			},
		}
		got := ae.resolveInstruction(req)
		t.Logf("composed instruction (all disabled): %s", got)
		if !strings.Contains(got, "Capabilities: the tool groups enabled for this agent are: none.") {
			t.Fatalf("resolveInstruction() did not list the (empty) capability groups: %q", got)
		}
		if !strings.Contains(got, "No write-capable tool group is enabled") {
			t.Fatalf("resolveInstruction() did not disclose the missing write capability: %q", got)
		}
		if !strings.Contains(got, "say plainly that you cannot because no write tool is enabled") {
			t.Fatalf("resolveInstruction() did not tell the agent how to answer a save request: %q", got)
		}
	})

	t.Run("session builtin does not count as write capability", func(t *testing.T) {
		// In production the catalog always carries set_session_title, which is
		// injected and default-on even when every capability group is switched
		// off. It must not make the agent believe it can persist a fact.
		scoped := &AgentExecutor{toolPool: &scopeOnlyToolPool{scopes: map[string]string{
			"set_session_title": "",
			"entity-create":     "graph:write",
			"search-hybrid":     "search",
		}}}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &empty,
				Tools:        nil,
			},
		}
		got := scoped.resolveInstruction(req)
		if !strings.Contains(got, "No write-capable tool group is enabled") {
			t.Fatalf("resolveInstruction() claimed write capability on a fully disabled agent: %q", got)
		}
	})

	t.Run("graph write enabled reflects it and drops the cannot-save wording", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &empty,
				Tools:        []string{"entity-create"},
			},
		}
		got := ae.resolveInstruction(req)
		t.Logf("composed instruction (graph write): %s", got)
		if !strings.Contains(got, "Graph · Write") {
			t.Fatalf("resolveInstruction() did not name the enabled Graph · Write group: %q", got)
		}
		if strings.Contains(got, "cannot save") || strings.Contains(got, "no write tool is enabled") {
			t.Fatalf("resolveInstruction() kept the cannot-save wording for a write-capable agent: %q", got)
		}
		if !strings.Contains(got, "Only report that something was saved, remembered, created or updated after a write-capable tool call actually executed") {
			t.Fatalf("resolveInstruction() dropped the no-fabricated-save rule: %q", got)
		}
	})

	t.Run("read-only tools still get the capability list", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &empty,
				Tools:        []string{"search-hybrid", "entity-query"},
			},
		}
		got := ae.resolveInstruction(req)
		if !strings.Contains(got, "Search") || !strings.Contains(got, "Graph · Read") {
			t.Fatalf("resolveInstruction() did not list the enabled read groups: %q", got)
		}
		if !strings.Contains(got, "No write-capable tool group is enabled") {
			t.Fatalf("resolveInstruction() did not disclose the missing write capability: %q", got)
		}
	})

	t.Run("block is not duplicated when the prompt already carries it", func(t *testing.T) {
		def := &AgentDefinition{SystemPrompt: &empty, Tools: []string{"entity-create"}}
		block := capabilityDisclosureInstruction(def, nil)
		prompt := "You are a helpful assistant.\n\n" + block
		req := ExecuteRequest{AgentDefinition: def}
		req.AgentDefinition.SystemPrompt = &prompt
		got := ae.resolveInstruction(req)
		if strings.Count(got, capabilityInstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() duplicated the capability block: %q", got)
		}
	})

	t.Run("no disclosure when the definition is unknown", func(t *testing.T) {
		got := ae.resolveInstruction(ExecuteRequest{})
		if strings.Contains(got, capabilityInstructionPrefix) {
			t.Fatalf("resolveInstruction() disclosed capabilities with no definition: %q", got)
		}
	})
}
