// Package modelconfig manages explicit default generative and embedding model
// configuration per-project and per-org. This is intentionally separate from
// provider credentials (domain/provider) — credential setup (API keys) and
// model selection (which model to use) are independent concerns.
//
// Resolution chain (generative and embedding):
//  1. Per-agent override (AgentDefinition.Model.Name) — handled by executor, not here
//  2. Project model config (kb.project_model_config)
//  3. Org model config    (kb.org_model_config)
//
// Model names must always include a provider prefix: "provider/model-name"
// (e.g. "deepseek/deepseek-v4-flash", "google/gemini-2.5-flash").
// If no config is found at any level, resolution returns ("", ModelSourceNone, nil)
// and callers must surface a "model not configured" error to the user.
package modelconfig

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// OrgModelConfig stores the default generative and embedding model for an org.
// Table: kb.org_model_config (migration 00120)
type OrgModelConfig struct {
	bun.BaseModel `bun:"table:kb.org_model_config,alias:omc"`

	OrgID           uuid.UUID `bun:"org_id,pk,type:uuid" json:"orgId"`
	GenerativeModel string    `bun:"generative_model,notnull" json:"generativeModel"`
	EmbeddingModel  string    `bun:"embedding_model,notnull" json:"embeddingModel"`
	CreatedAt       time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt       time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ProjectModelConfig stores the default generative and embedding model for a project.
// Takes precedence over OrgModelConfig.
// Table: kb.project_model_config (migration 00120)
type ProjectModelConfig struct {
	bun.BaseModel `bun:"table:kb.project_model_config,alias:pmc"`

	ProjectID       uuid.UUID `bun:"project_id,pk,type:uuid" json:"projectId"`
	GenerativeModel string    `bun:"generative_model,notnull" json:"generativeModel"`
	EmbeddingModel  string    `bun:"embedding_model,notnull" json:"embeddingModel"`
	CreatedAt       time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt       time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ModelSource describes where the effective model was resolved from.
type ModelSource string

const (
	ModelSourceProject ModelSource = "project"
	ModelSourceOrg     ModelSource = "org"
	// ModelSourceNone means no config was found at any level (project or org).
	// Callers must treat this as "not configured" and return an appropriate error.
	ModelSourceNone ModelSource = "none"
)

// UpsertModelConfigRequest is the request body for setting model config.
type UpsertModelConfigRequest struct {
	GenerativeModel string `json:"generativeModel"`
	EmbeddingModel  string `json:"embeddingModel"`
}

// ModelConfigResponse is the API response for a stored model config.
type ModelConfigResponse struct {
	GenerativeModel string    `json:"generativeModel"`
	EmbeddingModel  string    `json:"embeddingModel"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// EffectiveModelConfig is the response for the effective model resolution endpoint.
// It returns both the resolved model names and where each was resolved from.
type EffectiveModelConfig struct {
	GenerativeModel       string      `json:"generativeModel"`
	GenerativeModelSource ModelSource `json:"generativeModelSource"`
	EmbeddingModel        string      `json:"embeddingModel"`
	EmbeddingModelSource  ModelSource `json:"embeddingModelSource"`
}
