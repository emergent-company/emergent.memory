# graph

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph`

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

```go
func (c *Client) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) (*GraphRelationship, error)
func (c *Client) BulkCreateRelationships(ctx context.Context, req *BulkCreateRelationshipsRequest) (*BulkCreateRelationshipsResponse, error)
func (c *Client) GetRelationship(ctx context.Context, id string) (*GraphRelationship, error)
func (c *Client) UpdateRelationship(ctx context.Context, id string, req *UpdateRelationshipRequest) (*GraphRelationship, error)
func (c *Client) DeleteRelationship(ctx context.Context, id string) error
func (c *Client) RestoreRelationship(ctx context.Context, id string) (*GraphRelationship, error)
func (c *Client) GetRelationshipHistory(ctx context.Context, id string) (*RelationshipHistoryResponse, error)
func (c *Client) ListRelationships(ctx context.Context, opts *ListRelationshipsOptions) (*SearchRelationshipsResponse, error)
func (c *Client) HasRelationship(ctx context.Context, relType, srcID, dstID string) (bool, error)
```

## Analytics Methods

```go
func (c *Client) GetMostAccessed(ctx context.Context, opts *AnalyticsOptions) (*MostAccessedResponse, error)
func (c *Client) GetUnused(ctx context.Context, opts *UnusedOptions) (*UnusedObjectsResponse, error)
```

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

### GraphRelationship

```go
type GraphRelationship struct {
    VersionID   string         // Preferred: version-specific ID (changes on update)
    EntityID    string         // Preferred: stable ID (never changes)
    ID          string         // Deprecated: use VersionID
    CanonicalID string         // Deprecated: use EntityID
    ProjectID   string
    BranchID    *string
    Version     int
    Type        string
    SrcID       string
    DstID       string
    Properties  map[string]any
    Weight      *float32
    DeletedAt   *time.Time
    CreatedAt   time.Time
    // InverseRelationship is set when an inverse was auto-created from template pack config
    InverseRelationship *GraphRelationship
}
```

### CreateRelationshipRequest

!!! tip
    Use `CanonicalID` (not `ID`) for `SrcID` and `DstID` to avoid stale version-specific IDs after an `UpdateObject` call.

```go
type CreateRelationshipRequest struct {
    Type       string
    SrcID      string
    DstID      string
    Properties map[string]any
    Weight     *float32
    BranchID   *string
}
```

### UpdateRelationshipRequest

```go
type UpdateRelationshipRequest struct {
    Properties map[string]any
    Weight     *float32
}
```

### ListRelationshipsOptions

```go
type ListRelationshipsOptions struct {
    Type           string   // Single type filter
    Types          []string // Multiple type filter
    SrcID          string
    DstID          string
    ObjectID       string   // Either side
    BranchID       string
    IncludeDeleted bool
    Limit          int
    Cursor         string
}
```

### SearchRelationshipsResponse

```go
type SearchRelationshipsResponse struct {
    Items      []*GraphRelationship
    NextCursor *string
    Total      int
}
```

### BulkCreateRelationshipsRequest / Response

```go
type BulkCreateRelationshipsRequest struct {
    Items []CreateRelationshipRequest // Max 100
}

type BulkCreateRelationshipsResponse struct {
    Success int
    Failed  int
    Results []BulkCreateRelationshipResult
}

type BulkCreateRelationshipResult struct {
    Index        int
    Success      bool
    Relationship *GraphRelationship
    Error        *string
}
```

### RelationshipHistoryResponse

```go
type RelationshipHistoryResponse struct {
    Versions []*GraphRelationship
}
```

### AnalyticsOptions / UnusedOptions

```go
type AnalyticsOptions struct {
    Limit    int
    Types    []string
    Labels   []string
    BranchID string
    Order    string
}

type UnusedOptions struct {
    Limit    int
    Types    []string
    Labels   []string
    BranchID string
    DaysIdle int
}
```

### MostAccessedResponse / UnusedObjectsResponse

```go
type MostAccessedResponse struct {
    Items []AnalyticsObjectItem
    Total int
    Meta  map[string]any
}

type UnusedObjectsResponse struct {
    Items []AnalyticsObjectItem
    Total int
    Meta  map[string]any
}

type AnalyticsObjectItem struct {
    ID              string
    CanonicalID     string
    Type            string
    Key             *string
    Properties      map[string]any
    Labels          []string
    LastAccessedAt  *time.Time
    AccessCount     *int64
    DaysSinceAccess *int
    CreatedAt       time.Time
}
```
