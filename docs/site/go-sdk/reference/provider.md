# provider

Package `provider` provides a client for the Emergent Provider API.

The provider client is a **non-context client** — it requires no org or project context headers. It manages LLM credentials at the organization level, the model catalog for each provider, and per-project provider policies (which credential source to use and which models to call).

Supported providers: **Google AI** (`google-ai`) and **Vertex AI** (`vertex-ai`).

## Import

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/provider"
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
    ProviderGoogleAI ProviderType = "google-ai"
    ProviderVertexAI ProviderType = "vertex-ai"
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

### Organization Credential Methods

```go
// Save a Google AI API key for an org
func (c *Client) SaveGoogleAICredential(ctx context.Context, orgID string, req *SaveGoogleAICredentialRequest) error

// Save Vertex AI service account credentials for an org
func (c *Client) SaveVertexAICredential(ctx context.Context, orgID string, req *SaveVertexAICredentialRequest) error

// Delete stored credentials for a provider/org combination
func (c *Client) DeleteOrgCredential(ctx context.Context, orgID, provider string) error

// List stored credential metadata (no secrets) for an org
func (c *Client) ListOrgCredentials(ctx context.Context, orgID string) ([]OrgCredential, error)

// Set default models for an org + provider
func (c *Client) SetOrgModelSelection(ctx context.Context, orgID, provider string, req *SetOrgModelSelectionRequest) error
```

**Endpoints:**
- `POST /api/v1/organizations/{orgID}/providers/google-ai/credentials`
- `POST /api/v1/organizations/{orgID}/providers/vertex-ai/credentials`
- `DELETE /api/v1/organizations/{orgID}/providers/{provider}/credentials`
- `GET /api/v1/organizations/{orgID}/providers/credentials`
- `PUT /api/v1/organizations/{orgID}/providers/{provider}/models`

---

### Model Catalog Methods

```go
// List models for a provider; modelType is optional ("embedding" or "generative")
func (c *Client) ListModels(ctx context.Context, provider, modelType string) ([]SupportedModel, error)
```

**Endpoint:** `GET /api/v1/providers/{provider}/models`

---

### Project Policy Methods

```go
// Set the provider policy for a project
func (c *Client) SetProjectPolicy(ctx context.Context, projectID, provider string, req *SetProjectPolicyRequest) error

// Get the current provider policy for a project
func (c *Client) GetProjectPolicy(ctx context.Context, projectID, provider string) (*ProjectPolicy, error)

// List all provider policies for a project
func (c *Client) ListProjectPolicies(ctx context.Context, projectID string) ([]ProjectPolicy, error)
```

**Endpoints:**
- `PUT /api/v1/projects/{projectID}/providers/{provider}/policy`
- `GET /api/v1/projects/{projectID}/providers/{provider}/policy`
- `GET /api/v1/projects/{projectID}/providers/policies`

---

### Usage Methods

```go
// Aggregated usage + estimated cost for a project (since/until optional; pass zero time.Time to omit)
func (c *Client) GetProjectUsage(ctx context.Context, projectID string, since, until time.Time) (*UsageSummary, error)

// Aggregated usage + estimated cost across all projects in an org
func (c *Client) GetOrgUsage(ctx context.Context, orgID string, since, until time.Time) (*UsageSummary, error)
```

**Endpoints:**
- `GET /api/v1/projects/{projectID}/usage`
- `GET /api/v1/organizations/{orgID}/usage`

---

## Types

### OrgCredential

```go
type OrgCredential struct {
    ID         string    `json:"id"`
    OrgID      string    `json:"orgId"`
    Provider   string    `json:"provider"`
    GCPProject string    `json:"gcpProject,omitempty"`
    Location   string    `json:"location,omitempty"`
    CreatedAt  time.Time `json:"createdAt"`
    UpdatedAt  time.Time `json:"updatedAt"`
}
```

Credential metadata only — no API key or service account JSON is returned.

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

### ProjectPolicy

```go
type ProjectPolicy struct {
    ID              string    `json:"id"`
    ProjectID       string    `json:"projectId"`
    Provider        string    `json:"provider"`
    Policy          string    `json:"policy"` // "none", "organization", "project"
    GCPProject      string    `json:"gcpProject,omitempty"`
    Location        string    `json:"location,omitempty"`
    EmbeddingModel  string    `json:"embeddingModel,omitempty"`
    GenerativeModel string    `json:"generativeModel,omitempty"`
    CreatedAt       time.Time `json:"createdAt"`
    UpdatedAt       time.Time `json:"updatedAt"`
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

### Request types

```go
type SaveGoogleAICredentialRequest struct {
    APIKey string `json:"apiKey"`
}

type SaveVertexAICredentialRequest struct {
    ServiceAccountJSON string `json:"serviceAccountJson"`
    GCPProject         string `json:"gcpProject"`
    Location           string `json:"location"`
}

type SetOrgModelSelectionRequest struct {
    EmbeddingModel  string `json:"embeddingModel"`
    GenerativeModel string `json:"generativeModel"`
}

type SetProjectPolicyRequest struct {
    Policy             string `json:"policy"`
    APIKey             string `json:"apiKey,omitempty"`
    ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
    GCPProject         string `json:"gcpProject,omitempty"`
    Location           string `json:"location,omitempty"`
    EmbeddingModel     string `json:"embeddingModel,omitempty"`
    GenerativeModel    string `json:"generativeModel,omitempty"`
}
```

---

## Example

```go
// Store a Google AI API key for an org
err := client.Provider.SaveGoogleAICredential(ctx, orgID, &provider.SaveGoogleAICredentialRequest{
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

// Configure a project to use org credentials with a specific model
err = client.Provider.SetProjectPolicy(ctx, projectID, provider.ProviderGoogleAI, &provider.SetProjectPolicyRequest{
    Policy:          provider.PolicyOrganization,
    GenerativeModel: "gemini-1.5-pro",
    EmbeddingModel:  "text-embedding-004",
})
if err != nil {
    log.Fatal(err)
}

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
