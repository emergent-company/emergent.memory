# projects

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects`

The `projects` client manages Emergent projects — the primary unit of organization for graph data, documents, and agents.

## Methods

```go
func (c *Client) List(ctx context.Context, opts *ListOptions) ([]Project, error)
func (c *Client) Get(ctx context.Context, id string, opts *GetOptions) (*Project, error)
func (c *Client) Create(ctx context.Context, req *CreateProjectRequest) (*Project, error)
func (c *Client) Update(ctx context.Context, id string, req *UpdateProjectRequest) (*Project, error)
func (c *Client) Delete(ctx context.Context, id string) error
func (c *Client) ListMembers(ctx context.Context, projectID string) ([]ProjectMember, error)
func (c *Client) RemoveMember(ctx context.Context, projectID, userID string) error
```

## Key Types

### Project

```go
type Project struct {
    ID             string
    Name           string
    OrgID          string
    Description    string
    Status         string
    TemplatePack   *TemplatePack
    Stats          *ProjectStats
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### ProjectStats

```go
type ProjectStats struct {
    ObjectCount       int
    RelationshipCount int
    DocumentCount     int
    ChunkCount        int
}
```

### ProjectMember

```go
type ProjectMember struct {
    UserID    string
    ProjectID string
    Role      string
    JoinedAt  time.Time
}
```

### CreateProjectRequest

```go
type CreateProjectRequest struct {
    Name        string
    OrgID       string
    Description string
}
```

### UpdateProjectRequest

```go
type UpdateProjectRequest struct {
    Name        string
    Description string
    Status      string
}
```

### ListOptions

```go
type ListOptions struct {
    OrgID  string
    Limit  int
    Offset int
}
```

### GetOptions

```go
type GetOptions struct {
    IncludeStats bool
}
```

## Example

```go
// List all projects
projects, err := client.Projects.List(ctx, &projects.ListOptions{
    OrgID: "org_abc123",
})

// Get a project with stats
proj, err := client.Projects.Get(ctx, "proj_xyz", &projects.GetOptions{
    IncludeStats: true,
})
fmt.Printf("%s: %d objects, %d documents\n",
    proj.Name, proj.Stats.ObjectCount, proj.Stats.DocumentCount)

// List members
members, err := client.Projects.ListMembers(ctx, "proj_xyz")
```
