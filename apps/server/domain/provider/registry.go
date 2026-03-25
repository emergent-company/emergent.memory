package provider

import "fmt"

// CredentialField describes a required credential field for a provider.
type CredentialField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"` // if true, the field value is encrypted
}

// ProviderDefinition describes a supported LLM provider and its authentication requirements.
type ProviderDefinition struct {
	Type             ProviderType      `json:"type"`
	DisplayName      string            `json:"displayName"`
	Description      string            `json:"description"`
	CredentialFields []CredentialField `json:"credentialFields"`
}

// Registry holds the set of supported LLM providers.
type Registry struct {
	providers map[ProviderType]*ProviderDefinition
}

// NewRegistry creates and returns a Registry pre-populated with the
// supported providers: Google AI and Vertex AI.
func NewRegistry() *Registry {
	r := &Registry{
		providers: make(map[ProviderType]*ProviderDefinition, 2),
	}

	r.providers[ProviderGoogleAI] = &ProviderDefinition{
		Type:        ProviderGoogleAI,
		DisplayName: "Google AI",
		Description: "Google AI (Gemini API) authenticated via API key",
		CredentialFields: []CredentialField{
			{Name: "api_key", Description: "Google AI API key", Required: true, Secret: true},
		},
	}

	r.providers[ProviderVertexAI] = &ProviderDefinition{
		Type:        ProviderVertexAI,
		DisplayName: "Vertex AI",
		Description: "Google Cloud Vertex AI authenticated via service account",
		CredentialFields: []CredentialField{
			{Name: "service_account_json", Description: "GCP service account JSON key file contents", Required: true, Secret: true},
			{Name: "gcp_project", Description: "GCP project ID", Required: true, Secret: false},
			{Name: "location", Description: "GCP region (e.g. us-central1)", Required: true, Secret: false},
		},
	}

	r.providers[ProviderOpenAICompatible] = &ProviderDefinition{
		Type:        ProviderOpenAICompatible,
		DisplayName: "OpenAI-Compatible",
		Description: "Any OpenAI-compatible LLM endpoint (Ollama, llama.cpp, vLLM, etc.)",
		CredentialFields: []CredentialField{
			{Name: "base_url", Description: "Base URL of the OpenAI-compatible API (e.g. http://localhost:11434/v1)", Required: true, Secret: false},
			{Name: "api_key", Description: "API key (use any value for keyless local servers)", Required: false, Secret: true},
		},
	}

	return r
}

// Get returns the definition for the given provider type.
// Returns an error if the provider is not registered.
func (r *Registry) Get(pt ProviderType) (*ProviderDefinition, error) {
	def, ok := r.providers[pt]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", pt)
	}
	return def, nil
}

// List returns all registered provider definitions.
func (r *Registry) List() []*ProviderDefinition {
	defs := make([]*ProviderDefinition, 0, len(r.providers))
	for _, d := range r.providers {
		defs = append(defs, d)
	}
	return defs
}

// IsSupported returns true if the given provider type is registered.
func (r *Registry) IsSupported(pt ProviderType) bool {
	_, ok := r.providers[pt]
	return ok
}

// SupportedTypes returns a slice of all registered provider types.
func (r *Registry) SupportedTypes() []ProviderType {
	types := make([]ProviderType, 0, len(r.providers))
	for t := range r.providers {
		types = append(types, t)
	}
	return types
}
