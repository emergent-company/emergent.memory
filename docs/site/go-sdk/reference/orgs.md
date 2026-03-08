# orgs

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/orgs`

The `orgs` client manages Emergent organizations — the top-level containers for projects.

## Methods

```go
func (c *Client) List(ctx context.Context) ([]Organization, error)
func (c *Client) Get(ctx context.Context, id string) (*Organization, error)
func (c *Client) Create(ctx context.Context, req *CreateOrganizationRequest) (*Organization, error)
func (c *Client) Delete(ctx context.Context, id string) error
```

## Key Types

### Organization

```go
type Organization struct {
    ID        string
    Name      string
    Slug      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### CreateOrganizationRequest

```go
type CreateOrganizationRequest struct {
    Name string
    Slug string
}
```

## Example

```go
// List all organizations the current user belongs to
orgList, err := client.Orgs.List(ctx)
for _, org := range orgList {
    fmt.Printf("%s (%s)\n", org.Name, org.ID)
}

// Create a new organization
org, err := client.Orgs.Create(ctx, &orgs.CreateOrganizationRequest{
    Name: "Acme Corp",
    Slug: "acme",
})
```
