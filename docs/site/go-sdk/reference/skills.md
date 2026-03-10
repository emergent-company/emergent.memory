# skills

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills`

The `skills` client manages skills — reusable Markdown workflow instructions that agents can load on demand via the `skill` tool. Skills are either global (org-wide) or project-scoped. Project-scoped skills override global skills with the same name within that project.

See also: [agentdefinitions](agentdefinitions.md) for the `Tools` field where `"skill"` is opted in.

## Methods

```go
func (c *Client) List(ctx context.Context, projectID string) ([]*Skill, error)
func (c *Client) Get(ctx context.Context, id string) (*Skill, error)
func (c *Client) Create(ctx context.Context, projectID string, req *CreateSkillRequest) (*Skill, error)
func (c *Client) Update(ctx context.Context, id string, req *UpdateSkillRequest) (*Skill, error)
func (c *Client) Delete(ctx context.Context, id string) error
```

Pass an empty `projectID` to `List` and `Create` to operate on global skills. Pass a project ID to list the merged global + project-scoped set, or to create a project-scoped skill.

## Key Types

### Skill

```go
type Skill struct {
    ID          string
    Name        string         // Lowercase slug, e.g. "deploy-checklist"
    Description string
    Content     string         // Full Markdown content
    Metadata    map[string]any
    ProjectID   *string        // nil = global
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### CreateSkillRequest

```go
type CreateSkillRequest struct {
    Name        string         // Required. Lowercase slug, max 64 chars.
    Description string         // Required. Used for semantic retrieval.
    Content     string         // Required. Markdown workflow instructions.
    Metadata    map[string]any // Optional.
}
```

### UpdateSkillRequest

All fields are optional (pointer or omitempty). Only provided fields are updated.

```go
type UpdateSkillRequest struct {
    Description *string
    Content     *string
    Metadata    map[string]any
}
```

## Accessing the client

`skills.Client` is available on the top-level SDK client:

```go
client.Skills.List(ctx, projectID)
client.Skills.Create(ctx, projectID, req)
```

## Example

```go
// Create a global skill
skill, err := client.Skills.Create(ctx, "", &skills.CreateSkillRequest{
    Name:        "deploy-checklist",
    Description: "Step-by-step deployment checklist for production releases",
    Content:     "# Deploy Checklist\n\n1. Run tests\n2. Tag the release\n3. Push to registry\n",
})
fmt.Printf("Created skill: %s (%s)\n", skill.Name, skill.ID)

// Create a project-scoped skill (overrides global for this project)
projectSkill, err := client.Skills.Create(ctx, projectID, &skills.CreateSkillRequest{
    Name:        "deploy-checklist",
    Description: "Custom deploy steps for this project",
    Content:     "# Project Deploy\n\n...",
})

// List merged skills for a project (global + project, project wins on conflict)
allSkills, err := client.Skills.List(ctx, projectID)
for _, s := range allSkills {
    scope := "global"
    if s.ProjectID != nil {
        scope = "project"
    }
    fmt.Printf("%s [%s] — %s\n", s.Name, scope, s.Description)
}

// Update description and content
updated, err := client.Skills.Update(ctx, skill.ID, &skills.UpdateSkillRequest{
    Description: strPtr("Updated description for better semantic retrieval"),
    Content:     strPtr("# Deploy Checklist v2\n\n..."),
})

// Delete
err = client.Skills.Delete(ctx, skill.ID)
```
