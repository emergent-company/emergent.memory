package provider

import "github.com/emergent-company/emergent.memory/pkg/adk"

// EnvVendorAdapter exposes the vendor Registry to pkg/adk as an
// adk.EnvVendorResolver, so the env-var fallback can build any registered
// vendor from env without pkg/adk importing domain/provider (which would be an
// import cycle).
type EnvVendorAdapter struct {
	registry *Registry
}

// NewEnvVendorAdapter creates a new EnvVendorAdapter.
func NewEnvVendorAdapter(registry *Registry) *EnvVendorAdapter {
	return &EnvVendorAdapter{registry: registry}
}

// EnvVendor satisfies adk.EnvVendorResolver. It returns the vendor's
// deterministic env prefix plus its generative default endpoint, default
// protocol, declared Responses hosts, and auth style. The Responses host list is
// carried through verbatim so the env path applies the same host rule as the DB
// path, against the *resolved* base URL (which may be overridden by env).
// Embedding-only vendors are not resolvable for generative env use.
func (a *EnvVendorAdapter) EnvVendor(provider string) (adk.EnvVendor, bool) {
	def, err := a.registry.Get(ProviderType(provider))
	if err != nil || !definitionServesModelType(def, ModelTypeGenerative) {
		return adk.EnvVendor{}, false
	}
	baseURL := def.DefaultBaseURLs[ModelTypeGenerative]
	return adk.EnvVendor{
		Prefix:         adk.EnvPrefix(provider),
		DefaultBaseURL: baseURL,
		Protocol:       string(def.Protocol),
		ResponsesHosts: def.ResponsesHosts,
		Auth:           string(def.Auth),
	}, true
}

// EnvVendorOrder satisfies adk.EnvVendorResolver, returning generative-capable
// vendor dialects in registry display order so the env fallback can pick a
// configured model deterministically. Embedding-only vendors are excluded so
// they can never be selected for (or enable) generative env configuration.
func (a *EnvVendorAdapter) EnvVendorOrder() []string {
	types := a.registry.SupportedTypes()
	out := make([]string, 0, len(types))
	for _, t := range types {
		def, err := a.registry.Get(t)
		if err != nil || !definitionServesModelType(def, ModelTypeGenerative) {
			continue
		}
		out = append(out, string(t))
	}
	return out
}
