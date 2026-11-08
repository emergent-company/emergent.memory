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
// exactly once and name every catalog id — but only on surfaces that render
// A2UI (RenderA2UI), never on a text-only surface like agentcompat where a
// card-only reply would strand an empty text delta.
func TestResolveInstructionAppendsA2UICatalogInstruction(t *testing.T) {
	ae := &AgentExecutor{}
	base := "You are a helpful assistant."

	t.Run("injected exactly once on an A2UI surface", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &base}, RenderA2UI: true}
		got := ae.resolveInstruction(req)
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() = %q, want exactly one A2UI instruction block", got)
		}
		if !strings.HasPrefix(got, base) {
			t.Fatalf("resolveInstruction() = %q, want it to start with %q", got, base)
		}
	})

	t.Run("names every id in BasicCatalog", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &base}, RenderA2UI: true}
		got := ae.resolveInstruction(req)
		for id := range a2ui.BasicCatalog {
			if !strings.Contains(got, id) {
				t.Fatalf("resolveInstruction() does not name catalog component %q: %q", id, got)
			}
		}
	})

	t.Run("not duplicated when the prompt already carries it", func(t *testing.T) {
		prompt := base + "\n\n" + a2ui.BasicCatalog.Instruction()
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &prompt}, RenderA2UI: true}
		got := ae.resolveInstruction(req)
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() duplicated the A2UI instruction: %q", got)
		}
	})

	t.Run("not injected on a text-only surface (agentcompat)", func(t *testing.T) {
		// A text-only transport forwards only StreamEventTextDelta, so it must
		// never be told the A2UI catalog: a card-only reply would emit no text
		// delta and leave the response empty (issue #414 regression).
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{SystemPrompt: &base}}
		got := ae.resolveInstruction(req)
		if strings.Contains(got, a2ui.InstructionPrefix) {
			t.Fatalf("resolveInstruction() = %q, want no A2UI instruction on a text-only surface", got)
		}
	})

	t.Run("injected even with no definition when the surface renders A2UI", func(t *testing.T) {
		// RenderA2UI is set by the transport regardless of definition, so
		// legacy/system runs on an A2UI surface still learn the catalog.
		got := ae.resolveInstruction(ExecuteRequest{RenderA2UI: true})
		if strings.Count(got, a2ui.InstructionPrefix) != 1 {
			t.Fatalf("resolveInstruction() = %q, want the A2UI instruction for a nil-definition A2UI run", got)
		}
	})
}
