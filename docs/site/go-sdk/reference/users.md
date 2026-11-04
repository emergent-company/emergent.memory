# users

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/users`

The `users` client manages the authenticated user's profile.

## Methods

```go
func (c *Client) GetProfile(ctx context.Context) (*UserProfile, error)
func (c *Client) UpdateProfile(ctx context.Context, req *UpdateProfileRequest) (*UserProfile, error)
```

## Key Types

### UserProfile

```go
type UserProfile struct {
    ID        string
    Name      string
    Email     string
    Role      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### UpdateProfileRequest

```go
type UpdateProfileRequest struct {
    Name  string
    Email string
}
```

## Example

```go
// Get the current user's profile
profile, err := client.Users.GetProfile(ctx)
fmt.Printf("Logged in as: %s (%s)\n", profile.Name, profile.Email)

// Update the profile
updated, err := client.Users.UpdateProfile(ctx, &users.UpdateProfileRequest{
    Name: "Jane Smith",
})
```
