# chunking

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chunking`

The `chunking` client re-runs the chunking pipeline on an already-ingested document, using the project's current chunking strategy.

## Methods

```go
func (c *Client) RecreateChunks(ctx context.Context, documentID string) (*RecreateChunksResponse, error)
```

## Key Types

### RecreateChunksResponse

```go
type RecreateChunksResponse struct {
    JobID   string
    Summary RecreateChunksSummary
}

type RecreateChunksSummary struct {
    DocumentID    string
    OldChunkCount int
    NewChunkCount int
    Status        string
}
```

## Example

```go
resp, err := client.Chunking.RecreateChunks(ctx, "doc_abc123")
if err != nil {
    return err
}
fmt.Printf("Re-chunked %s: %d → %d chunks (job: %s)\n",
    resp.Summary.DocumentID,
    resp.Summary.OldChunkCount,
    resp.Summary.NewChunkCount,
    resp.JobID,
)
```

!!! note "When to use"
    Call `RecreateChunks` after changing the project's chunking strategy to apply the new
    strategy to existing documents, without re-uploading them.
