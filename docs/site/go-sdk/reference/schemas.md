# Schemas

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas`

**Client field:** `client.Schemas`

Schemas are versioned bundles of object type schemas, relationship type schemas, UI configs, and extraction prompts. They can be installed on projects to bootstrap a consistent graph schema, and are also produced by [Discovery Jobs](discoveryjobs.md).

## Project-Scoped Methods

These methods operate on the project set via `SetContext`.

### GetCompiledTypes

```go
func (c *Client) GetCompiledTypes(ctx context.Context) (*CompiledTypesResponse, error)
```

Returns the merged set of object and relationship type definitions for the current project (across all installed schemas).

`GET /api/schemas/projects/:projectId/compiled-types`

---

### GetAvailableSchemas

```go
func (c *Client) GetAvailableSchemas(ctx context.Context) ([]SchemaListItem, error)
```

Lists schemas available to install on the current project.

`GET /api/schemas/projects/:projectId/available`

---

### GetInstalledSchemas

```go
func (c *Client) GetInstalledSchemas(ctx context.Context) ([]InstalledSchemaItem, error)
```

Lists schemas already installed on the current project.

`GET /api/schemas/projects/:projectId/installed`

---

### AssignSchema

```go
func (c *Client) AssignSchema(ctx context.Context, req *AssignSchemaRequest) (*ProjectSchema, error)
```

Installs a schema on the current project.

`POST /api/schemas/projects/:projectId/assign`

---

### UpdateAssignment

```go
func (c *Client) UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAssignmentRequest) (*UpdateAssignmentResponse, error)
```

Updates an existing assignment (e.g. toggle `Active`).

`PATCH /api/schemas/projects/:projectId/assignments/:assignmentId`

---

### DeleteAssignment

```go
func (c *Client) DeleteAssignment(ctx context.Context, assignmentID string) error
```

Removes a schema assignment from the current project.

`DELETE /api/schemas/projects/:projectId/assignments/:assignmentId`

---

## Global Schema CRUD Methods

### CreateSchema

```go
func (c *Client) CreateSchema(ctx context.Context, req *CreateSchemaRequest) (*MemorySchema, error)
```

Creates a new schema in the registry.

`POST /api/schemas`

---

### GetSchema

```go
func (c *Client) GetSchema(ctx context.Context, schemaID string) (*MemorySchema, error)
```

Retrieves a schema by ID.

`GET /api/schemas/:schemaId`

---

### DeleteSchema

```go
func (c *Client) DeleteSchema(ctx context.Context, schemaID string) error
```

Deletes a schema. Fails if the schema is assigned to any projects.

`DELETE /api/schemas/:schemaId`

---

## Types

### MemorySchema

```go
type MemorySchema struct {
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
    SchemaID    string          `json:"schemaId,omitempty"`
    SchemaName  string          `json:"schemaName,omitempty"`
}

type RelationshipTypeSchema struct {
    Name        string `json:"name"`
    Label       string `json:"label,omitempty"`
    Description string `json:"description,omitempty"`
    SourceType  string `json:"sourceType,omitempty"`
    TargetType  string `json:"targetType,omitempty"`
    SchemaID    string `json:"schemaId,omitempty"`
    SchemaName  string `json:"schemaName,omitempty"`
}
```

### InstalledSchemaItem

```go
type InstalledSchemaItem struct {
    ID             string                 `json:"id"` // assignment ID
    SchemaID       string                 `json:"schemaId"`
    Name           string                 `json:"name"`
    Version        string                 `json:"version"`
    Description    *string                `json:"description,omitempty"`
    Active         bool                   `json:"active"`
    InstalledAt    time.Time              `json:"installedAt"`
    Customizations map[string]interface{} `json:"customizations,omitempty"`
}
```

### AssignSchemaRequest

```go
type AssignSchemaRequest struct {
    SchemaID       string                 `json:"schema_id"`
    Customizations map[string]interface{} `json:"customizations,omitempty"`
}
```

### CreateSchemaRequest

```go
type CreateSchemaRequest struct {
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
types, err := client.Schemas.GetCompiledTypes(ctx)
if err != nil {
    log.Fatal(err)
}
for _, t := range types.ObjectTypes {
    fmt.Printf("Type: %s (from schema %s)\n", t.Name, t.SchemaName)
}

// Install a schema on the current project
available, err := client.Schemas.GetAvailableSchemas(ctx)
if err != nil || len(available) == 0 {
    log.Fatal("no schemas available")
}
assignment, err := client.Schemas.AssignSchema(ctx, &schemas.AssignSchemaRequest{
    SchemaID: available[0].ID,
})
fmt.Println("Installed:", assignment.SchemaID, "assignment:", assignment.ID)
```
