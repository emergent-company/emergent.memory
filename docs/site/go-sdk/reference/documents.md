# documents

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents`

The `documents` client manages documents stored in the Emergent platform — upload, retrieval, deletion, and content access.

## Methods

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResult, error)
func (c *Client) Get(ctx context.Context, id string) (*Document, error)
func (c *Client) Create(ctx context.Context, createReq *CreateRequest) (*Document, error)
func (c *Client) Delete(ctx context.Context, id string) (*DeleteResponse, error)
func (c *Client) BulkDelete(ctx context.Context, ids []string) (*DeleteResponse, error)
func (c *Client) GetContent(ctx context.Context, id string) (*string, error)
```

## Key Types

### Document

```go
type Document struct {
    ID         string
    Filename   string
    SourceType string
    Size       int64
    Status     string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    ProjectID  string
}
```

### ListOptions

```go
type ListOptions struct {
    Limit      int
    Offset     int
    SourceType string
    Status     string
    Query      string
}
```

### ListResult

```go
type ListResult struct {
    Documents []Document
    Total     int
}
```

### CreateRequest

```go
type CreateRequest struct {
    Filename   string
    Content    []byte
    SourceType string
}
```

### DeleteResponse

```go
type DeleteResponse struct {
    Deleted int
    Summary DeleteSummary
}

type DeleteSummary struct {
    ObjectsDeleted       int
    RelationshipsDeleted int
    ChunksDeleted        int
}
```

### DeletionImpact / BulkDeletionImpact

```go
type DeletionImpact struct {
    DocumentID           string
    ObjectsToDelete      int
    RelationshipsToDelete int
    ChunksToDelete       int
}

type BulkDeletionImpact struct {
    Documents []DeletionImpact
    Summary   ImpactSummary
}
```

## Example

```go
// List documents
result, err := client.Documents.List(ctx, &documents.ListOptions{
    Limit:  20,
    Status: "processed",
})

// Get a single document
doc, err := client.Documents.Get(ctx, "doc_abc123")

// Get the raw text content
text, err := client.Documents.GetContent(ctx, doc.ID)

// Delete a document (also removes its chunks and graph objects)
resp, err := client.Documents.Delete(ctx, doc.ID)
fmt.Printf("Deleted %d objects, %d chunks\n",
    resp.Summary.ObjectsDeleted, resp.Summary.ChunksDeleted)
```
