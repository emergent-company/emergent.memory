package agents

import (
	"strings"
	"testing"
)

func TestAgentLanguage(t *testing.T) {
	t.Run("returns trimmed string when Config language set", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"language": "  Spanish  "}}
		if got := agentLanguage(def); got != "Spanish" {
			t.Fatalf("agentLanguage() = %q, want %q", got, "Spanish")
		}
	})

	t.Run("returns empty string when def is nil", func(t *testing.T) {
		if got := agentLanguage(nil); got != "" {
			t.Fatalf("agentLanguage(nil) = %q, want empty", got)
		}
	})

	t.Run("returns empty string when Config is nil", func(t *testing.T) {
		def := &AgentDefinition{}
		if got := agentLanguage(def); got != "" {
			t.Fatalf("agentLanguage() = %q, want empty", got)
		}
	})

	t.Run("returns empty string when key missing", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"other": "x"}}
		if got := agentLanguage(def); got != "" {
			t.Fatalf("agentLanguage() = %q, want empty", got)
		}
	})

	t.Run("returns empty string when value is non-string", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"language": 42}}
		if got := agentLanguage(def); got != "" {
			t.Fatalf("agentLanguage() = %q, want empty", got)
		}
	})

	t.Run("returns empty string when value is blank", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"language": "   "}}
		if got := agentLanguage(def); got != "" {
			t.Fatalf("agentLanguage() = %q, want empty", got)
		}
	})
}

func TestResolveInstructionLanguage(t *testing.T) {
	base := "You are a helpful assistant."

	t.Run("appends directive when language set", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
				Config:       map[string]any{"language": "Spanish"},
			},
		}
		got := ae.resolveInstruction(req)
		if !strings.Contains(got, "\n\nAlways respond in Spanish.") {
			t.Fatalf("resolveInstruction() = %q, want it to contain %q", got, "\n\nAlways respond in Spanish.")
		}
		if !strings.HasPrefix(got, base) {
			t.Fatalf("resolveInstruction() = %q, want it to start with %q", got, base)
		}
	})

	t.Run("does not append when language unset", func(t *testing.T) {
		ae := &AgentExecutor{}
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{
				SystemPrompt: &base,
			},
		}
		got := ae.resolveInstruction(req)
		if strings.Contains(got, "Always respond in") {
			t.Fatalf("resolveInstruction() = %q, want no language directive", got)
		}
		if got != base {
			t.Fatalf("resolveInstruction() = %q, want %q", got, base)
		}
	})

	t.Run("does not append when no agent definition", func(t *testing.T) {
		ae := &AgentExecutor{}
		got := ae.resolveInstruction(ExecuteRequest{})
		if strings.Contains(got, "Always respond in") {
			t.Fatalf("resolveInstruction() = %q, want no language directive", got)
		}
		if got != base {
			t.Fatalf("resolveInstruction() = %q, want %q", got, base)
		}
	})
}
