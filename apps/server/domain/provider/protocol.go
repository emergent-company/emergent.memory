package provider

// Protocol identifies the wire protocol a vendor speaks. Several vendors may
// share one protocol; the protocol selects the client adapter used to execute
// a request.
type Protocol string

const (
	// ProtocolOpenAIChat is the OpenAI Chat Completions wire protocol.
	ProtocolOpenAIChat Protocol = "openai-chat"
	// ProtocolGoogleGenAI is Google's native generateContent protocol.
	ProtocolGoogleGenAI Protocol = "google-genai"
	// ProtocolAnthropicMessages is Anthropic's native Messages protocol.
	ProtocolAnthropicMessages Protocol = "anthropic-messages"
)

// AuthStyle names how a vendor expects credentials to be injected.
type AuthStyle string

const (
	// AuthBearer sends Authorization: Bearer <key>.
	AuthBearer AuthStyle = "bearer"
	// AuthAPIKeyHeader sends api-key: <key> (Azure OpenAI).
	AuthAPIKeyHeader AuthStyle = "api-key"
	// AuthXAPIKey sends x-api-key: <key> (Anthropic).
	AuthXAPIKey AuthStyle = "x-api-key"
	// AuthGoogleAPIKey sends x-goog-api-key: <key> (Gemini native).
	AuthGoogleAPIKey AuthStyle = "x-goog-api-key"
	// AuthNone sends no credential header (local deployments).
	AuthNone AuthStyle = "none"
	// AuthSigned delegates to a registered signer hook. No signer is currently
	// implemented, so an openai-chat vendor must not declare it; service-account
	// vendors use the google-genai path instead.
	AuthSigned AuthStyle = "signed"
)

// CatalogStrategy names how a vendor's model catalog is resolved.
type CatalogStrategy string

const (
	// CatalogOpenAIModels lists models via GET {base_url}/models.
	CatalogOpenAIModels CatalogStrategy = "openai-models"
	// CatalogGoogleGenAI lists models via the Google genai SDK.
	CatalogGoogleGenAI CatalogStrategy = "google-genai"
	// CatalogStatic returns the definition's embedded model list.
	CatalogStatic CatalogStrategy = "static"
)

// ExtraField describes one vendor-specific configuration input rendered
// dynamically by the API and UI (for example an Azure api-version).
type ExtraField struct {
	Key         string             `json:"key"`
	Label       string             `json:"label"`
	Type        string             `json:"type"` // string | password | number | boolean | select
	Required    bool               `json:"required"`
	Secret      bool               `json:"secret"`
	Default     string             `json:"default,omitempty"`
	Placeholder string             `json:"placeholder,omitempty"`
	Options     []ExtraFieldOption `json:"options,omitempty"`
}

// ExtraFieldOption is one choice of a select extra field.
type ExtraFieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CredentialLabel optionally renames the primary credential input for a vendor.
type CredentialLabel struct {
	Label       string `json:"label,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Required    bool   `json:"required,omitempty"`
}
