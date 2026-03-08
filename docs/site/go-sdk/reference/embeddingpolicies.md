# Embedding Policies

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/embeddingpolicies`

**Client field:** `client.EmbeddingPolicies`

Embedding policies control how graph objects are vectorized: which object types to embed, which fields to use, the text template, and the embedding model. Multiple active policies can coexist in a project.

## Methods

### List

```go
func (c *Client) List(ctx context.Context) ([]EmbeddingPolicy, error)
```

Lists all embedding policies for the current project.

`GET /api/graph/embedding-policies?project_id=<projectID>`

---

### GetByID

```go
func (c *Client) GetByID(ctx context.Context, policyID string) (*EmbeddingPolicy, error)
```

Returns a single policy by ID.

`GET /api/graph/embedding-policies/:id?project_id=<projectID>`

---

### Create

```go
func (c *Client) Create(ctx context.Context, req *CreateEmbeddingPolicyRequest) (*EmbeddingPolicy, error)
```

Creates a new embedding policy.

`POST /api/graph/embedding-policies`

---

### Update

```go
func (c *Client) Update(ctx context.Context, policyID string, req *UpdateEmbeddingPolicyRequest) (*EmbeddingPolicy, error)
```

Updates an existing embedding policy. All fields are optional — only supplied fields are changed.

`PUT /api/graph/embedding-policies/:id?project_id=<projectID>`

---

### Delete

```go
func (c *Client) Delete(ctx context.Context, policyID string) error
```

Deletes an embedding policy.

`DELETE /api/graph/embedding-policies/:id?project_id=<projectID>`

---

## Types

### EmbeddingPolicy

```go
type EmbeddingPolicy struct {
    ID             string    `json:"id"`
    ProjectID      string    `json:"project_id"`
    Name           string    `json:"name"`
    Description    *string   `json:"description,omitempty"`
    ObjectTypes    []string  `json:"object_types"`
    Fields         []string  `json:"fields"`
    Template       string    `json:"template"`
    Model          string    `json:"model"`
    Active         bool      `json:"active"`
    ChunkingConfig any       `json:"chunking_config,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

| Field | Description |
|-------|-------------|
| `ObjectTypes` | Object type names this policy applies to |
| `Fields` | Object property names to include in the embedding text |
| `Template` | Go text/template string for composing the embedding input |
| `Model` | Embedding model identifier (e.g. `"text-embedding-004"`) |
| `Active` | When `false` the policy is disabled but not deleted |
| `ChunkingConfig` | Optional chunking configuration (provider-specific JSON) |

### CreateEmbeddingPolicyRequest

```go
type CreateEmbeddingPolicyRequest struct {
    ProjectID      string   `json:"projectId"`
    Name           string   `json:"name"`
    Description    *string  `json:"description,omitempty"`
    ObjectTypes    []string `json:"object_types"`
    Fields         []string `json:"fields"`
    Template       string   `json:"template"`
    Model          string   `json:"model"`
    Active         *bool    `json:"active,omitempty"`
    ChunkingConfig any      `json:"chunking_config,omitempty"`
}
```

### UpdateEmbeddingPolicyRequest

```go
type UpdateEmbeddingPolicyRequest struct {
    Name           *string  `json:"name,omitempty"`
    Description    *string  `json:"description,omitempty"`
    ObjectTypes    []string `json:"object_types,omitempty"`
    Fields         []string `json:"fields,omitempty"`
    Template       *string  `json:"template,omitempty"`
    Model          *string  `json:"model,omitempty"`
    Active         *bool    `json:"active,omitempty"`
    ChunkingConfig any      `json:"chunking_config,omitempty"`
}
```

## Example

```go
active := true
policy, err := client.EmbeddingPolicies.Create(ctx, &embeddingpolicies.CreateEmbeddingPolicyRequest{
    ProjectID:   "proj-abc",
    Name:        "person-embeddings",
    ObjectTypes: []string{"Person"},
    Fields:      []string{"name", "bio", "skills"},
    Template:    "{{.name}}: {{.bio}}. Skills: {{.skills}}",
    Model:       "text-embedding-004",
    Active:      &active,
})
if err != nil {
    log.Fatal(err)
}
fmt.Println("Created policy:", policy.ID)

// Disable without deleting
boolFalse := false
_, err = client.EmbeddingPolicies.Update(ctx, policy.ID, &embeddingpolicies.UpdateEmbeddingPolicyRequest{
    Active: &boolFalse,
})
```
