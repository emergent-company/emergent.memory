package templatepacks

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// GraphTemplatePack represents a template pack in kb.graph_template_packs
type GraphTemplatePack struct {
	bun.BaseModel `bun:"table:kb.graph_template_packs,alias:gtp"`

	ID                      string          `bun:"id,pk,type:uuid" json:"id"`
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

// ProjectTemplatePack represents a project's installed template pack
type ProjectTemplatePack struct {
	bun.BaseModel `bun:"table:kb.project_template_packs,alias:ptp"`

	ID             string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID      string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	TemplatePackID string    `bun:"template_pack_id,notnull,type:uuid" json:"templatePackId"`
	Active         bool      `bun:"active,notnull,default:true" json:"active"`
	InstalledAt    time.Time `bun:"installed_at" json:"installedAt"`
	CreatedAt      time.Time `bun:"created_at" json:"createdAt"`
	UpdatedAt      time.Time `bun:"updated_at" json:"updatedAt"`

	// Joined template pack
	TemplatePack *GraphTemplatePack `bun:"rel:belongs-to,join:template_pack_id=id" json:"templatePack,omitempty"`
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

// TemplatePackListItem is a simplified pack for listing
type TemplatePackListItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description *string `json:"description,omitempty"`
	Author      *string `json:"author,omitempty"`
}

// InstalledPackItem represents an installed pack for a project
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

// AssignPackRequest is the request to assign a template pack to a project
type AssignPackRequest struct {
	TemplatePackID string                 `json:"template_pack_id"`
	Customizations map[string]interface{} `json:"customizations"`
}

// UpdateAssignmentRequest is the request to update a pack assignment
type UpdateAssignmentRequest struct {
	Active *bool `json:"active"`
}

// CreatePackRequest is the request to create a new template pack
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

// GetPackRequest is the request to get a template pack by ID
type GetPackRequest struct {
	ID string `json:"id"`
}

// DeletePackRequest is the request to delete a template pack
type DeletePackRequest struct {
	ID string `json:"id"`
}
