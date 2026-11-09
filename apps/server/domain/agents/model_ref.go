package agents

import (
	"errors"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/modelref"
)

// ErrInvalidAgentModelRef is the canonical error surfaced by every agent
// model-reference write path (REST create/update, MCP create/update, project
// agent overrides) when model.name is malformed.
var ErrInvalidAgentModelRef = errors.New(`model.name must be in 'provider/model' form (e.g. "deepseek/deepseek-v4-flash", "google/gemini-2.5-flash")`)

// NormalizeAgentModelConfig validates and canonicalizes the model reference on
// an agent definition or override. It is the single funnel for every write path
// that sets Model.Name / Model.Provider, so REST, MCP, and project overrides all
// accept and reject the same references.
//
// Accepted forms:
//   - canonical prefix: {"name":"openai/gpt-4o"}                → name "openai/gpt-4o"
//   - structured slug:  {"name":"gpt-4o","provider":"openai"}   → name "openai/gpt-4o"
//   - no override:      empty name and provider                 → left empty
//
// One canonical representation is stored: Model.Name holds "provider/model" and
// Model.Provider is cleared. A structured provider is folded into the name so
// the executor no longer needs to merge the two fields. A nil config or an
// empty reference is valid ("use the project default").
func NormalizeAgentModelConfig(m *ModelConfig) error {
	if m == nil {
		return nil
	}
	ref, err := modelref.ParseWithProvider(m.Name, m.Provider)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAgentModelRef, err)
	}
	if ref.IsZero() {
		// No override. Drop a stray provider so the persisted shape is canonical.
		m.Provider = ""
		return nil
	}
	m.Name = ref.String()
	m.Provider = ""
	return nil
}

// healAgentModelRef normalizes a stored model reference in place. A reference
// that cannot be parsed (for example a legacy bare name with no provider
// prefix) is cleared so the agent falls back to the project default instead of
// failing at model construction. Valid references — including the legacy
// structured {name,provider} shape — are canonicalized.
func healAgentModelRef(m *ModelConfig) {
	if m == nil {
		return
	}
	if err := NormalizeAgentModelConfig(m); err != nil {
		m.Name = ""
		m.Provider = ""
	}
}

// FoldAgentModelRef returns a normalized copy of m with any legacy structured
// provider folded into the canonical "provider/model" name and Provider cleared.
// It never mutates m and never errors: a reference that cannot be parsed is
// returned unchanged (as a copy), and nil returns nil.
//
// It is read-side defense in depth for #1465. Every write path funnels through
// NormalizeAgentModelConfig, so newly written rows store the canonical shape.
// But kb.agent_definitions.model was never backfilled, so rows written before
// the invariant shipped can still hold the legacy structured shape
// {"name":"<bare>","provider":"<slug>"} (e.g. the dev `operator` row storing
// {"name":"deepseek-v4-flash","provider":"openai"}). Folding on read makes those
// rows surface the same "provider/model" string as the normalized rows (which
// report the project default reconstructed as provider/model), instead of one
// bare name and one prefixed name in the same list.
func FoldAgentModelRef(m *ModelConfig) *ModelConfig {
	if m == nil {
		return nil
	}
	// Shallow copy: Name/Provider are strings we reassign; the remaining pointer
	// and slice fields are shared but never mutated here, so this is safe.
	out := *m
	ref, err := modelref.ParseWithProvider(m.Name, m.Provider)
	if err != nil || ref.IsZero() {
		// Unparsable (or empty) reference: leave the stored value untouched.
		return &out
	}
	out.Name = ref.String()
	out.Provider = ""
	return &out
}
