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
