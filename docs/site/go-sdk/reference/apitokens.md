# apitokens

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apitokens`

The `apitokens` client manages project-scoped API tokens (`emt_*` prefix). Tokens are used to authenticate SDK clients with `Mode: "apitoken"` or `Mode: "apikey"`.

## Methods

```go
func (c *Client) Create(ctx context.Context, projectID string, req *CreateTokenRequest) (*CreateTokenResponse, error)
func (c *Client) List(ctx context.Context, projectID string) (*ListResponse, error)
func (c *Client) Get(ctx context.Context, projectID, tokenID string) (*APIToken, error)
func (c *Client) Revoke(ctx context.Context, projectID, tokenID string) error
```

## Key Types

### APIToken

```go
type APIToken struct {
    ID          string
    Name        string
    ProjectID   string
    Prefix      string    // First few chars of the token (for identification)
    ExpiresAt   *time.Time
    LastUsedAt  *time.Time
    CreatedAt   time.Time
}
```

### CreateTokenResponse

```go
type CreateTokenResponse struct {
    Token     string    // Full token value — only returned on creation
    APIToken
}
```

!!! warning "Token value shown once"
    The full token value is only returned in `CreateTokenResponse.Token`. After creation,
    only the prefix is stored on the server. Store the token securely before the response
    is discarded.

### CreateTokenRequest

```go
type CreateTokenRequest struct {
    Name      string
    ExpiresAt *time.Time // nil for no expiration
}
```

### ListResponse

```go
type ListResponse struct {
    Tokens []APIToken
}
```

## Example

```go
// Create a token
resp, err := client.APITokens.Create(ctx, "proj_xyz", &apitokens.CreateTokenRequest{
    Name: "CI/CD pipeline token",
})
if err != nil {
    return err
}
// Save resp.Token — it won't be shown again
fmt.Printf("Created token: %s (prefix: %s)\n", resp.Token, resp.Prefix)

// List tokens
list, err := client.APITokens.List(ctx, "proj_xyz")
for _, tok := range list.Tokens {
    fmt.Printf("%s — %s\n", tok.Name, tok.Prefix)
}

// Revoke a token
err = client.APITokens.Revoke(ctx, "proj_xyz", "tok_abc123")
```
