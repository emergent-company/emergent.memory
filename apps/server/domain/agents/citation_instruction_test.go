package agents

import (
	"strings"
	"testing"
)

func TestResolveInstructionAppendsCitationInstruction(t *testing.T) {
	ae := &AgentExecutor{}
	base := "You are a knowledge graph query assistant."

	t.Run("KB-backed agent gets the citation instruction", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Tools:        []string{"search-hybrid", "entity-query"},
			},
		}
		got := ae.resolveInstruction(req)
		if !strings.Contains(got, citationInstruction) {
			t.Fatalf("resolveInstruction() did not include citation instruction: %q", got)
		}
		if !strings.HasPrefix(got, base) {
			t.Fatalf("resolveInstruction() = %q, want it to start with %q", got, base)
		}
	})

	t.Run("non-KB agent does not get the citation instruction", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Tools:        []string{"web_search"},
			},
		}
		got := ae.resolveInstruction(req)
		if strings.Contains(got, citationInstruction) {
			t.Fatalf("resolveInstruction() should not include citation instruction for non-KB agent: %q", got)
		}
	})

	t.Run("instruction not duplicated when literal prompt already carries it", func(t *testing.T) {
		// graphQueryAgentSystemPrompt already ends with the instruction.
		if !strings.Contains(graphQueryAgentSystemPrompt, citationInstruction) {
			t.Fatalf("graphQueryAgentSystemPrompt should contain the citation instruction")
		}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: strPtr(graphQueryAgentSystemPrompt),
				Tools:        []string{"search-hybrid"},
			},
		}
		got := ae.resolveInstruction(req)
		if strings.Count(got, citationInstruction) != 1 {
			t.Fatalf("resolveInstruction() should not duplicate the citation instruction: %q", got)
		}
	})
}
