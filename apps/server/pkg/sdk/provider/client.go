// Package provider provides the Provider service client for the Emergent API SDK.
// It covers LLM provider config management, model catalog, and usage reporting.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Provider API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new provider client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// --- Types ---

// ProviderType identifies a supported LLM provider.
type ProviderType = string

const (
	ProviderGoogleAI ProviderType = "google-ai"
	ProviderVertexAI ProviderType = "vertex-ai"
)

// ModelType classifies a model.
type ModelType = string

const (
	ModelTypeEmbedding  ModelType = "embedding"
	ModelTypeGenerative ModelType = "generative"
)

// ProviderConfig is the public-safe representation of a stored provider config.
// Credential fields (APIKey, ServiceAccountJSON) are never returned.
type ProviderConfig struct {
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	GCPProject      string    `json:"gcpProject,omitempty"`
	Location        string    `json:"location,omitempty"`
	GenerativeModel string    `json:"generativeModel,omitempty"`
	EmbeddingModel  string    `json:"embeddingModel,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// SupportedModel is a cached model entry from the provider catalog.
type SupportedModel struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	ModelName   string    `json:"modelName"`
	ModelType   string    `json:"modelType"`
	DisplayName string    `json:"displayName,omitempty"`
	LastSynced  time.Time `json:"lastSynced"`
}

// UsageSummaryRow is a single row of aggregated usage data.
type UsageSummaryRow struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	TotalText        int64   `json:"total_text"`
	TotalImage       int64   `json:"total_image"`
	TotalVideo       int64   `json:"total_video"`
	TotalAudio       int64   `json:"total_audio"`
	TotalOutput      int64   `json:"total_output"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// UsageSummary is the API response for a usage summary query.
type UsageSummary struct {
	Note string            `json:"note"`
	Data []UsageSummaryRow `json:"data"`
}

// TestProviderResponse is returned by the provider test endpoint.
type TestProviderResponse struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reply     string `json:"reply"`
	LatencyMs int64  `json:"latencyMs"`
}

// --- Request types ---

// UpsertProviderConfigRequest is the unified request body for creating or
// updating a provider config (org-level or project-level).
// For google-ai: set APIKey.
// For vertex-ai: set ServiceAccountJSON, GCPProject, Location.
// GenerativeModel and EmbeddingModel are auto-selected from the catalog if omitted.
type UpsertProviderConfigRequest struct {
	APIKey             string `json:"apiKey,omitempty"`
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
	GCPProject         string `json:"gcpProject,omitempty"`
	Location           string `json:"location,omitempty"`
	GenerativeModel    string `json:"generativeModel,omitempty"`
	EmbeddingModel     string `json:"embeddingModel,omitempty"`
}

// --- Organization Provider Config Methods ---

// UpsertOrgConfig stores credentials and model selections for an organization's provider.
// Runs a live credential test and syncs the model catalog on success.
func (c *Client) UpsertOrgConfig(ctx context.Context, orgID, provider string, req *UpsertProviderConfigRequest) (*ProviderConfig, error) {
	var result ProviderConfig
	err := c.doJSON(ctx, "PUT",
		fmt.Sprintf("/api/v1/organizations/%s/providers/%s",
			url.PathEscape(orgID), url.PathEscape(provider)),
		req, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetOrgConfig returns the stored config metadata (no secrets) for an org's provider.
func (c *Client) GetOrgConfig(ctx context.Context, orgID, provider string) (*ProviderConfig, error) {
	var result ProviderConfig
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/organizations/%s/providers/%s",
			url.PathEscape(orgID), url.PathEscape(provider)),
		nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteOrgConfig removes a provider config from an organization.
func (c *Client) DeleteOrgConfig(ctx context.Context, orgID, provider string) error {
	return c.doJSON(ctx, "DELETE",
		fmt.Sprintf("/api/v1/organizations/%s/providers/%s",
			url.PathEscape(orgID), url.PathEscape(provider)),
		nil, nil)
}

// ListOrgConfigs returns all provider config metadata for an organization.
func (c *Client) ListOrgConfigs(ctx context.Context, orgID string) ([]ProviderConfig, error) {
	var result []ProviderConfig
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/organizations/%s/providers", url.PathEscape(orgID)),
		nil, &result)
	return result, err
}

// --- Project Provider Config Methods ---

// UpsertProjectConfig stores credentials and model selections for a project's provider.
func (c *Client) UpsertProjectConfig(ctx context.Context, projectID, provider string, req *UpsertProviderConfigRequest) (*ProviderConfig, error) {
	var result ProviderConfig
	err := c.doJSON(ctx, "PUT",
		fmt.Sprintf("/api/v1/projects/%s/providers/%s",
			url.PathEscape(projectID), url.PathEscape(provider)),
		req, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProjectConfig returns the stored config metadata (no secrets) for a project's provider.
func (c *Client) GetProjectConfig(ctx context.Context, projectID, provider string) (*ProviderConfig, error) {
	var result ProviderConfig
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/projects/%s/providers/%s",
			url.PathEscape(projectID), url.PathEscape(provider)),
		nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProjectConfig removes a provider config from a project.
func (c *Client) DeleteProjectConfig(ctx context.Context, projectID, provider string) error {
	return c.doJSON(ctx, "DELETE",
		fmt.Sprintf("/api/v1/projects/%s/providers/%s",
			url.PathEscape(projectID), url.PathEscape(provider)),
		nil, nil)
}

// --- Model Catalog Methods ---

// ListModels returns the cached model catalog for a provider.
// modelType is optional ("embedding" or "generative"); pass "" for all models.
func (c *Client) ListModels(ctx context.Context, provider, modelType string) ([]SupportedModel, error) {
	path := fmt.Sprintf("/api/v1/providers/%s/models", url.PathEscape(provider))
	if modelType != "" {
		path += "?type=" + url.QueryEscape(modelType)
	}
	var result []SupportedModel
	err := c.doJSON(ctx, "GET", path, nil, &result)
	return result, err
}

// TestProvider sends a live generate call to verify provider credentials work.
// projectID and orgID are optional context hints for credential resolution.
func (c *Client) TestProvider(ctx context.Context, provider, projectID, orgID string) (*TestProviderResponse, error) {
	path := fmt.Sprintf("/api/v1/providers/%s/test", url.PathEscape(provider))
	params := url.Values{}
	if projectID != "" {
		params.Set("projectId", projectID)
	}
	if orgID != "" {
		params.Set("orgId", orgID)
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var result TestProviderResponse
	err := c.doJSON(ctx, "POST", path, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// --- Usage & Cost Methods ---

// GetProjectUsage returns aggregated usage and estimated cost for a project.
func (c *Client) GetProjectUsage(ctx context.Context, projectID string, since, until time.Time) (*UsageSummary, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/usage", url.PathEscape(projectID))
	path = appendTimeRange(path, since, until)

	var result UsageSummary
	err := c.doJSON(ctx, "GET", path, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetOrgUsage returns aggregated usage and estimated cost for all projects in an org.
func (c *Client) GetOrgUsage(ctx context.Context, orgID string, since, until time.Time) (*UsageSummary, error) {
	path := fmt.Sprintf("/api/v1/organizations/%s/usage", url.PathEscape(orgID))
	path = appendTimeRange(path, since, until)

	var result UsageSummary
	err := c.doJSON(ctx, "GET", path, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// --- Internal helpers ---

func (c *Client) doJSON(ctx context.Context, method, path string, bodyIn, bodyOut any) error {
	var bodyReader io.Reader
	if bodyIn != nil {
		b, err := json.Marshal(bodyIn)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if bodyIn != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if bodyOut != nil {
		if err := json.NewDecoder(resp.Body).Decode(bodyOut); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return nil
}

func appendTimeRange(path string, since, until time.Time) string {
	params := url.Values{}
	if !since.IsZero() {
		params.Set("since", since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		params.Set("until", until.UTC().Format(time.RFC3339))
	}
	if len(params) > 0 {
		return path + "?" + params.Encode()
	}
	return path
}
