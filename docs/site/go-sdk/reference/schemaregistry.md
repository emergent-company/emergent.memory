# Schema Registry

**Package:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry`

**Client field:** `client.SchemaRegistry`

The schema registry holds the complete set of object type definitions for a project. Types can originate from schemas (`source: "schema"`), be created manually (`source: "custom"`), or be produced by Discovery Jobs (`source: "discovered"`). The registry drives extraction prompts, UI rendering, and graph validation.

## Methods

### GetProjectTypes

```go
func (c *Client) GetProjectTypes(ctx context.Context, projectID string, opts *ListTypesOptions) ([]SchemaRegistryEntry, error)
```

Returns all object types registered for a project, with optional filters.

`GET /api/schema-registry/projects/:projectId`

---

### GetObjectType

```go
func (c *Client) GetObjectType(ctx context.Context, projectID, typeName string) (*SchemaRegistryEntry, error)
```

Returns a specific object type definition including its incoming/outgoing relationship types.

`GET /api/schema-registry/projects/:projectId/types/:typeName`

---

### GetTypeStats

```go
func (c *Client) GetTypeStats(ctx context.Context, projectID string) (*SchemaRegistryStats, error)
```

Returns counts of types by source and enabled status.

`GET /api/schema-registry/projects/:projectId/stats`

---

### CreateType

```go
func (c *Client) CreateType(ctx context.Context, projectID string, req *CreateTypeRequest) (*SchemaRegistryEntry, error)
```

Registers a new custom object type.

`POST /api/schema-registry/projects/:projectId/types`

---

### UpdateType

```go
func (c *Client) UpdateType(ctx context.Context, projectID, typeName string, req *UpdateTypeRequest) (*SchemaRegistryEntry, error)
```

Updates an existing type definition.

`PUT /api/schema-registry/projects/:projectId/types/:typeName`

---

### DeleteType

```go
func (c *Client) DeleteType(ctx context.Context, projectID, typeName string) error
```

Removes a type from the project registry. Note: only `source: "custom"` or `source: "discovered"` types can be deleted; schema types must be removed by uninstalling the schema.

`DELETE /api/schema-registry/projects/:projectId/types/:typeName`

---

## Types

### SchemaRegistryEntry

```go
type SchemaRegistryEntry struct {
    ID                    string                 `json:"id"`
    Type                  string                 `json:"type"`
    Source                string                 `json:"source"` // "schema" | "custom" | "discovered"
    SchemaID              *string                `json:"schema_id,omitempty"`
    SchemaName            *string                `json:"schema_name,omitempty"`
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

### SchemaRegistryStats

```go
type SchemaRegistryStats struct {
    TotalTypes       int `json:"total_types"`
    EnabledTypes     int `json:"enabled_types"`
    SchemaTypes      int `json:"schema_types"`
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
    Source      string // "schema" | "custom" | "discovered" | "all"
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
types, err := client.SchemaRegistry.GetProjectTypes(ctx, "proj-abc", &schemaregistry.ListTypesOptions{
    EnabledOnly: &all,
})
for _, t := range types {
    fmt.Printf("%s (%s) — %d objects\n", t.Type, t.Source, t.ObjectCount)
}

// Register a custom type
entry, err := client.SchemaRegistry.CreateType(ctx, "proj-abc", &schemaregistry.CreateTypeRequest{
    TypeName:    "ResearchPaper",
    Description: ptrString("An academic research paper"),
    JSONSchema:  json.RawMessage(`{"title":{"type":"string"},"abstract":{"type":"string"}}`),
})
fmt.Println("Registered:", entry.Type)
```
