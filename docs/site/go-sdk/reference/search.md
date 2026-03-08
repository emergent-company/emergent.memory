# search

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/search`

The `search` client provides unified search over the knowledge graph, supporting lexical, semantic (vector), and hybrid strategies across graph objects, text chunks, and relationships.

## Methods

```go
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)
```

## Key Types

### SearchRequest

```go
type SearchRequest struct {
    Query          string // Search query text (required)
    Limit          int    // Max results (default 10)
    Strategy       string // "hybrid" (default), "semantic", "keyword"
    ResultTypes    string // "graph", "text", "both" (default: "both")
    FusionStrategy string // "weighted", "rrf", "interleave", "graph_first", "text_first"
    IncludeDebug   bool   // Include scoring debug info in results
}
```

### SearchResponse

```go
type SearchResponse struct {
    Results  []SearchResult
    Metadata SearchMetadata
    Total    int  // Convenience alias for Metadata.Total
}
```

### SearchResult

Results are unified across types — check the `Type` field to determine which fields are populated.

```go
type SearchResult struct {
    // Common fields
    Type  string  // "graph", "text", or "relationship"
    Score float32
    Rank  int

    // Graph object fields (Type == "graph")
    ObjectID        string
    CanonicalID     string
    ObjectType      string
    Key             string
    Fields          map[string]any
    LexicalScore    *float32
    VectorScore     *float32
    TruncatedFields []string

    // Text chunk fields (Type == "text")
    DocumentID string
    ChunkID    string
    Content    string

    // Relationship fields (Type == "relationship")
    RelationshipType string
    SrcObjectID      string
    SrcObjectType    string
    SrcKey           *string
    DstObjectID      string
    DstObjectType    string
    DstKey           *string
}
```

### SearchMetadata

```go
type SearchMetadata struct {
    Total      int     // json: "totalResults"
    GraphCount int     // json: "graphResultCount"
    TextCount  int     // json: "textResultCount"
    RelCount   int     // json: "relationshipResultCount"
    ElapsedMs  float64 // json: "elapsed_ms"
}
```

## Example

```go
resp, err := client.Search.Search(ctx, &search.SearchRequest{
    Query:       "machine learning pipeline",
    Limit:       20,
    Strategy:    "hybrid",
    ResultTypes: "both",
})
if err != nil {
    return err
}
fmt.Printf("Found %d results in %.1fms\n", resp.Total, resp.Metadata.ElapsedMs)
for _, r := range resp.Results {
    switch r.Type {
    case "graph":
        fmt.Printf("[graph] %s %s (score: %.3f)\n", r.ObjectType, r.Key, r.Score)
    case "text":
        fmt.Printf("[text] chunk %s (score: %.3f): %s\n", r.ChunkID, r.Score, r.Content[:80])
    case "relationship":
        fmt.Printf("[rel] %s -> %s -> %s\n", r.SrcObjectID, r.RelationshipType, r.DstObjectID)
    }
}
```

## Related

For lower-level search operations directly on the graph (FTS, vector, hybrid, find-similar, search-with-neighbors), see the [graph reference](graph.md).
