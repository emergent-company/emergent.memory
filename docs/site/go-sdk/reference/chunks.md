# chunks

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chunks`

The `chunks` client provides access to document chunks — the text segments produced by the chunking pipeline after document ingestion.

## Methods

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) (*ListResponse, error)
func (c *Client) Delete(ctx context.Context, id string) error
func (c *Client) BulkDelete(ctx context.Context, ids []string) (*BulkDeletionSummary, error)
func (c *Client) DeleteByDocument(ctx context.Context, documentID string) (*DocumentChunksDeletionResult, error)
func (c *Client) BulkDeleteByDocuments(ctx context.Context, documentIDs []string) (*BulkDocumentChunksDeletionSummary, error)
```

## Key Types

### Chunk

```go
type Chunk struct {
    ID         string
    DocumentID string
    Content    string
    Index      int
    Metadata   *ChunkMetadata
    CreatedAt  time.Time
}
```

### ChunkMetadata

```go
type ChunkMetadata struct {
    SourceType string
    PageNumber int
    Section    string
    Tokens     int
}
```

### ListOptions

```go
type ListOptions struct {
    DocumentID string
    Limit      int
    Offset     int
}
```

### ListResponse

```go
type ListResponse struct {
    Chunks []Chunk
    Total  int
}
```

### BulkDeletionSummary

```go
type BulkDeletionSummary struct {
    Deleted int
    Failed  int
    Results []DeletionResult
}
```

## Example

```go
// List chunks for a document
resp, err := client.Chunks.List(ctx, &chunks.ListOptions{
    DocumentID: "doc_abc123",
    Limit:      50,
})
for _, chunk := range resp.Chunks {
    fmt.Printf("[%d] %s\n", chunk.Index, chunk.Content[:min(80, len(chunk.Content))])
}

// Delete all chunks for a document
result, err := client.Chunks.DeleteByDocument(ctx, "doc_abc123")
fmt.Printf("Deleted %d chunks\n", result.Deleted)
```
