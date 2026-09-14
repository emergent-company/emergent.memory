package main

import (
	"context"
	"net/http"
	"net/url"
)

// --- provider configuration (project providers, model catalog, pricing) ---

// ProjectProviderConfig mirrors memory's ProjectProviderConfigResponse: the
// public-safe metadata of one project-level provider config (credentials are
// never included). All keys are camelCase.
type ProjectProviderConfig struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	Provider        string `json:"provider"`
	GCPProject      string `json:"gcpProject,omitempty"`
	Location        string `json:"location,omitempty"`
	BaseURL         string `json:"baseUrl,omitempty"`
	GenerativeModel string `json:"generativeModel,omitempty"`
	EmbeddingModel  string `json:"embeddingModel,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// ProviderConfigInput is the request body for upserting a project provider
// config (PUT /api/v1/projects/{projectId}/providers/{provider}). Credentials
// differ by provider type: google takes an API key; google-vertex takes a
// service-account JSON + GCP project + location; openai/deepseek take an API
// key + base URL.
type ProviderConfigInput struct {
	APIKey             string `json:"apiKey,omitempty"`
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
	GCPProject         string `json:"gcpProject,omitempty"`
	Location           string `json:"location,omitempty"`
	BaseURL            string `json:"baseUrl,omitempty"`
	GenerativeModel    string `json:"generativeModel,omitempty"`
	EmbeddingModel     string `json:"embeddingModel,omitempty"`
}

// ProviderTestResult is the response of a live credential test (POST
// /api/v1/projects/{projectId}/providers/{provider}/test).
type ProviderTestResult struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Reply          string `json:"reply"`
	LatencyMs      int64  `json:"latencyMs"`
	EmbeddingModel string `json:"embeddingModel,omitempty"`
	EmbeddingOK    bool   `json:"embeddingOk"`
	EmbeddingError string `json:"embeddingError,omitempty"`
}

// ProjectModelConfig is the project's default generative + embedding models
// (GET/PUT /api/v1/projects/{projectId}/model-config; memory's
// kb.project_model_config). Model names carry a provider prefix, e.g.
// "deepseek/deepseek-v4-pro".
type ProjectModelConfig struct {
	GenerativeModel string `json:"generativeModel"`
	EmbeddingModel  string `json:"embeddingModel"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// ProviderSupportedModel mirrors memory's ProviderSupportedModel: one cached
// catalog entry of a model a provider supports.
type ProviderSupportedModel struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	ModelName       string `json:"modelName"`
	ModelType       string `json:"modelType"`
	DisplayName     string `json:"displayName,omitempty"`
	MaxOutputTokens *int   `json:"maxOutputTokens,omitempty"`
	MaxInputTokens  *int   `json:"maxInputTokens,omitempty"`
	LastSynced      string `json:"lastSynced,omitempty"`
}

// modelPriceRates is the USD-per-1M-token rate set shared by the retail
// pricing rows and the project pricing overrides (embedding it flattens the
// fields onto the JSON object of the embedding struct).
type modelPriceRates struct {
	TextInputPrice  float64 `json:"textInputPrice"`
	ImageInputPrice float64 `json:"imageInputPrice"`
	VideoInputPrice float64 `json:"videoInputPrice"`
	AudioInputPrice float64 `json:"audioInputPrice"`
	OutputPrice     float64 `json:"outputPrice"`
}

// ProviderPricing is one retail (automatic) pricing row from GET /api/v1/pricing.
type ProviderPricing struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	modelPriceRates
	LastSynced string `json:"lastSynced,omitempty"`
}

// ProjectCustomPricing is one project pricing override row (GET/PUT
// /api/v1/projects/{projectId}/pricing-overrides). A custom rate replaces the
// retail rate for (provider, model).
type ProjectCustomPricing struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	modelPriceRates
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ListProjectProviders lists the provider configs configured for the project
// (GET /api/v1/projects/{projectId}/providers). Bare JSON array — no
// {success,data} envelope.
func (m *MemoryClient) ListProjectProviders(ctx context.Context) ([]ProjectProviderConfig, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/providers"
	var out []ProjectProviderConfig
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProviderModels lists the cached model catalog for one provider (GET
// /api/v1/providers/{provider}/models). Bare JSON array.
func (m *MemoryClient) ListProviderModels(ctx context.Context, provider string) ([]ProviderSupportedModel, error) {
	path := "/api/v1/providers/" + url.PathEscape(provider) + "/models"
	var out []ProviderSupportedModel
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListPricing lists the retail (automatic) pricing rows across providers (GET
// /api/v1/pricing). Bare JSON array.
func (m *MemoryClient) ListPricing(ctx context.Context) ([]ProviderPricing, error) {
	var out []ProviderPricing
	if err := m.do(ctx, http.MethodGet, "/api/v1/pricing", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjectPricingOverrides lists the project's pricing overrides (GET
// /api/v1/projects/{projectId}/pricing-overrides). Bare JSON array; memory
// may return a JSON null when there are none, which decodes to a nil slice.
func (m *MemoryClient) ListProjectPricingOverrides(ctx context.Context) ([]ProjectCustomPricing, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/pricing-overrides"
	var out []ProjectCustomPricing
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertProjectPricingOverride sets (or replaces) the custom rate for one
// (provider, model) (PUT /api/v1/projects/{projectId}/pricing-overrides). The
// body carries the five rate fields in camelCase; the response is the bare
// saved entry.
func (m *MemoryClient) UpsertProjectPricingOverride(ctx context.Context, provider, model string, rates modelPriceRates) (*ProjectCustomPricing, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/pricing-overrides"
	body := struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		modelPriceRates
	}{Provider: provider, Model: model, modelPriceRates: rates}
	var out ProjectCustomPricing
	if err := m.do(ctx, http.MethodPut, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectPricingOverride removes the custom rate for one (provider,
// model), reverting it to the retail rate (DELETE
// /api/v1/projects/{projectId}/pricing-overrides/{provider}/{model}).
func (m *MemoryClient) DeleteProjectPricingOverride(ctx context.Context, provider, model string) error {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/pricing-overrides/" + url.PathEscape(provider) + "/" + url.PathEscape(model)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// UpsertProjectProviderConfig sets a project's provider credentials + model
// selections (PUT /api/v1/projects/{projectId}/providers/{provider}).
func (m *MemoryClient) UpsertProjectProviderConfig(ctx context.Context, provider string, in ProviderConfigInput) (*ProjectProviderConfig, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/providers/" + url.PathEscape(provider)
	var out ProjectProviderConfig
	if err := m.do(ctx, http.MethodPut, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectProviderConfig removes a project's provider config (DELETE
// /api/v1/projects/{projectId}/providers/{provider}).
func (m *MemoryClient) DeleteProjectProviderConfig(ctx context.Context, provider string) error {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/providers/" + url.PathEscape(provider)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

// TestProjectProvider sends a live generate+embed call using the project's
// configured credentials (POST /api/v1/projects/{projectId}/providers/{provider}/test).
func (m *MemoryClient) TestProjectProvider(ctx context.Context, provider string) (*ProviderTestResult, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/providers/" + url.PathEscape(provider) + "/test"
	var out ProviderTestResult
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProjectModelConfig returns the project's stored default models (GET
// /api/v1/projects/{projectId}/model-config). Returns an empty config (no
// error) when none is set.
func (m *MemoryClient) GetProjectModelConfig(ctx context.Context) (*ProjectModelConfig, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/model-config"
	var out ProjectModelConfig
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsertProjectModelConfig sets the project's default generative + embedding
// models (PUT /api/v1/projects/{projectId}/model-config). Model names must
// include a provider prefix (e.g. "deepseek/deepseek-v4-pro").
func (m *MemoryClient) UpsertProjectModelConfig(ctx context.Context, generativeModel, embeddingModel string) (*ProjectModelConfig, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/model-config"
	body := map[string]string{"generativeModel": generativeModel, "embeddingModel": embeddingModel}
	var out ProjectModelConfig
	if err := m.do(ctx, http.MethodPut, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
