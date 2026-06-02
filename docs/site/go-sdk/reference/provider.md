# provider

Package `provider` provides a client for the Emergent Provider API.

The provider client is a **non-context client** — it requires no org or project context headers. It manages LLM credentials at the organization level, the model catalog for each provider, and per-project provider policies (which credential source to use and which models to call).

Supported providers: **Google AI** (`google`), **Vertex AI** (`google-vertex`), **OpenAI** (`openai`), and **DeepSeek** (`deepseek`).

## Import

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Provider`. No `SetContext` call is needed or available.

---

## Constants

### ProviderType

```go
const (
    ProviderGoogleAI ProviderType = "google"
    ProviderVertexAI ProviderType = "google-vertex"
    ProviderOpenAI   ProviderType = "openai"
    ProviderDeepSeek ProviderType = "deepseek"
)
```

---

### ProviderPolicy

```go
const (
    PolicyNone         ProviderPolicy = "none"
    PolicyOrganization ProviderPolicy = "organization"
    PolicyProject      ProviderPolicy = "project"
)
```

Controls where a project sources its LLM credentials:

| Value | Description |
|---|---|
| `"none"` | No credentials configured |
| `"organization"` | Inherit credentials from the parent org |
| `"project"` | Use project-specific credentials |

---

### ModelType

```go
const (
    ModelTypeEmbedding  ModelType = "embedding"
    ModelTypeGenerative ModelType = "generative"
)
```

---

## Methods

### Organization Config Methods

```go
// Store credentials + auto-select models for an org's provider.
// Runs a live credential test and syncs the model catalog on success.
func (c *Client) UpsertOrgConfig(ctx context.Context, orgID, provider string, req *UpsertProviderConfigRequest) (*ProviderConfig, error)

// Get stored config metadata (no secrets) for an org's provider.
func (c *Client) GetOrgConfig(ctx context.Context, orgID, provider string) (*ProviderConfig, error)

// Delete stored config for an org's provider.
func (c *Client) DeleteOrgConfig(ctx context.Context, orgID, provider string) error

// List all provider configs for an org.
func (c *Client) ListOrgConfigs(ctx context.Context, orgID string) ([]ProviderConfig, error)
```

**Endpoints:**
- `PUT /api/v1/organizations/{orgID}/providers/{provider}`
- `GET /api/v1/organizations/{orgID}/providers/{provider}`
- `DELETE /api/v1/organizations/{orgID}/providers/{provider}`
- `GET /api/v1/organizations/{orgID}/providers`

---

### Project Config Methods

```go
// Store a project-level provider override (beats org config in resolution chain).
func (c *Client) UpsertProjectConfig(ctx context.Context, projectID, provider string, req *UpsertProviderConfigRequest) (*ProviderConfig, error)

// Get project-level provider config.
func (c *Client) GetProjectConfig(ctx context.Context, projectID, provider string) (*ProviderConfig, error)

// Delete project-level override; project falls back to org config.
func (c *Client) DeleteProjectConfig(ctx context.Context, projectID, provider string) error

// List all project-level provider configs for a project.
func (c *Client) ListProjectConfigs(ctx context.Context, projectID string) ([]ProjectProviderConfig, error)

// List project-level overrides across all projects in an org.
func (c *Client) ListProjectConfigsByOrg(ctx context.Context, orgID string) ([]ProjectProviderConfig, error)
```

**Endpoints:**
- `PUT /api/v1/projects/{projectID}/providers/{provider}`
- `GET /api/v1/projects/{projectID}/providers/{provider}`
- `DELETE /api/v1/projects/{projectID}/providers/{provider}`
- `GET /api/v1/projects/{projectID}/providers`

---

### Model Catalog Methods

```go
// List models for a provider; modelType is optional ("embedding" or "generative").
func (c *Client) ListModels(ctx context.Context, provider, modelType string) ([]SupportedModel, error)
```

**Endpoint:** `GET /api/v1/providers/{provider}/models`

---

### Test Methods

```go
// Run a live generate call to verify credentials are working.
// Pass projectID to test via the project's resolved credential chain; pass orgID to test org credentials directly.
func (c *Client) TestProvider(ctx context.Context, provider, projectID, orgID string) (*TestProviderResponse, error)
```

**Endpoint:** `POST /api/v1/providers/{provider}/test`

---

### Usage Methods

```go
// Aggregated usage + estimated cost for a project (pass zero time.Time to omit since/until).
func (c *Client) GetProjectUsage(ctx context.Context, projectID string, since, until time.Time) (*UsageSummary, error)

// Aggregated usage across all projects in an org.
func (c *Client) GetOrgUsage(ctx context.Context, orgID string, since, until time.Time) (*UsageSummary, error)

// Usage broken down per project within an org.
func (c *Client) GetOrgUsageByProject(ctx context.Context, orgID string, since, until time.Time) (*OrgUsageByProject, error)
```

**Endpoints:**
- `GET /api/v1/projects/{projectID}/usage`
- `GET /api/v1/organizations/{orgID}/usage`
- `GET /api/v1/organizations/{orgID}/usage/by-project`

---

## Types

### ProviderConfig

```go
type ProviderConfig struct {
    ID              string    `json:"id"`
    OrgID           string    `json:"orgId"`
    Provider        string    `json:"provider"`
    GCPProject      string    `json:"gcpProject,omitempty"`
    Location        string    `json:"location,omitempty"`
    GenerativeModel string    `json:"generativeModel,omitempty"`
    EmbeddingModel  string    `json:"embeddingModel,omitempty"`
    CreatedAt       time.Time `json:"createdAt"`
    UpdatedAt       time.Time `json:"updatedAt"`
}
```

Credential secrets are never returned.

---

### ProjectProviderConfig

```go
type ProjectProviderConfig struct {
    ID              string    `json:"id"`
    ProjectID       string    `json:"projectId"`
    OrgID           string    `json:"orgId"`
    Provider        string    `json:"provider"`
    GCPProject      string    `json:"gcpProject,omitempty"`
    Location        string    `json:"location,omitempty"`
    GenerativeModel string    `json:"generativeModel,omitempty"`
    EmbeddingModel  string    `json:"embeddingModel,omitempty"`
    CreatedAt       time.Time `json:"createdAt"`
    UpdatedAt       time.Time `json:"updatedAt"`
}
```

---

### SupportedModel

```go
type SupportedModel struct {
    ID          string    `json:"id"`
    Provider    string    `json:"provider"`
    ModelName   string    `json:"modelName"`
    ModelType   string    `json:"modelType"` // "embedding" or "generative"
    DisplayName string    `json:"displayName,omitempty"`
    LastSynced  time.Time `json:"lastSynced"`
}
```

---

### TestProviderResponse

```go
type TestProviderResponse struct {
    Provider  string `json:"provider"`
    Success   bool   `json:"success"`
    Message   string `json:"message,omitempty"`
    Model     string `json:"model,omitempty"`
}
```

---

### UsageSummary

```go
type UsageSummary struct {
    Note string            `json:"note"`
    Data []UsageSummaryRow `json:"data"`
}

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
```

---

### UpsertProviderConfigRequest

```go
type UpsertProviderConfigRequest struct {
    APIKey             string `json:"apiKey,omitempty"`             // google, openai, deepseek
    ServiceAccountJSON string `json:"serviceAccountJson,omitempty"` // google-vertex
    GCPProject         string `json:"gcpProject,omitempty"`         // google-vertex
    Location           string `json:"location,omitempty"`           // google-vertex
    BaseURL            string `json:"baseUrl,omitempty"`            // openai (custom endpoint)
    GenerativeModel    string `json:"generativeModel,omitempty"`    // auto-selected if omitted
    EmbeddingModel     string `json:"embeddingModel,omitempty"`     // auto-selected if omitted
}
```

Model names must include the provider prefix (e.g. `"google/gemini-2.5-flash"`).

---

## Example

```go
// Store a Google AI API key at org level (auto-selects models)
_, err := client.Provider.UpsertOrgConfig(ctx, orgID, provider.ProviderGoogleAI, &provider.UpsertProviderConfigRequest{
    APIKey: os.Getenv("GOOGLE_AI_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}

// List available generative models
models, err := client.Provider.ListModels(ctx, provider.ProviderGoogleAI, provider.ModelTypeGenerative)
if err != nil {
    log.Fatal(err)
}
for _, m := range models {
    fmt.Printf("%s (%s)\n", m.ModelName, m.DisplayName)
}

// Override model selection for a specific project
_, err = client.Provider.UpsertProjectConfig(ctx, projectID, provider.ProviderGoogleAI, &provider.UpsertProviderConfigRequest{
    APIKey:          os.Getenv("GOOGLE_AI_API_KEY"),
    GenerativeModel: "google/gemini-2.5-pro",
    EmbeddingModel:  "google/text-embedding-004",
})
if err != nil {
    log.Fatal(err)
}

// Verify credentials are working
result, err := client.Provider.TestProvider(ctx, provider.ProviderGoogleAI, projectID, "")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("test passed: %v — model: %s\n", result.Success, result.Model)

// Query usage this month
since := time.Now().UTC().AddDate(0, -1, 0)
usage, err := client.Provider.GetProjectUsage(ctx, projectID, since, time.Time{})
if err != nil {
    log.Fatal(err)
}
for _, row := range usage.Data {
    fmt.Printf("%s / %s: $%.4f\n", row.Provider, row.Model, row.EstimatedCostUSD)
}
```
