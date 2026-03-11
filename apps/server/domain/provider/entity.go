package provider

import (
	"time"

	"github.com/uptrace/bun"
)

// ProviderType identifies a supported LLM provider.
type ProviderType string

const (
	ProviderGoogleAI ProviderType = "google"
	ProviderVertexAI ProviderType = "google-vertex"
)

// ModelType classifies a model as embedding or generative.
type ModelType string

const (
	ModelTypeEmbedding  ModelType = "embedding"
	ModelTypeGenerative ModelType = "generative"
)

// OperationType classifies an LLM operation for usage tracking.
type OperationType string

const (
	OperationGenerate OperationType = "generate"
	OperationEmbed    OperationType = "embed"
)

// --- Bun entities mapping to migration tables ---

// OrgProviderConfig stores encrypted credentials and model selections for a
// provider at the organization level.
// Table: kb.org_provider_configs (migration 00042)
type OrgProviderConfig struct {
	bun.BaseModel `bun:"table:kb.org_provider_configs,alias:opc"`

	ID                  string       `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrgID               string       `bun:"org_id,notnull,type:uuid" json:"orgId"`
	Provider            ProviderType `bun:"provider,notnull" json:"provider"`
	EncryptedCredential []byte       `bun:"encrypted_credential,notnull" json:"-"`
	EncryptionNonce     []byte       `bun:"encryption_nonce,notnull" json:"-"`
	GCPProject          string       `bun:"gcp_project" json:"gcpProject,omitempty"`
	Location            string       `bun:"location" json:"location,omitempty"`
	GenerativeModel     string       `bun:"generative_model" json:"generativeModel,omitempty"`
	EmbeddingModel      string       `bun:"embedding_model" json:"embeddingModel,omitempty"`
	CreatedAt           time.Time    `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt           time.Time    `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ProjectProviderConfig stores encrypted credentials and model selections for a
// provider at the project level.
// Table: kb.project_provider_configs (migration 00042)
type ProjectProviderConfig struct {
	bun.BaseModel `bun:"table:kb.project_provider_configs,alias:ppc"`

	ID                  string       `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID           string       `bun:"project_id,notnull,type:uuid" json:"projectId"`
	Provider            ProviderType `bun:"provider,notnull" json:"provider"`
	EncryptedCredential []byte       `bun:"encrypted_credential,notnull" json:"-"`
	EncryptionNonce     []byte       `bun:"encryption_nonce,notnull" json:"-"`
	GCPProject          string       `bun:"gcp_project" json:"gcpProject,omitempty"`
	Location            string       `bun:"location" json:"location,omitempty"`
	GenerativeModel     string       `bun:"generative_model" json:"generativeModel,omitempty"`
	EmbeddingModel      string       `bun:"embedding_model" json:"embeddingModel,omitempty"`
	CreatedAt           time.Time    `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt           time.Time    `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// UpsertProviderConfigRequest is the request body for creating or updating a
// provider config (org-level or project-level).
// For google: set APIKey.
// For google-vertex: set ServiceAccountJSON, GCPProject, Location.
type UpsertProviderConfigRequest struct {
	APIKey             string `json:"apiKey,omitempty"`
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
	GCPProject         string `json:"gcpProject,omitempty"`
	Location           string `json:"location,omitempty"`
	GenerativeModel    string `json:"generativeModel,omitempty"`
	EmbeddingModel     string `json:"embeddingModel,omitempty"`
}

// ProviderConfigResponse is the public-safe representation of a stored provider config.
// Credential fields (APIKey, ServiceAccountJSON) are never returned.
type ProviderConfigResponse struct {
	ID              string       `json:"id"`
	Provider        ProviderType `json:"provider"`
	GCPProject      string       `json:"gcpProject,omitempty"`
	Location        string       `json:"location,omitempty"`
	GenerativeModel string       `json:"generativeModel,omitempty"`
	EmbeddingModel  string       `json:"embeddingModel,omitempty"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

// ProviderSupportedModel is a cached entry of a model available from a provider.
// Table: kb.provider_supported_models (migration 00038)
type ProviderSupportedModel struct {
	bun.BaseModel `bun:"table:kb.provider_supported_models,alias:psm"`

	ID              string       `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Provider        ProviderType `bun:"provider,notnull" json:"provider"`
	ModelName       string       `bun:"model_name,notnull" json:"modelName"`
	ModelType       ModelType    `bun:"model_type,notnull" json:"modelType"`
	DisplayName     string       `bun:"display_name" json:"displayName,omitempty"`
	MaxOutputTokens *int         `bun:"max_output_tokens" json:"maxOutputTokens,omitempty"`
	LastSynced      time.Time    `bun:"last_synced,notnull,default:now()" json:"lastSynced"`
}

// LLMUsageEvent records a single LLM operation's token usage and estimated cost.
// Table: kb.llm_usage_events (migration 00039)
type LLMUsageEvent struct {
	bun.BaseModel `bun:"table:kb.llm_usage_events,alias:lue"`

	ID               string        `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID        string        `bun:"project_id,notnull,type:uuid" json:"projectId"`
	OrgID            string        `bun:"org_id,notnull,type:uuid" json:"orgId"`
	Provider         ProviderType  `bun:"provider,notnull" json:"provider"`
	Model            string        `bun:"model,notnull" json:"model"`
	Operation        OperationType `bun:"operation,notnull,default:'generate'" json:"operation"`
	TextInputTokens  int64         `bun:"text_input_tokens,notnull,default:0" json:"textInputTokens"`
	ImageInputTokens int64         `bun:"image_input_tokens,notnull,default:0" json:"imageInputTokens"`
	VideoInputTokens int64         `bun:"video_input_tokens,notnull,default:0" json:"videoInputTokens"`
	AudioInputTokens int64         `bun:"audio_input_tokens,notnull,default:0" json:"audioInputTokens"`
	OutputTokens     int64         `bun:"output_tokens,notnull,default:0" json:"outputTokens"`
	EstimatedCostUSD float64       `bun:"estimated_cost_usd,notnull,default:0" json:"estimatedCostUsd"`
	RunID            *string       `bun:"run_id,type:uuid,nullzero" json:"runId,omitempty"`
	RootRunID        *string       `bun:"root_run_id,type:uuid,nullzero" json:"rootRunId,omitempty"`
	CreatedAt        time.Time     `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// ProviderPricing stores global retail pricing per model (synced daily).
// Prices are per 1 million tokens.
// Table: kb.provider_pricing (migration 00040)
type ProviderPricing struct {
	bun.BaseModel `bun:"table:kb.provider_pricing,alias:pp"`

	ID              string       `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Provider        ProviderType `bun:"provider,notnull" json:"provider"`
	Model           string       `bun:"model,notnull" json:"model"`
	TextInputPrice  float64      `bun:"text_input_price,notnull,default:0" json:"textInputPrice"`
	ImageInputPrice float64      `bun:"image_input_price,notnull,default:0" json:"imageInputPrice"`
	VideoInputPrice float64      `bun:"video_input_price,notnull,default:0" json:"videoInputPrice"`
	AudioInputPrice float64      `bun:"audio_input_price,notnull,default:0" json:"audioInputPrice"`
	OutputPrice     float64      `bun:"output_price,notnull,default:0" json:"outputPrice"`
	LastSynced      time.Time    `bun:"last_synced,notnull,default:now()" json:"lastSynced"`
}

// OrganizationCustomPricing stores org-specific pricing overrides (enterprise rates).
// Prices are per 1 million tokens.
// Table: kb.organization_custom_pricing (migration 00040)
type OrganizationCustomPricing struct {
	bun.BaseModel `bun:"table:kb.organization_custom_pricing,alias:ocp"`

	ID              string       `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrgID           string       `bun:"org_id,notnull,type:uuid" json:"orgId"`
	Provider        ProviderType `bun:"provider,notnull" json:"provider"`
	Model           string       `bun:"model,notnull" json:"model"`
	TextInputPrice  float64      `bun:"text_input_price,notnull,default:0" json:"textInputPrice"`
	ImageInputPrice float64      `bun:"image_input_price,notnull,default:0" json:"imageInputPrice"`
	VideoInputPrice float64      `bun:"video_input_price,notnull,default:0" json:"videoInputPrice"`
	AudioInputPrice float64      `bun:"audio_input_price,notnull,default:0" json:"audioInputPrice"`
	OutputPrice     float64      `bun:"output_price,notnull,default:0" json:"outputPrice"`
	CreatedAt       time.Time    `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt       time.Time    `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}
