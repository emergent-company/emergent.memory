# search

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search`

The `search` client provides unified search over the knowledge graph, supporting lexical, semantic (vector), and hybrid strategies.

## Methods

```go
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)
```

## Key Types

### SearchRequest

```go
type SearchRequest struct {
    Query  string   // Search query text
    Types  []string // Filter by object types
    Limit  int      // Max results (default 10)
    Offset int      // Pagination offset
}
```

### SearchResponse

```go
type SearchResponse struct {
    Results  []*SearchResult
    Total    int
    Metadata *SearchMetadata
}
```

### SearchResult

```go
type SearchResult struct {
    Object      *graph.GraphObject
    Score       float64
    Highlights  []string
    MatchedOn   string // "lexical", "vector", or "hybrid"
}
```

### SearchMetadata

```go
type SearchMetadata struct {
    Query       string
    TotalFound  int
    SearchType  string
    TimingMs    float64
}
```

## Example

```go
resp, err := client.Search.Search(ctx, &search.SearchRequest{
    Query: "machine learning pipeline",
    Types: []string{"Document", "Note"},
    Limit: 20,
})
if err != nil {
    return err
}
for _, r := range resp.Results {
    fmt.Printf("%s (score: %.3f)\n", r.Object.EntityID, r.Score)
}
```

## Related

For more advanced search operations on the graph directly (FTS, vector, hybrid, find-similar, search-with-neighbors), see the [graph reference](graph.md) which exposes `FTSSearch`, `VectorSearch`, `HybridSearch`, `FindSimilar`, and `SearchWithNeighbors`.
