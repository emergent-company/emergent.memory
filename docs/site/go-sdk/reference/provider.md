# provider

Package `provider` provides a client for the Memory Provider API.

The provider client is a **non-context client** — it requires no org or project context headers, so every call takes the project ID explicitly. It manages project-scoped LLM credentials, the model catalog for each provider, and each project's model selection.

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

### ModelType

```go
const (
    ModelTypeEmbedding  ModelType = "embedding"
    ModelTypeGenerative ModelType = "generative"
)
```

---

## Methods

### Organization Config Methods (removed)

Organization-level provider config is no longer supported. `UpsertOrgConfig` and
`ListOrgConfigs` remain only as deprecated stubs that return an error; `GetOrgConfig`
and `DeleteOrgConfig` are gone. Use the project config methods below.

---

### Project Config Methods

```go
// Store a project's provider credential + auto-select models.
// Runs a live credential test and syncs the model catalog on success.
func (c *Client) UpsertProjectConfig(ctx context.Context, projectID, provider string, req *UpsertProviderConfigRequest) (*ProviderConfig, error)

// Get the project's provider config metadata (no secrets).
func (c *Client) GetProjectConfig(ctx context.Context, projectID, provider string) (*ProviderConfig, error)

// Delete the project's provider config.
func (c *Client) DeleteProjectConfig(ctx context.Context, projectID, provider string) error

// List all provider configs for a project.
func (c *Client) ListProjectConfigs(ctx context.Context, projectID string) ([]ProjectProviderConfig, error)

// List project-level configs across all projects in an org.
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
// Store a Google AI API key for a project (auto-selects models)
_, err := client.Provider.UpsertProjectConfig(ctx, projectID, provider.ProviderGoogleAI, &provider.UpsertProviderConfigRequest{
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
