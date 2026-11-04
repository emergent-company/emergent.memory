package blueprints

import (
	"fmt"
	"sort"
	"strings"

	sdkmodelref "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/modelref"
)

// ProjectProvider captures a project's configured LLM provider for the
// install-time model-mapping step: the provider slug plus its configured
// generative/embedding model selections. It is a plain, dependency-free view of
// the SDK's provider.ProjectProviderConfig so the mapping logic stays testable
// without a live server.
type ProjectProvider struct {
	Provider        string
	GenerativeModel string
	EmbeddingModel  string
}

// ModelMap resolves install-time model choices for a blueprint's agents against
// a project's configured providers and the per-project stored overrides.
//
// "Servable" is advisory: a pinned "provider/model" reference is servable when
// its provider prefix is one the project has configured. An agent whose pinned
// model is not servable still installs — the mapping step is informational and
// never a hard failure (the Preflight gate remains the only abort).
type ModelMap struct {
	providersBySlug map[string]ProjectProvider
	overrides       map[string]string
	// providersKnown is false when the provider list could not be fetched. In
	// that case availability is unknown and Resolve conservatively keeps a
	// pinned model rather than downgrading it to the project default.
	providersKnown bool
}

// NewModelMap builds a ModelMap from the project's configured providers and the
// stored overrides for this blueprint (may be nil/empty). The provider list is
// treated as authoritative (known).
func NewModelMap(providers []ProjectProvider, overrides map[string]string) *ModelMap {
	m := &ModelMap{
		providersBySlug: make(map[string]ProjectProvider, len(providers)),
		overrides:       overrides,
		providersKnown:  true,
	}
	for _, p := range providers {
		if p.Provider == "" {
			continue
		}
		m.providersBySlug[p.Provider] = p
	}
	if m.overrides == nil {
		m.overrides = map[string]string{}
	}
	return m
}

// MarkProvidersUnknown flags that the project's provider list could not be
// determined. Resolve then keeps pinned models instead of downgrading them.
func (m *ModelMap) MarkProvidersUnknown() {
	m.providersKnown = false
}

// ProvidersKnown reports whether the project's provider list was determined.
func (m *ModelMap) ProvidersKnown() bool {
	return m.providersKnown
}

// HasProviders reports whether the project has at least one configured provider.
func (m *ModelMap) HasProviders() bool {
	return len(m.providersBySlug) > 0
}

// ServableModels returns the project's configured generative models as ordered,
// deduplicated "provider/model" options (the select choices for an agent's
// install-time mapping). Embedding models are excluded — agent model references
// are generative. Providers without a configured generative model contribute
// nothing to this list (but still count as a servable prefix).
func (m *ModelMap) ServableModels() []string {
	seen := make(map[string]bool)
	var out []string
	slugs := make([]string, 0, len(m.providersBySlug))
	for slug := range m.providersBySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		p := m.providersBySlug[slug]
		model := strings.TrimSpace(p.GenerativeModel)
		if model == "" {
			continue
		}
		ref := p.Provider + "/" + model
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// IsServable reports whether a "provider/model" reference can be served by the
// project: its provider prefix is one of the project's configured providers.
// A malformed or empty reference is never servable.
func (m *ModelMap) IsServable(ref string) bool {
	r, err := sdkmodelref.Parse(ref)
	if err != nil {
		return false
	}
	_, ok := m.providersBySlug[r.Provider]
	return ok
}

// StoredOverride returns the stored override for an agent. An absent key means
// "no override"; a present key with an empty value is an explicit "project
// default" choice (the server persists the empty string as a deliberate
// clearing, not an absence).
func (m *ModelMap) StoredOverride(agent string) string {
	return m.overrides[agent]
}

// Resolve returns the effective model choice for an agent following the
// install-time precedence:
//
//	stored override > pinned model (if servable) > "" (project default)
//
// A stored override that is PRESENT wins even when its value is empty: an empty
// value is an explicit "project default" choice, not an absence. When the
// provider list could not be determined, a pinned model is kept unchanged
// (availability unknown) rather than downgraded to the default. An empty result
// means "project default": the caller should leave/clear the agent's model name.
func (m *ModelMap) Resolve(agent AgentFile) string {
	if v, ok := m.overrides[agent.Name]; ok {
		return v
	}
	pinned := PinnedModel(agent)
	if pinned == "" {
		return ""
	}
	if !m.providersKnown || m.IsServable(pinned) {
		return pinned
	}
	return ""
}

// ConfiguredModelsSummary returns a human-readable, comma-separated summary of
// the project's configured generative models for warning messages, or a
// "no providers configured" placeholder.
func (m *ModelMap) ConfiguredModelsSummary() string {
	models := m.ServableModels()
	if len(models) == 0 {
		return "no providers configured"
	}
	return strings.Join(models, ", ")
}

// PinnedModel returns the agent's pinned "provider/model" reference (already
// normalized by Preflight), or "" when the agent has no model block.
func PinnedModel(agent AgentFile) string {
	if agent.Model == nil {
		return ""
	}
	return agent.Model.Name
}

// ApplyModelChoice sets agent.Model.Name to choice, mirroring how
// NormalizeAgentModels canonicalizes the reference. An empty choice clears the
// name (and any stray provider) so the project default applies; a non-empty
// choice replaces the reference the applier sends. The agent's temperature /
// maxTokens / enableThinking tuning is preserved.
func ApplyModelChoice(agent *AgentFile, choice string) {
	if agent == nil || agent.Model == nil {
		return
	}
	agent.Model.Name = strings.TrimSpace(choice)
	agent.Model.Provider = ""
}

// ParseMapModel parses a --map-model flag value of the form
// "agent=provider/model" (empty model = project default). It returns the agent
// name and the resolved choice, or an error for a malformed value.
func ParseMapModel(value string) (agent, model string, err error) {
	const help = "expected agent=provider/model (empty model = project default)"
	agent, model, found := strings.Cut(value, "=")
	agent = strings.TrimSpace(agent)
	if agent == "" || !found {
		return "", "", fmt.Errorf("invalid --map-model %q: %s", value, help)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return agent, "", nil // project default
	}
	if _, err := sdkmodelref.Parse(model); err != nil {
		return "", "", fmt.Errorf("invalid --map-model model for agent %q: %v", agent, err)
	}
	return agent, model, nil
}
