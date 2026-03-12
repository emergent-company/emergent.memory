package schemas

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// GraphMemorySchema represents a memory schema in kb.graph_schemas
type GraphMemorySchema struct {
	bun.BaseModel `bun:"table:kb.graph_schemas,alias:gtp"`

	ID                      string          `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name                    string          `bun:"name,notnull" json:"name"`
	Version                 string          `bun:"version,notnull" json:"version"`
	Description             *string         `bun:"description" json:"description,omitempty"`
	Author                  *string         `bun:"author" json:"author,omitempty"`
	Source                  *string         `bun:"source" json:"source,omitempty"`
	License                 *string         `bun:"license" json:"license,omitempty"`
	RepositoryURL           *string         `bun:"repository_url" json:"repositoryUrl,omitempty"`
	DocumentationURL        *string         `bun:"documentation_url" json:"documentationUrl,omitempty"`
	ObjectTypeSchemas       json.RawMessage `bun:"object_type_schemas,type:jsonb" json:"objectTypeSchemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas,type:jsonb" json:"relationshipTypeSchemas,omitempty"`
	UIConfigs               json.RawMessage `bun:"ui_configs,type:jsonb" json:"uiConfigs,omitempty"`
	ExtractionPrompts       json.RawMessage `bun:"extraction_prompts,type:jsonb" json:"extractionPrompts,omitempty"`
	Checksum                *string         `bun:"checksum" json:"checksum,omitempty"`
	Draft                   bool            `bun:"draft,notnull,default:false" json:"draft"`
	PublishedAt             *time.Time      `bun:"published_at" json:"publishedAt,omitempty"`
	DeprecatedAt            *time.Time      `bun:"deprecated_at" json:"deprecatedAt,omitempty"`
	CreatedAt               time.Time       `bun:"created_at" json:"createdAt"`
	UpdatedAt               time.Time       `bun:"updated_at" json:"updatedAt"`
}

// ProjectMemorySchema represents a project's installed memory schema
type ProjectMemorySchema struct {
	bun.BaseModel `bun:"table:kb.project_schemas,alias:ptp"`

	ID          string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID   string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SchemaID    string    `bun:"schema_id,notnull,type:uuid" json:"schemaId"`
	Active      bool      `bun:"active,notnull,default:true" json:"active"`
	InstalledAt time.Time `bun:"installed_at" json:"installedAt"`
	CreatedAt   time.Time `bun:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at" json:"updatedAt"`

	// Joined memory schema
	MemorySchema *GraphMemorySchema `bun:"rel:belongs-to,join:schema_id=id" json:"memorySchema,omitempty"`
}

// CompiledTypesResponse contains the compiled object and relationship types for a project
type CompiledTypesResponse struct {
	ObjectTypes       []ObjectTypeSchema       `json:"objectTypes"`
	RelationshipTypes []RelationshipTypeSchema `json:"relationshipTypes"`
}

// ObjectTypeSchema represents an object type definition
type ObjectTypeSchema struct {
	Name        string          `json:"name"`
	Label       string          `json:"label,omitempty"`
	Description string          `json:"description,omitempty"`
	Properties  json.RawMessage `json:"properties,omitempty"`
	PackID      string          `json:"packId,omitempty"`
	PackName    string          `json:"packName,omitempty"`
}

// RelationshipTypeSchema represents a relationship type definition
type RelationshipTypeSchema struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	SourceType  string `json:"sourceType,omitempty"`
	TargetType  string `json:"targetType,omitempty"`
	PackID      string `json:"packId,omitempty"`
	PackName    string `json:"packName,omitempty"`
}

// MemorySchemaListItem is a simplified schema for listing
type MemorySchemaListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
}

// InstalledSchemaItem represents an installed schema for a project
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

// AssignPackRequest is the request to assign a memory schema to a project
type AssignPackRequest struct {
	SchemaID       string                 `json:"schema_id"`
	Customizations map[string]interface{} `json:"customizations"`
	// DryRun, when true, computes and returns the full conflict/merge preview
	// without making any database changes. The response uses HTTP 200.
	DryRun bool `json:"dry_run,omitempty"`
	// Merge, when true, additively merges incoming type schemas into existing
	// registered types rather than skipping conflicting type names.
	Merge bool `json:"merge,omitempty"`
}

// PropertyConflict describes a single property-level conflict during a merge.
type PropertyConflict struct {
	Property    string          `json:"property"`
	ExistingDef json.RawMessage `json:"existing_def"`
	IncomingDef json.RawMessage `json:"incoming_def"`
	// Resolution is always "existing_wins" — existing properties are never overwritten.
	Resolution string `json:"resolution"`
}

// SchemaConflict describes a type-level conflict when assigning a pack whose
// type names overlap with types already registered in the project.
type SchemaConflict struct {
	TypeName              string             `json:"type_name"`
	ExistingSchema        json.RawMessage    `json:"existing_schema"`
	IncomingSchema        json.RawMessage    `json:"incoming_schema"`
	MergedSchema          json.RawMessage    `json:"merged_schema,omitempty"`
	AddedProperties       []string           `json:"added_properties,omitempty"`
	ConflictingProperties []PropertyConflict `json:"conflicting_properties,omitempty"`
}

// AssignPackResult is the response from AssignPack / dry-run.
// Replaces the bare *ProjectMemorySchema return so callers get conflict details.
type AssignPackResult struct {
	// DryRun mirrors the request flag so callers can distinguish the response mode.
	DryRun bool `json:"dry_run"`
	// AssignmentID is empty for dry-run responses.
	AssignmentID string `json:"assignment_id,omitempty"`
	PackID       string `json:"pack_id"`
	PackName     string `json:"pack_name"`
	// InstalledTypes are type names newly written to the registry.
	InstalledTypes []string `json:"installed_types"`
	// SkippedTypes are type names that already existed and were not merged.
	SkippedTypes []string `json:"skipped_types,omitempty"`
	// MergedTypes are type names whose schemas were additively extended.
	MergedTypes []string `json:"merged_types,omitempty"`
	// Conflicts contains full diff detail for each conflicting type.
	Conflicts []SchemaConflict `json:"conflicts,omitempty"`
	// AlreadyInstalled is true when merge=true and the pack was already assigned.
	AlreadyInstalled bool `json:"already_installed,omitempty"`
}

// UpdateAssignmentRequest is the request to update a pack assignment
type UpdateAssignmentRequest struct {
	Active *bool `json:"active"`
}

// CreatePackRequest is the request to create a new schema
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

// UpdatePackRequest is the request to update an existing schema (partial update — only non-nil fields are applied)
type UpdatePackRequest struct {
	Name                    *string         `json:"name,omitempty"`
	Version                 *string         `json:"version,omitempty"`
	Description             *string         `json:"description,omitempty"`
	Author                  *string         `json:"author,omitempty"`
	License                 *string         `json:"license,omitempty"`
	RepositoryURL           *string         `json:"repository_url,omitempty"`
	DocumentationURL        *string         `json:"documentation_url,omitempty"`
	ObjectTypeSchemas       json.RawMessage `json:"object_type_schemas,omitempty"`
	RelationshipTypeSchemas json.RawMessage `json:"relationship_type_schemas,omitempty"`
	UIConfigs               json.RawMessage `json:"ui_configs,omitempty"`
	ExtractionPrompts       json.RawMessage `json:"extraction_prompts,omitempty"`
}

// GetPackRequest is the request to get a schema by ID
type GetPackRequest struct {
	ID string `json:"id"`
}

// DeletePackRequest is the request to delete a schema
type DeletePackRequest struct {
	ID string `json:"id"`
}
