package provider

import "fmt"

// CredentialField describes a required credential field for a provider.
type CredentialField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"` // if true, the field value is encrypted
}

// Compat carries a vendor's protocol-level request/response quirks.
type Compat struct {
	// MaxTokensField is the output-cap field name: "max_tokens" or
	// "max_completion_tokens". Empty means the adapter default.
	MaxTokensField string
	// SupportsToolChoice reports whether the vendor accepts an explicit
	// tool_choice constraint.
	SupportsToolChoice bool
	// ThinkingStyle names the vendor's thinking/CoT toggle mechanism:
	// "", "chat_template_kwargs", "thinking", "thinking_type".
	ThinkingStyle string
}

// ProviderDefinition describes a supported LLM/embedding vendor and its
// wire protocol, authentication, endpoints, and vendor-specific inputs.
type ProviderDefinition struct {
	Type        ProviderType `json:"type"`
	DisplayName string       `json:"displayName"`
	Description string       `json:"description"`

	// Protocol is the default wire protocol.
	Protocol Protocol `json:"protocol"`
	// Auth is the default credential-injection style.
	Auth AuthStyle `json:"auth"`
	// AuthByProtocol overrides Auth for a specific protocol.
	AuthByProtocol map[Protocol]AuthStyle `json:"-"`

	// DefaultBaseURLs maps model type to the vendor's default endpoint.
	DefaultBaseURLs map[ModelType]string `json:"defaultBaseUrls,omitempty"`
	// ModelTypes lists the model types this vendor serves.
	ModelTypes []ModelType `json:"modelTypes,omitempty"`
	// URLPatterns identify the vendor from a base URL when unset.
	URLPatterns []string `json:"urlPatterns,omitempty"`
	// ExtraFields are vendor-specific configuration inputs rendered dynamically.
	ExtraFields []ExtraField `json:"extraFields,omitempty"`
	// CredentialLabel optionally renames the primary credential input.
	CredentialLabel *CredentialLabel `json:"credentialLabel,omitempty"`

	// CatalogStrategy selects how the model catalog is resolved.
	CatalogStrategy CatalogStrategy `json:"catalogStrategy"`
	// Compat holds protocol-level quirks.
	Compat Compat `json:"compat,omitempty"`

	// Order sorts the vendor list in the UI (lower first).
	Order int `json:"order"`

	// Icon is the vendor brand mark as SVG bytes (not serialized).
	Icon []byte `json:"-"`

	// Names/Descriptions carry localized variants keyed by locale.
	Names        map[string]string `json:"-"`
	Descriptions map[string]string `json:"-"`

	// CredentialFields is the legacy UI-facing credential list.
	CredentialFields []CredentialField `json:"credentialFields"`
}

// Registry holds the set of supported LLM vendors.
type Registry struct {
	providers map[ProviderType]*ProviderDefinition
	order     []ProviderType
}

// NewRegistry creates a Registry from the embedded built-in vendor definitions.
func NewRegistry() *Registry {
	defs := Builtins()
	r := &Registry{
		providers: make(map[ProviderType]*ProviderDefinition, len(defs)),
		order:     make([]ProviderType, 0, len(defs)),
	}
	for _, d := range defs {
		if _, exists := r.providers[d.Type]; exists {
			continue
		}
		r.providers[d.Type] = d
		r.order = append(r.order, d.Type)
	}
	return r
}

// Get returns the definition for the given vendor type.
func (r *Registry) Get(pt ProviderType) (*ProviderDefinition, error) {
	def, ok := r.providers[pt]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", pt)
	}
	return def, nil
}

// List returns all registered vendor definitions in display order.
func (r *Registry) List() []*ProviderDefinition {
	defs := make([]*ProviderDefinition, 0, len(r.order))
	for _, t := range r.order {
		defs = append(defs, r.providers[t])
	}
	return defs
}

// IsSupported returns true if the given vendor type is registered.
func (r *Registry) IsSupported(pt ProviderType) bool {
	_, ok := r.providers[pt]
	return ok
}

// SupportedTypes returns all registered vendor types in display order.
func (r *Registry) SupportedTypes() []ProviderType {
	out := make([]ProviderType, len(r.order))
	copy(out, r.order)
	return out
}

// ProtocolFor returns the effective protocol for a vendor, allowing a
// per-protocol auth override lookup.
func (r *Registry) ProtocolFor(pt ProviderType) Protocol {
	if d, ok := r.providers[pt]; ok {
		return d.Protocol
	}
	return ""
}
