# branches

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/branches`

The `branches` client manages graph branches — isolated copies of the graph for staging changes before merging to main.

## Methods

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) ([]*Branch, error)
func (c *Client) Get(ctx context.Context, id string) (*Branch, error)
func (c *Client) Create(ctx context.Context, createReq *CreateBranchRequest) (*Branch, error)
func (c *Client) Update(ctx context.Context, id string, updateReq *UpdateBranchRequest) (*Branch, error)
func (c *Client) Delete(ctx context.Context, id string) error
```

To merge a branch, use `client.Graph.MergeBranch`.

## Key Types

### Branch

```go
type Branch struct {
    ID          string
    Name        string
    Description string
    ProjectID   string
    Status      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### CreateBranchRequest

```go
type CreateBranchRequest struct {
    Name        string
    Description string
}
```

### UpdateBranchRequest

```go
type UpdateBranchRequest struct {
    Name        string
    Description string
}
```

### ListOptions

```go
type ListOptions struct {
    Limit  int
    Offset int
}
```

## Example

```go
// Create a branch for staging changes
branch, err := client.Branches.Create(ctx, &branches.CreateBranchRequest{
    Name:        "feature/add-relationships",
    Description: "Staging branch for new relationship types",
})

// After making changes on the branch, merge back
mergeResp, err := client.Graph.MergeBranch(ctx, "main", &graph.BranchMergeRequest{
    SourceBranchID: branch.ID,
    Strategy:       "merge",
})
fmt.Printf("Merged %d objects, %d relationships\n",
    mergeResp.ObjectSummary.Merged, mergeResp.RelationshipSummary.Merged)
```
