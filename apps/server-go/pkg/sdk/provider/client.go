// Package provider provides the Provider service client for the Emergent API SDK.
// It covers LLM credential management, model catalog, project policies, and usage reporting.
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

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
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

// ProviderPolicy controls credential inheritance at the project level.
type ProviderPolicy = string

const (
	PolicyNone         ProviderPolicy = "none"
	PolicyOrganization ProviderPolicy = "organization"
	PolicyProject      ProviderPolicy = "project"
)

// ModelType classifies a model.
type ModelType = string

const (
	ModelTypeEmbedding  ModelType = "embedding"
	ModelTypeGenerative ModelType = "generative"
)

// OrgCredential is the public-safe representation of a stored org credential (no secrets).
type OrgCredential struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"orgId"`
	Provider   string    `json:"provider"`
	GCPProject string    `json:"gcpProject,omitempty"`
	Location   string    `json:"location,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
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

// ProjectPolicy is the per-provider policy for a project (no credential secrets).
type ProjectPolicy struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	Provider        string    `json:"provider"`
	Policy          string    `json:"policy"`
	GCPProject      string    `json:"gcpProject,omitempty"`
	Location        string    `json:"location,omitempty"`
	EmbeddingModel  string    `json:"embeddingModel,omitempty"`
	GenerativeModel string    `json:"generativeModel,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
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

// --- Request types ---

// SaveGoogleAICredentialRequest is the body for saving a Google AI API key.
type SaveGoogleAICredentialRequest struct {
	APIKey string `json:"apiKey"`
}

// SaveVertexAICredentialRequest is the body for saving Vertex AI credentials.
type SaveVertexAICredentialRequest struct {
	ServiceAccountJSON string `json:"serviceAccountJson"`
	GCPProject         string `json:"gcpProject"`
	Location           string `json:"location"`
}

// SetOrgModelSelectionRequest sets default models for an org + provider.
type SetOrgModelSelectionRequest struct {
	EmbeddingModel  string `json:"embeddingModel"`
	GenerativeModel string `json:"generativeModel"`
}

// SetProjectPolicyRequest sets a project's provider policy.
type SetProjectPolicyRequest struct {
	Policy             string `json:"policy"`
	APIKey             string `json:"apiKey,omitempty"`
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
	GCPProject         string `json:"gcpProject,omitempty"`
	Location           string `json:"location,omitempty"`
	EmbeddingModel     string `json:"embeddingModel,omitempty"`
	GenerativeModel    string `json:"generativeModel,omitempty"`
}

// --- Organization Credential Methods ---

// SaveGoogleAICredential stores a Google AI API key for an organization.
func (c *Client) SaveGoogleAICredential(ctx context.Context, orgID string, req *SaveGoogleAICredentialRequest) error {
	return c.doJSON(ctx, "POST",
		fmt.Sprintf("/api/v1/organizations/%s/providers/google-ai/credentials", url.PathEscape(orgID)),
		req, nil)
}

// SaveVertexAICredential stores Vertex AI credentials for an organization.
func (c *Client) SaveVertexAICredential(ctx context.Context, orgID string, req *SaveVertexAICredentialRequest) error {
	return c.doJSON(ctx, "POST",
		fmt.Sprintf("/api/v1/organizations/%s/providers/vertex-ai/credentials", url.PathEscape(orgID)),
		req, nil)
}

// DeleteOrgCredential removes a provider credential from an organization.
func (c *Client) DeleteOrgCredential(ctx context.Context, orgID, provider string) error {
	return c.doJSON(ctx, "DELETE",
		fmt.Sprintf("/api/v1/organizations/%s/providers/%s/credentials",
			url.PathEscape(orgID), url.PathEscape(provider)),
		nil, nil)
}

// ListOrgCredentials returns stored credential metadata for an organization.
func (c *Client) ListOrgCredentials(ctx context.Context, orgID string) ([]OrgCredential, error) {
	var result []OrgCredential
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/organizations/%s/providers/credentials", url.PathEscape(orgID)),
		nil, &result)
	return result, err
}

// SetOrgModelSelection sets the default models for an org + provider.
func (c *Client) SetOrgModelSelection(ctx context.Context, orgID, provider string, req *SetOrgModelSelectionRequest) error {
	return c.doJSON(ctx, "PUT",
		fmt.Sprintf("/api/v1/organizations/%s/providers/%s/models",
			url.PathEscape(orgID), url.PathEscape(provider)),
		req, nil)
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

// --- Project Policy Methods ---

// SetProjectPolicy sets the provider policy for a project.
func (c *Client) SetProjectPolicy(ctx context.Context, projectID, provider string, req *SetProjectPolicyRequest) error {
	return c.doJSON(ctx, "PUT",
		fmt.Sprintf("/api/v1/projects/%s/providers/%s/policy",
			url.PathEscape(projectID), url.PathEscape(provider)),
		req, nil)
}

// GetProjectPolicy returns the current provider policy for a project.
func (c *Client) GetProjectPolicy(ctx context.Context, projectID, provider string) (*ProjectPolicy, error) {
	var result ProjectPolicy
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/projects/%s/providers/%s/policy",
			url.PathEscape(projectID), url.PathEscape(provider)),
		nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListProjectPolicies returns all provider policies for a project.
func (c *Client) ListProjectPolicies(ctx context.Context, projectID string) ([]ProjectPolicy, error) {
	var result []ProjectPolicy
	err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/api/v1/projects/%s/providers/policies", url.PathEscape(projectID)),
		nil, &result)
	return result, err
}

// --- Usage & Cost Methods ---

// GetProjectUsage returns aggregated usage and estimated cost for a project.
// since and until are optional; pass zero time.Time to omit.
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
// since and until are optional; pass zero time.Time to omit.
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

// doJSON performs an authenticated JSON HTTP request. bodyIn is marshalled to the
// request body (if non-nil); the response body is unmarshalled into bodyOut (if non-nil).
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

// appendTimeRange adds since/until query params to a path if they are non-zero.
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
