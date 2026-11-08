package agents

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// TestResolveInstructionAppendsA2UICatalogInstruction pins the #414 contract: the
// A2UI emission path depended on the model writing a ```a2ui fence that no
// server-side instruction described. The catalog instruction (rendered from
// a2ui.BasicCatalog, the same map the validator reads) must now be present
// exactly once and name every catalog id.
func TestResolveInstructionAppendsA2UICatalogInstruction(t *testing.T) {
	ae := &AgentExecutor{}
	base := "You are a helpful assistant."

	t.Run("injected exactly once", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &base}}
		got := ae.resolveInstruction(req)
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() = %q, want exactly one A2UI instruction block", got)
		}
		if !strings.HasPrefix(got, base) {
			t.Fatalf("resolveInstruction() = %q, want it to start with %q", got, base)
		}
	})

	t.Run("names every id in BasicCatalog", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &base}}
		got := ae.resolveInstruction(req)
		for id := range a2ui.BasicCatalog {
			if !strings.Contains(got, id) {
				t.Fatalf("resolveInstruction() does not name catalog component %q: %q", id, got)
			}
		}
	})

	t.Run("not duplicated when the prompt already carries it", func(t *testing.T) {
		prompt := base + "\n\n" + a2ui.BasicCatalog.Instruction()
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &prompt}}
		got := ae.resolveInstruction(req)
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() duplicated the A2UI instruction: %q", got)
		}
	})

	t.Run("injected even with no definition (surfaces are always enabled)", func(t *testing.T) {
		// A2UI extraction is unconditional, so the instruction is injected for
		// legacy/system runs too, not just definition-backed agents.
		got := ae.resolveInstruction(ExecuteRequest{})
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() = %q, want the A2UI instruction for a nil-definition run", got)
		}
	})
}
