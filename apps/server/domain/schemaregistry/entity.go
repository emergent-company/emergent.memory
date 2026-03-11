package schemaregistry

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// ProjectObjectSchemaRegistry represents the kb.project_object_schema_registry table
type ProjectObjectSchemaRegistry struct {
	bun.BaseModel `bun:"table:kb.project_object_schema_registry,alias:ptr"`

	ID                  string          `bun:"id,pk,type:uuid" json:"id"`
	ProjectID           string          `bun:"project_id,notnull,type:uuid" json:"projectId"`
	TypeName            string          `bun:"type_name,notnull" json:"typeName"`
	Source              string          `bun:"source,notnull" json:"source"` // 'template', 'custom', 'discovered'
	SchemaID            *string         `bun:"schema_id,type:uuid" json:"schemaId,omitempty"`
	SchemaVersion       int             `bun:"schema_version,notnull,default:1" json:"schemaVersion"`
	JSONSchema          json.RawMessage `bun:"json_schema,type:jsonb,notnull" json:"jsonSchema"`
	UIConfig            json.RawMessage `bun:"ui_config,type:jsonb" json:"uiConfig,omitempty"`
	ExtractionConfig    json.RawMessage `bun:"extraction_config,type:jsonb" json:"extractionConfig,omitempty"`
	Enabled             bool            `bun:"enabled,notnull,default:true" json:"enabled"`
	DiscoveryConfidence *float64        `bun:"discovery_confidence" json:"discoveryConfidence,omitempty"`
	Description         *string         `bun:"description" json:"description,omitempty"`
	CreatedBy           *string         `bun:"created_by,type:uuid" json:"createdBy,omitempty"`
	CreatedAt           time.Time       `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt           time.Time       `bun:"updated_at,notnull,default:now()" json:"updatedAt"`

	// Relations - for joining
	MemorySchema *GraphMemorySchema `bun:"rel:belongs-to,join:schema_id=id" json:"memorySchema,omitempty"`
}

// GraphMemorySchema represents the kb.graph_schemas table (for joins)
type GraphMemorySchema struct {
	bun.BaseModel `bun:"table:kb.graph_schemas,alias:tp"`

	ID                      string          `bun:"id,pk,type:uuid" json:"id"`
	Name                    string          `bun:"name,notnull" json:"name"`
	Version                 string          `bun:"version,notnull" json:"version"`
	Description             *string         `bun:"description" json:"description,omitempty"`
	RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas,type:jsonb" json:"relationshipTypeSchemas,omitempty"`
	CreatedAt               time.Time       `bun:"created_at" json:"createdAt"`
	UpdatedAt               time.Time       `bun:"updated_at" json:"updatedAt"`
}

// ProjectMemorySchema represents the kb.project_schemas table (for relationship lookups)
type ProjectMemorySchema struct {
	bun.BaseModel `bun:"table:kb.project_schemas,alias:ptp"`

	ID          string    `bun:"id,pk,type:uuid" json:"id"`
	ProjectID   string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SchemaID    string    `bun:"schema_id,notnull,type:uuid" json:"schemaId"`
	Active      bool      `bun:"active,notnull,default:true" json:"active"`
	InstalledAt time.Time `bun:"installed_at" json:"installedAt"`
	CreatedAt   time.Time `bun:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at" json:"updatedAt"`

	// Joined memory schema
	MemorySchema *GraphMemorySchema `bun:"rel:belongs-to,join:schema_id=id" json:"memorySchema,omitempty"`
}

// GraphObject for object counting (kb.graph_objects)
type GraphObject struct {
	bun.BaseModel `bun:"table:kb.graph_objects,alias:go"`

	ID        string     `bun:"id,pk,type:uuid" json:"id"`
	ProjectID string     `bun:"project_id,type:uuid" json:"projectId"`
	Type      string     `bun:"type" json:"type"`
	DeletedAt *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`
}

// Project for validation (kb.projects)
type Project struct {
	bun.BaseModel `bun:"table:kb.projects,alias:p"`

	ID             string `bun:"id,pk,type:uuid" json:"id"`
	OrganizationID string `bun:"organization_id,type:uuid" json:"organizationId"`
}
