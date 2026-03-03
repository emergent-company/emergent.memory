# graph

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph`

The `graph` client provides full access to the Emergent knowledge graph — objects, relationships, search, traversal, analytics, and branch merging.

!!! tip "Dual-ID Model"
    Graph objects have two IDs: `VersionID` (changes on update) and `EntityID` (stable forever).
    See the [Graph ID Model guide](../graph-id-model.md) for details.

## Object Methods

```go
func (c *Client) CreateObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error)
func (c *Client) UpsertObject(ctx context.Context, req *CreateObjectRequest) (*GraphObject, error)
func (c *Client) GetObject(ctx context.Context, id string) (*GraphObject, error)
func (c *Client) GetByAnyID(ctx context.Context, id string) (*GraphObject, error)
func (c *Client) GetObjects(ctx context.Context, ids []string) ([]*GraphObject, error)
func (c *Client) UpdateObject(ctx context.Context, id string, req *UpdateObjectRequest) (*GraphObject, error)
func (c *Client) DeleteObject(ctx context.Context, id string) error
func (c *Client) RestoreObject(ctx context.Context, id string) (*GraphObject, error)
func (c *Client) GetObjectHistory(ctx context.Context, id string) (*ObjectHistoryResponse, error)
func (c *Client) GetObjectEdges(ctx context.Context, id string, opts *GetObjectEdgesOptions) (*GetObjectEdgesResponse, error)
func (c *Client) ListObjects(ctx context.Context, opts *ListObjectsOptions) (*SearchObjectsResponse, error)
func (c *Client) CountObjects(ctx context.Context, opts *CountObjectsOptions) (int, error)
func (c *Client) BulkUpdateStatus(ctx context.Context, req *BulkUpdateStatusRequest) (*BulkUpdateStatusResponse, error)
func (c *Client) BulkCreateObjects(ctx context.Context, req *BulkCreateObjectsRequest) (*BulkCreateObjectsResponse, error)
func (c *Client) ListTags(ctx context.Context, opts *ListTagsOptions) ([]string, error)
```

## Search Methods

```go
func (c *Client) FTSSearch(ctx context.Context, opts *FTSSearchOptions) (*SearchResponse, error)
func (c *Client) VectorSearch(ctx context.Context, req *VectorSearchRequest) (*SearchResponse, error)
func (c *Client) HybridSearch(ctx context.Context, req *HybridSearchRequest) (*SearchResponse, error)
func (c *Client) FindSimilar(ctx context.Context, id string, opts *FindSimilarOptions) ([]SimilarObjectResult, error)
func (c *Client) SearchWithNeighbors(ctx context.Context, req *SearchWithNeighborsRequest) (*SearchWithNeighborsResponse, error)
```

## Traversal Methods

```go
func (c *Client) ExpandGraph(ctx context.Context, req *GraphExpandRequest) (*GraphExpandResponse, error)
func (c *Client) TraverseGraph(ctx context.Context, req *TraverseGraphRequest) (*TraverseGraphResponse, error)
```

## Relationship Methods

Methods for creating and managing relationships are called on the same `graph.Client` — see the `CreateRelationshipRequest` / `ListRelationshipsOptions` types below.

## Branch Methods

```go
func (c *Client) MergeBranch(ctx context.Context, targetBranchID string, req *BranchMergeRequest) (*BranchMergeResponse, error)
```

## Key Types

### GraphObject

```go
type GraphObject struct {
    VersionID   string                 // Mutable — changes on UpdateObject
    EntityID    string                 // Stable — never changes
    ID          string                 // Deprecated: use VersionID
    CanonicalID string                 // Deprecated: use EntityID
    Type        string
    Key         string
    Status      string
    Properties  map[string]interface{}
    Labels      []string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### CreateObjectRequest

```go
type CreateObjectRequest struct {
    Type       string                 // Required
    Key        string
    Status     string
    Properties map[string]interface{}
    Labels     []string
}
```

### UpdateObjectRequest

```go
type UpdateObjectRequest struct {
    Status     string
    Properties map[string]interface{}
    Labels     []string
}
```

### ListObjectsOptions

```go
type ListObjectsOptions struct {
    Type            string
    Status          string
    Labels          []string
    PropertyFilters []PropertyFilter
    Limit           int
    Offset          int
    SortBy          string
    SortOrder       string
}
```

### PropertyFilter

```go
type PropertyFilter struct {
    Field    string
    Operator string // "eq", "neq", "gt", "gte", "lt", "lte", "contains", "startsWith", "exists"
    Value    interface{}
}
```

### ListTagsOptions

```go
type ListTagsOptions struct {
    Type   string
    Prefix string
    Limit  int
}
```

### BulkCreateObjectsRequest

```go
type BulkCreateObjectsRequest struct {
    Objects []*CreateObjectRequest // Max 100 items
}
```

### HybridSearchRequest

```go
type HybridSearchRequest struct {
    Query       string
    Types       []string
    Labels      []string
    Limit       int
    Offset      int
    Alpha       float64 // Weight between lexical (0) and vector (1); default 0.5
}
```

### GraphExpandRequest

```go
type GraphExpandRequest struct {
    ObjectID   string
    Depth      int
    Projection *GraphExpandProjection
    Filters    *GraphExpandFilters
}
```

### BranchMergeRequest

```go
type BranchMergeRequest struct {
    SourceBranchID string
    Strategy       string // "merge", "replace"
}
```
