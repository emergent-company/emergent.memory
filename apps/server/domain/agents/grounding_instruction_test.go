package agents

import (
	"strings"
	"testing"
)

// TestResolveInstructionAppendsGroundingInstruction pins the #245 contract: an
// agent that answers from retrieved memory must be told to stick to its sources,
// admit a gap instead of guessing, and label unmerged-branch content. The agent
// from the report had an empty system prompt, so before this instruction existed
// it received only the citation block — no behavioural guidance at all.
func TestResolveInstructionAppendsGroundingInstruction(t *testing.T) {
	ae := &AgentExecutor{}
	empty := ""

	t.Run("empty system prompt KB agent gets the grounding instruction", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &empty,
				Tools:        []string{"search-hybrid"},
			},
		}
		got := ae.resolveInstruction(req)
		if !strings.Contains(got, groundingInstruction) {
			t.Fatalf("resolveInstruction() did not include grounding instruction: %q", got)
		}
	})

	t.Run("non-KB agent does not get the grounding instruction", func(t *testing.T) {
		base := "You are a helpful assistant."
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Tools:        []string{"web_search"},
			},
		}
		if got := ae.resolveInstruction(req); strings.Contains(got, groundingInstruction) {
			t.Fatalf("resolveInstruction() should not include grounding instruction for non-KB agent: %q", got)
		}
	})

	t.Run("instruction not duplicated when literal prompt already carries it", func(t *testing.T) {
		prompt := "You are a knowledge graph query assistant.\n\n" + groundingInstruction
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &prompt,
				Tools:        []string{"search-hybrid"},
			},
		}
		if got := ae.resolveInstruction(req); strings.Count(got, groundingInstruction) != 1 {
			t.Fatalf("resolveInstruction() should not duplicate the grounding instruction: %q", got)
		}
	})
}
