# Template Packs

**Package:** `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/templatepacks`

**Client field:** `client.TemplatePacks`

Template packs are versioned bundles of object type schemas, relationship type schemas, UI configs, and extraction prompts. They can be installed on projects to bootstrap a consistent graph schema, and are also produced by [Discovery Jobs](discoveryjobs.md).

## Project-Scoped Methods

These methods operate on the project set via `SetContext`.

### GetCompiledTypes

```go
func (c *Client) GetCompiledTypes(ctx context.Context) (*CompiledTypesResponse, error)
```

Returns the merged set of object and relationship type definitions for the current project (across all installed packs).

`GET /api/template-packs/projects/:projectId/compiled-types`

---

### GetAvailablePacks

```go
func (c *Client) GetAvailablePacks(ctx context.Context) ([]TemplatePackListItem, error)
```

Lists template packs available to install on the current project.

`GET /api/template-packs/projects/:projectId/available`

---

### GetInstalledPacks

```go
func (c *Client) GetInstalledPacks(ctx context.Context) ([]InstalledPackItem, error)
```

Lists template packs already installed on the current project.

`GET /api/template-packs/projects/:projectId/installed`

---

### AssignPack

```go
func (c *Client) AssignPack(ctx context.Context, req *AssignPackRequest) (*ProjectTemplatePack, error)
```

Installs a template pack on the current project.

`POST /api/template-packs/projects/:projectId/assign`

---

### UpdateAssignment

```go
func (c *Client) UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAssignmentRequest) (*UpdateAssignmentResponse, error)
```

Updates an existing assignment (e.g. toggle `Active`).

`PATCH /api/template-packs/projects/:projectId/assignments/:assignmentId`

---

### DeleteAssignment

```go
func (c *Client) DeleteAssignment(ctx context.Context, assignmentID string) error
```

Removes a template pack assignment from the current project.

`DELETE /api/template-packs/projects/:projectId/assignments/:assignmentId`

---

## Global Pack CRUD Methods

### CreatePack

```go
func (c *Client) CreatePack(ctx context.Context, req *CreatePackRequest) (*TemplatePack, error)
```

Creates a new template pack in the registry.

`POST /api/template-packs`

---

### GetPack

```go
func (c *Client) GetPack(ctx context.Context, packID string) (*TemplatePack, error)
```

Retrieves a template pack by ID.

`GET /api/template-packs/:packId`

---

### DeletePack

```go
func (c *Client) DeletePack(ctx context.Context, packID string) error
```

Deletes a template pack. Fails if the pack is assigned to any projects.

`DELETE /api/template-packs/:packId`

---

## Types

### TemplatePack

```go
type TemplatePack struct {
    ID                      string          `json:"id"`
    Name                    string          `json:"name"`
    Version                 string          `json:"version"`
    Description             *string         `json:"description,omitempty"`
    Author                  *string         `json:"author,omitempty"`
    Source                  *string         `json:"source,omitempty"`
    License                 *string         `json:"license,omitempty"`
    RepositoryURL           *string         `json:"repositoryUrl,omitempty"`
    DocumentationURL        *string         `json:"documentationUrl,omitempty"`
    ObjectTypeSchemas       json.RawMessage `json:"objectTypeSchemas,omitempty"`
    RelationshipTypeSchemas json.RawMessage `json:"relationshipTypeSchemas,omitempty"`
    UIConfigs               json.RawMessage `json:"uiConfigs,omitempty"`
    ExtractionPrompts       json.RawMessage `json:"extractionPrompts,omitempty"`
    Checksum                *string         `json:"checksum,omitempty"`
    Draft                   bool            `json:"draft"`
    PublishedAt             *time.Time      `json:"publishedAt,omitempty"`
    DeprecatedAt            *time.Time      `json:"deprecatedAt,omitempty"`
    CreatedAt               time.Time       `json:"createdAt"`
    UpdatedAt               time.Time       `json:"updatedAt"`
}
```

### CompiledTypesResponse

```go
type CompiledTypesResponse struct {
    ObjectTypes       []ObjectTypeSchema       `json:"objectTypes"`
    RelationshipTypes []RelationshipTypeSchema `json:"relationshipTypes"`
}

type ObjectTypeSchema struct {
    Name        string          `json:"name"`
    Label       string          `json:"label,omitempty"`
    Description string          `json:"description,omitempty"`
    Properties  json.RawMessage `json:"properties,omitempty"`
    PackID      string          `json:"packId,omitempty"`
    PackName    string          `json:"packName,omitempty"`
}

type RelationshipTypeSchema struct {
    Name        string `json:"name"`
    Label       string `json:"label,omitempty"`
    Description string `json:"description,omitempty"`
    SourceType  string `json:"sourceType,omitempty"`
    TargetType  string `json:"targetType,omitempty"`
    PackID      string `json:"packId,omitempty"`
    PackName    string `json:"packName,omitempty"`
}
```

### InstalledPackItem

```go
type InstalledPackItem struct {
    ID             string                 `json:"id"` // assignment ID
    TemplatePackID string                 `json:"templatePackId"`
    Name           string                 `json:"name"`
    Version        string                 `json:"version"`
    Description    *string                `json:"description,omitempty"`
    Active         bool                   `json:"active"`
    InstalledAt    time.Time              `json:"installedAt"`
    Customizations map[string]interface{} `json:"customizations,omitempty"`
}
```

### AssignPackRequest

```go
type AssignPackRequest struct {
    TemplatePackID string                 `json:"template_pack_id"`
    Customizations map[string]interface{} `json:"customizations,omitempty"`
}
```

### CreatePackRequest

```go
type CreatePackRequest struct {
    Name                    string          `json:"name"`
    Version                 string          `json:"version"`
    Description             *string         `json:"description,omitempty"`
    Author                  *string         `json:"author,omitempty"`
    License                 *string         `json:"license,omitempty"`
    RepositoryURL           *string         `json:"repository_url,omitempty"`
    DocumentationURL        *string         `json:"documentation_url,omitempty"`
    ObjectTypeSchemas       json.RawMessage `json:"object_type_schemas"`
    RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty"`
    UIConfigs               json.RawMessage `json:"ui_configs,omitempty"`
    ExtractionPrompts       json.RawMessage `json:"extraction_prompts,omitempty"`
}
```

## Example

```go
// Get types available for the current project
types, err := client.TemplatePacks.GetCompiledTypes(ctx)
if err != nil {
    log.Fatal(err)
}
for _, t := range types.ObjectTypes {
    fmt.Printf("Type: %s (from pack %s)\n", t.Name, t.PackName)
}

// Install a pack on the current project
available, _ := client.TemplatePacks.GetAvailablePacks(ctx)
assignment, err := client.TemplatePacks.AssignPack(ctx, &templatepacks.AssignPackRequest{
    TemplatePackID: available[0].ID,
})
fmt.Println("Installed:", assignment.TemplatePackID, "assignment:", assignment.ID)
```
