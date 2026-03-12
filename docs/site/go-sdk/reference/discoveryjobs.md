# Discovery Jobs

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/discoveryjobs`

**Client field:** `client.DiscoveryJobs`

Discovery Jobs perform AI-driven schema analysis on your documents to detect recurring object types and relationships. Once a job completes you can finalize the results into a new or existing Template Pack.

## Methods

### StartDiscovery

```go
func (c *Client) StartDiscovery(ctx context.Context, req *StartDiscoveryRequest) (*StartDiscoveryResponse, error)
```

Starts a new schema discovery job.

`POST /api/discovery-jobs`

| Parameter | Type | Description |
|-----------|------|-------------|
| `DocumentIDs` | `[]string` | IDs of documents to analyse |
| `BatchSize` | `int` | Documents processed per batch (optional) |
| `MinConfidence` | `float32` | Minimum confidence threshold 0–1 (optional) |
| `IncludeRelationships` | `bool` | Also discover relationship types (optional) |
| `MaxIterations` | `int` | Cap on LLM iterations (optional) |

Returns `StartDiscoveryResponse` with `JobID`.

---

### ListJobs

```go
func (c *Client) ListJobs(ctx context.Context) ([]JobListItem, error)
```

Lists all discovery jobs for the current project.

`GET /api/discovery-jobs`

---

### GetJobStatus

```go
func (c *Client) GetJobStatus(ctx context.Context, jobID string) (*JobStatusResponse, error)
```

Returns the current status and discovered types/relationships for a job.

`GET /api/discovery-jobs/:id`

---

### CancelJob

```go
func (c *Client) CancelJob(ctx context.Context, jobID string) (*CancelJobResponse, error)
```

Cancels a running discovery job.

`POST /api/discovery-jobs/:id/cancel`

---

### FinalizeDiscovery

```go
func (c *Client) FinalizeDiscovery(ctx context.Context, jobID string, req *FinalizeDiscoveryRequest) (*FinalizeDiscoveryResponse, error)
```

Converts a completed job's results into a template pack.

`POST /api/discovery-jobs/:id/finalize`

| Parameter | Type | Description |
|-----------|------|-------------|
| `PackName` | `string` | Name for the new template pack |
| `Mode` | `string` | `"create"` or `"update"` |
| `ExistingPackID` | `*string` | Required when `Mode` is `"update"` |
| `IncludedTypes` | `[]IncludedType` | Type definitions to include |
| `IncludedRelationships` | `[]IncludedRelationship` | Relationship definitions to include (optional) |

Returns `FinalizeDiscoveryResponse` with `TemplatePackID`.

---

## Types

### StartDiscoveryRequest

```go
type StartDiscoveryRequest struct {
    DocumentIDs          []string `json:"document_ids"`
    BatchSize            int      `json:"batch_size,omitempty"`
    MinConfidence        float32  `json:"min_confidence,omitempty"`
    IncludeRelationships bool     `json:"include_relationships,omitempty"`
    MaxIterations        int      `json:"max_iterations,omitempty"`
}
```

### StartDiscoveryResponse

```go
type StartDiscoveryResponse struct {
    JobID string `json:"job_id"`
}
```

### JobStatusResponse

```go
type JobStatusResponse struct {
    ID                      string     `json:"id"`
    Status                  string     `json:"status"`
    Progress                JSONMap    `json:"progress"`
    CreatedAt               time.Time  `json:"created_at"`
    StartedAt               *time.Time `json:"started_at,omitempty"`
    CompletedAt             *time.Time `json:"completed_at,omitempty"`
    ErrorMessage            *string    `json:"error_message,omitempty"`
    DiscoveredTypes         JSONArray  `json:"discovered_types"`
    DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
    TemplatePackID          *string    `json:"template_pack_id,omitempty"`
}
```

`Status` values: `"pending"`, `"running"`, `"completed"`, `"failed"`, `"cancelled"`.

### JobListItem

```go
type JobListItem struct {
    ID                      string     `json:"id"`
    Status                  string     `json:"status"`
    Progress                JSONMap    `json:"progress"`
    CreatedAt               time.Time  `json:"created_at"`
    CompletedAt             *time.Time `json:"completed_at,omitempty"`
    DiscoveredTypes         JSONArray  `json:"discovered_types"`
    DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
    TemplatePackID          *string    `json:"template_pack_id,omitempty"`
}
```

### FinalizeDiscoveryRequest / IncludedType / IncludedRelationship

!!! note "JSON naming convention"
    `FinalizeDiscoveryRequest` uses camelCase JSON tags (e.g. `packName`, `existingPackId`) to match the server API contract. `IncludedType` and `IncludedRelationship` use snake_case. Use the struct fields directly — the SDK handles serialization.

```go
type FinalizeDiscoveryRequest struct {
    PackName              string                 `json:"packName"`
    Mode                  string                 `json:"mode"` // "create" | "update"
    ExistingPackID        *string                `json:"existingPackId,omitempty"`
    IncludedTypes         []IncludedType         `json:"includedTypes"`
    IncludedRelationships []IncludedRelationship `json:"includedRelationships,omitempty"`
}

type IncludedType struct {
    TypeName           string         `json:"type_name"`
    Description        string         `json:"description"`
    Properties         map[string]any `json:"properties"`
    RequiredProperties []string       `json:"required_properties"`
    ExampleInstances   []any          `json:"example_instances"`
    Frequency          int            `json:"frequency"`
}

type IncludedRelationship struct {
    SourceType   string `json:"source_type"`
    TargetType   string `json:"target_type"`
    RelationType string `json:"relation_type"`
    Description  string `json:"description"`
    Cardinality  string `json:"cardinality"`
}
```

### FinalizeDiscoveryResponse

```go
type FinalizeDiscoveryResponse struct {
    TemplatePackID string `json:"template_pack_id"`
    Message        string `json:"message"`
}
```

## Example

```go
// Start discovery on a set of documents
res, err := client.DiscoveryJobs.StartDiscovery(ctx, &discoveryjobs.StartDiscoveryRequest{
    DocumentIDs:          []string{"doc-1", "doc-2", "doc-3"},
    IncludeRelationships: true,
    MinConfidence:        0.7,
})
if err != nil {
    log.Fatal(err)
}
jobID := res.JobID

// Poll until complete
for {
    status, err := client.DiscoveryJobs.GetJobStatus(ctx, jobID)
    if err != nil {
        log.Fatal(err)
    }
    if status.Status == "completed" || status.Status == "failed" {
        break
    }
    time.Sleep(5 * time.Second)
}

// Finalize into a new template pack
final, err := client.DiscoveryJobs.FinalizeDiscovery(ctx, jobID, &discoveryjobs.FinalizeDiscoveryRequest{
    PackName: "my-schema-v1",
    Mode:     "create",
    IncludedTypes: selectedTypes, // built from status.DiscoveredTypes
})
```
