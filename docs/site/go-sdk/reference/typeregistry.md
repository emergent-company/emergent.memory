# Type Registry

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/typeregistry`

**Client field:** `client.TypeRegistry`

The type registry holds the complete set of object type definitions for a project. Types can originate from template packs (`source: "template"`), be created manually (`source: "custom"`), or be produced by Discovery Jobs (`source: "discovered"`). The registry drives extraction prompts, UI rendering, and graph validation.

## Methods

### GetProjectTypes

```go
func (c *Client) GetProjectTypes(ctx context.Context, projectID string, opts *ListTypesOptions) ([]TypeRegistryEntry, error)
```

Returns all object types registered for a project, with optional filters.

`GET /api/type-registry/projects/:projectId`

---

### GetObjectType

```go
func (c *Client) GetObjectType(ctx context.Context, projectID, typeName string) (*TypeRegistryEntry, error)
```

Returns a specific object type definition including its incoming/outgoing relationship types.

`GET /api/type-registry/projects/:projectId/types/:typeName`

---

### GetTypeStats

```go
func (c *Client) GetTypeStats(ctx context.Context, projectID string) (*TypeRegistryStats, error)
```

Returns counts of types by source and enabled status.

`GET /api/type-registry/projects/:projectId/stats`

---

### CreateType

```go
func (c *Client) CreateType(ctx context.Context, projectID string, req *CreateTypeRequest) (*TypeRegistryEntry, error)
```

Registers a new custom object type.

`POST /api/type-registry/projects/:projectId/types`

---

### UpdateType

```go
func (c *Client) UpdateType(ctx context.Context, projectID, typeName string, req *UpdateTypeRequest) (*TypeRegistryEntry, error)
```

Updates an existing type definition.

`PUT /api/type-registry/projects/:projectId/types/:typeName`

---

### DeleteType

```go
func (c *Client) DeleteType(ctx context.Context, projectID, typeName string) error
```

Removes a type from the project registry. Note: only `source: "custom"` or `source: "discovered"` types can be deleted; template pack types must be removed by uninstalling the pack.

`DELETE /api/type-registry/projects/:projectId/types/:typeName`

---

## Types

### TypeRegistryEntry

```go
type TypeRegistryEntry struct {
    ID                    string                 `json:"id"`
    Type                  string                 `json:"type"`
    Source                string                 `json:"source"` // "template" | "custom" | "discovered"
    TemplatePackID        *string                `json:"template_pack_id,omitempty"`
    TemplatePackName      *string                `json:"template_pack_name,omitempty"`
    SchemaVersion         int                    `json:"schema_version"`
    JSONSchema            json.RawMessage        `json:"json_schema"`
    UIConfig              map[string]interface{} `json:"ui_config"`
    ExtractionConfig      map[string]interface{} `json:"extraction_config"`
    Enabled               bool                   `json:"enabled"`
    DiscoveryConfidence   *float64               `json:"discovery_confidence,omitempty"`
    Description           *string                `json:"description,omitempty"`
    ObjectCount           int                    `json:"object_count,omitempty"`
    CreatedAt             time.Time              `json:"created_at"`
    UpdatedAt             time.Time              `json:"updated_at"`
    OutgoingRelationships []RelationshipTypeInfo `json:"outgoing_relationships,omitempty"`
    IncomingRelationships []RelationshipTypeInfo `json:"incoming_relationships,omitempty"`
}
```

### RelationshipTypeInfo

```go
type RelationshipTypeInfo struct {
    Type         string   `json:"type"`
    Label        *string  `json:"label,omitempty"`
    InverseLabel *string  `json:"inverse_label,omitempty"`
    Description  *string  `json:"description,omitempty"`
    TargetTypes  []string `json:"target_types,omitempty"`
    SourceTypes  []string `json:"source_types,omitempty"`
}
```

### TypeRegistryStats

```go
type TypeRegistryStats struct {
    TotalTypes       int `json:"total_types"`
    EnabledTypes     int `json:"enabled_types"`
    TemplateTypes    int `json:"template_types"`
    CustomTypes      int `json:"custom_types"`
    DiscoveredTypes  int `json:"discovered_types"`
    TotalObjects     int `json:"total_objects"`
    TypesWithObjects int `json:"types_with_objects"`
}
```

### ListTypesOptions

```go
type ListTypesOptions struct {
    EnabledOnly *bool  // Filter enabled types only (server default: true)
    Source      string // "template" | "custom" | "discovered" | "all"
    Search      string // Search in type names
}
```

### CreateTypeRequest / UpdateTypeRequest

```go
type CreateTypeRequest struct {
    TypeName         string          `json:"type_name"`
    Description      *string         `json:"description,omitempty"`
    JSONSchema       json.RawMessage `json:"json_schema"`
    UIConfig         json.RawMessage `json:"ui_config,omitempty"`
    ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
    Enabled          *bool           `json:"enabled,omitempty"` // defaults true
}

type UpdateTypeRequest struct {
    Description      *string         `json:"description,omitempty"`
    JSONSchema       json.RawMessage `json:"json_schema,omitempty"`
    UIConfig         json.RawMessage `json:"ui_config,omitempty"`
    ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
    Enabled          *bool           `json:"enabled,omitempty"`
}
```

## Example

```go
// List all enabled types for a project
all := true
types, err := client.TypeRegistry.GetProjectTypes(ctx, "proj-abc", &typeregistry.ListTypesOptions{
    EnabledOnly: &all,
})
for _, t := range types {
    fmt.Printf("%s (%s) — %d objects\n", t.Type, t.Source, t.ObjectCount)
}

// Register a custom type
entry, err := client.TypeRegistry.CreateType(ctx, "proj-abc", &typeregistry.CreateTypeRequest{
    TypeName:    "ResearchPaper",
    Description: ptrString("An academic research paper"),
    JSONSchema:  json.RawMessage(`{"title":{"type":"string"},"abstract":{"type":"string"}}`),
})
fmt.Println("Registered:", entry.Type)
```
