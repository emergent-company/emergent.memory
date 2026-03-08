# datasources

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/datasources`

The `datasources` client manages data source integrations — connections to external systems (Google Drive, Notion, Confluence, etc.) that sync documents into the Emergent platform.

## Methods

```go
func (c *Client) ListProviders(ctx context.Context) ([]Provider, error)
func (c *Client) GetProviderSchema(ctx context.Context, providerType string) (*ProviderSchema, error)
func (c *Client) TestConfig(ctx context.Context, testReq *TestConfigRequest) (*TestConnectionResponse, error)
func (c *Client) GetSourceTypes(ctx context.Context) ([]SourceType, error)
func (c *Client) List(ctx context.Context, opts *ListIntegrationsOptions) ([]Integration, error)
func (c *Client) Get(ctx context.Context, integrationID string) (*Integration, error)
func (c *Client) Create(ctx context.Context, createReq *CreateIntegrationRequest) (*Integration, error)
func (c *Client) Update(ctx context.Context, integrationID string, updateReq *UpdateIntegrationRequest) (*Integration, error)
func (c *Client) Delete(ctx context.Context, integrationID string) error
func (c *Client) TestConnection(ctx context.Context, integrationID string) (*TestConnectionResponse, error)
func (c *Client) TriggerSync(ctx context.Context, integrationID string, syncReq *TriggerSyncRequest) (*TriggerSyncResponse, error)
```

## Key Types

### Provider

```go
type Provider struct {
    Type        string
    Name        string
    Description string
    AuthTypes   []string
}
```

### Integration

```go
type Integration struct {
    ID           string
    Name         string
    ProviderType string
    Status       string
    Config       map[string]interface{}
    LastSyncAt   *time.Time
    ProjectID    string
    CreatedAt    time.Time
}
```

### SyncJob

```go
type SyncJob struct {
    ID           string
    IntegrationID string
    Status       string
    StartedAt    time.Time
    FinishedAt   *time.Time
    DocumentsAdded   int
    DocumentsUpdated int
    DocumentsDeleted int
    Error        string
}
```

### CreateIntegrationRequest

```go
type CreateIntegrationRequest struct {
    Name         string
    ProviderType string
    Config       map[string]interface{}
}
```

### TriggerSyncRequest

```go
type TriggerSyncRequest struct {
    FullSync bool // Force a full re-sync (default: incremental)
}
```

## Example

```go
// List available provider types
providers, err := client.DataSources.ListProviders(ctx)
for _, p := range providers {
    fmt.Printf("%s: %s\n", p.Type, p.Name)
}

// Create a Google Drive integration
integration, err := client.DataSources.Create(ctx, &datasources.CreateIntegrationRequest{
    Name:         "Team Drive",
    ProviderType: "google_drive",
    Config: map[string]interface{}{
        "folder_id":     "1BxiMVs...",
        "service_account": "...",
    },
})

// Trigger sync
syncResp, err := client.DataSources.TriggerSync(ctx, integration.ID, &datasources.TriggerSyncRequest{
    FullSync: false,
})
fmt.Printf("Sync job: %s\n", syncResp.JobID)
```
