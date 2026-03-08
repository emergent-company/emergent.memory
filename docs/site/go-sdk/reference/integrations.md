# Integrations

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/integrations`

**Client field:** `client.Integrations`

Integrations connect the Emergent platform to third-party data sources (e.g. Confluence, Notion, GitHub). Once configured, you can trigger syncs to import content as documents and graph objects.

## Methods

### ListAvailable

```go
func (c *Client) ListAvailable(ctx context.Context) ([]AvailableIntegration, error)
```

Returns all integration providers supported by this Emergent deployment.

`GET /api/integrations/available`

---

### List

```go
func (c *Client) List(ctx context.Context) ([]Integration, error)
```

Lists all configured integrations for the current project.

`GET /api/integrations`

---

### Get

```go
func (c *Client) Get(ctx context.Context, name string) (*Integration, error)
```

Returns a specific integration by name. Includes sensitive config fields.

`GET /api/integrations/:name`

---

### GetPublic

```go
func (c *Client) GetPublic(ctx context.Context, name string) (*PublicIntegration, error)
```

Returns the non-sensitive view of an integration (no credentials).

`GET /api/integrations/:name/public`

---

### Create

```go
func (c *Client) Create(ctx context.Context, req *CreateIntegrationRequest) (*Integration, error)
```

Configures a new integration for the current project.

`POST /api/integrations`

---

### Update

```go
func (c *Client) Update(ctx context.Context, name string, req *UpdateIntegrationRequest) (*Integration, error)
```

Replaces an integration's configuration (full PUT).

`PUT /api/integrations/:name`

---

### Delete

```go
func (c *Client) Delete(ctx context.Context, name string) error
```

Removes an integration and its configuration.

`DELETE /api/integrations/:name`

---

### TestConnection

```go
func (c *Client) TestConnection(ctx context.Context, name string) (*TestConnectionResponse, error)
```

Tests whether the integration can reach its external service.

`POST /api/integrations/:name/test`

---

### TriggerSync

```go
func (c *Client) TriggerSync(ctx context.Context, name string, config *TriggerSyncConfig) (*TriggerSyncResponse, error)
```

Triggers an immediate sync. `config` is optional — pass `nil` for defaults.

`POST /api/integrations/:name/sync`

---

## Types

### AvailableIntegration

```go
type AvailableIntegration struct {
    Name             string   `json:"name"`
    DisplayName      string   `json:"display_name"`
    Description      string   `json:"description"`
    Category         string   `json:"category"`
    AuthType         string   `json:"auth_type"`
    RequiredFields   []string `json:"required_fields"`
    OptionalFields   []string `json:"optional_fields,omitempty"`
    DocumentationURL string   `json:"documentation_url,omitempty"`
}
```

### Integration

```go
type Integration struct {
    ID           string     `json:"id"`
    Name         string     `json:"name"`
    DisplayName  string     `json:"display_name"`
    ProviderType string     `json:"provider_type"`
    OrgID        string     `json:"org_id"`
    ProjectID    string     `json:"project_id"`
    Status       string     `json:"status"`
    Config       any        `json:"config,omitempty"`
    LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
    CreatedAt    time.Time  `json:"created_at"`
    UpdatedAt    time.Time  `json:"updated_at"`
}
```

`Status` values: `"active"`, `"inactive"`, `"error"`.

### PublicIntegration

Non-sensitive fields only (omits `Config`):

```go
type PublicIntegration struct {
    ID           string     `json:"id"`
    Name         string     `json:"name"`
    DisplayName  string     `json:"display_name"`
    ProviderType string     `json:"provider_type"`
    Status       string     `json:"status"`
    LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
    CreatedAt    time.Time  `json:"created_at"`
}
```

### CreateIntegrationRequest

```go
type CreateIntegrationRequest struct {
    Name         string `json:"name"`
    DisplayName  string `json:"display_name"`
    ProviderType string `json:"provider_type"`
    Config       any    `json:"config"`
}
```

`Config` shape varies by `ProviderType`; consult `ListAvailable().RequiredFields`.

### UpdateIntegrationRequest

```go
type UpdateIntegrationRequest struct {
    DisplayName string `json:"display_name,omitempty"`
    Config      any    `json:"config,omitempty"`
    Status      string `json:"status,omitempty"`
}
```

### TriggerSyncConfig

```go
type TriggerSyncConfig struct {
    FullSync        *bool    `json:"full_sync,omitempty"`
    SourceTypes     []string `json:"source_types,omitempty"`
    SpaceIDs        []string `json:"space_ids,omitempty"`
    IncludeArchived *bool    `json:"includeArchived,omitempty"`
    BatchSize       *int     `json:"batchSize,omitempty"`
}
```

### TestConnectionResponse / TriggerSyncResponse

```go
type TestConnectionResponse struct {
    Success bool   `json:"success"`
    Message string `json:"message"`
}

type TriggerSyncResponse struct {
    Success bool    `json:"success"`
    Message string  `json:"message"`
    JobID   *string `json:"job_id,omitempty"`
}
```

## Example

```go
// Create a Confluence integration
integration, err := client.Integrations.Create(ctx, &integrations.CreateIntegrationRequest{
    Name:         "confluence-main",
    DisplayName:  "Confluence (Main)",
    ProviderType: "confluence",
    Config: map[string]any{
        "base_url": "https://myorg.atlassian.net",
        "api_token": os.Getenv("CONFLUENCE_TOKEN"),
        "email":    "user@example.com",
    },
})
if err != nil {
    log.Fatal(err)
}

// Test the connection
test, _ := client.Integrations.TestConnection(ctx, integration.Name)
fmt.Println("Connection OK:", test.Success)

// Trigger a full sync
fullSync := true
syncResp, err := client.Integrations.TriggerSync(ctx, integration.Name, &integrations.TriggerSyncConfig{
    FullSync: &fullSync,
})
fmt.Println("Sync job:", syncResp.JobID)
```
