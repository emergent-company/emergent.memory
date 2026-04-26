package schemaregistry

import (
	"encoding/json"
	"time"
)

// SchemaRegistryEntryDTO is the API response for a schema registry entry
type SchemaRegistryEntryDTO struct {
	ID                    string                 `json:"id"`
	Type                  string                 `json:"type"`
	Source                string                 `json:"source"` // 'template', 'custom', 'discovered'
	SchemaID              *string                `json:"schema_id,omitempty"`
	SchemaName            *string                `json:"schema_name,omitempty"`
	SchemaVersion         int                    `json:"schema_version"`
	JSONSchema            json.RawMessage        `json:"json_schema"`
	UIConfig              map[string]interface{} `json:"ui_config"`
	ExtractionConfig      map[string]interface{} `json:"extraction_config"`
	Enabled               bool                   `json:"enabled"`
	DiscoveryConfidence   *float64               `json:"discovery_confidence,omitempty"`
	Description           *string                `json:"description,omitempty"`
	Namespace             *string                `json:"namespace,omitempty"`
	ObjectCount           int                    `json:"object_count,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	OutgoingRelationships []RelationshipTypeInfo `json:"outgoing_relationships,omitempty"`
	IncomingRelationships []RelationshipTypeInfo `json:"incoming_relationships,omitempty"`
}

// RelationshipTypeInfo describes a relationship type for a specific object type
type RelationshipTypeInfo struct {
	Type         string   `json:"type"`
	Label        *string  `json:"label,omitempty"`
	InverseLabel *string  `json:"inverse_label,omitempty"`
	InverseType  *string  `json:"inverse_type,omitempty"` // When set, creating this relationship auto-creates the inverse
	Description  *string  `json:"description,omitempty"`
	TargetTypes  []string `json:"target_types,omitempty"` // For outgoing: types this relationship can connect to
	SourceTypes  []string `json:"source_types,omitempty"` // For incoming: types this relationship can come from
}

// SchemaRegistryStats contains statistics for a project's schema registry
type SchemaRegistryStats struct {
	TotalTypes       int `json:"total_types"`
	EnabledTypes     int `json:"enabled_types"`
	TemplateTypes    int `json:"template_types"`
	CustomTypes      int `json:"custom_types"`
	DiscoveredTypes  int `json:"discovered_types"`
	TotalObjects     int `json:"total_objects"`
	TypesWithObjects int `json:"types_with_objects"`
}

// ListTypesQuery contains query parameters for listing types
type ListTypesQuery struct {
	EnabledOnly bool   `query:"enabled_only"`
	Source      string `query:"source"` // 'template', 'custom', 'discovered', 'all'
	Search      string `query:"search"`
	Namespace   string `query:"namespace"` // filter by namespace; "all" returns everything; default returns only namespace=NULL
}

// SchemaRegistryRowDTO represents a row from the complex SQL query with joins
type SchemaRegistryRowDTO struct {
	ID                  string          `bun:"id"`
	Type                string          `bun:"type"`
	Source              string          `bun:"source"`
	SchemaID            *string         `bun:"schema_id"`
	SchemaVersion       int             `bun:"schema_version"`
	JSONSchema          json.RawMessage `bun:"json_schema"`
	UIConfig            json.RawMessage `bun:"ui_config"`
	ExtractionConfig    json.RawMessage `bun:"extraction_config"`
	Enabled             bool            `bun:"enabled"`
	DiscoveryConfidence *float64        `bun:"discovery_confidence"`
	Description         *string         `bun:"description"`
	Namespace           *string         `bun:"namespace"`
	CreatedBy           *string         `bun:"created_by"`
	CreatedAt           time.Time       `bun:"created_at"`
	UpdatedAt           time.Time       `bun:"updated_at"`
	SchemaName          *string         `bun:"schema_name"`
	ObjectCount         int             `bun:"object_count"`
}

// ToDTO converts a row to the API response DTO
func (r *SchemaRegistryRowDTO) ToDTO() SchemaRegistryEntryDTO {
	var uiConfig map[string]interface{}
	var extractionConfig map[string]interface{}

	if r.UIConfig != nil {
		_ = json.Unmarshal(r.UIConfig, &uiConfig)
	}
	if uiConfig == nil {
		uiConfig = make(map[string]interface{})
	}

	if r.ExtractionConfig != nil {
		_ = json.Unmarshal(r.ExtractionConfig, &extractionConfig)
	}
	if extractionConfig == nil {
		extractionConfig = make(map[string]interface{})
	}

	return SchemaRegistryEntryDTO{
		ID:                  r.ID,
		Type:                r.Type,
		Source:              r.Source,
		SchemaID:            r.SchemaID,
		SchemaName:          r.SchemaName,
		SchemaVersion:       r.SchemaVersion,
		JSONSchema:          r.JSONSchema,
		UIConfig:            uiConfig,
		ExtractionConfig:    extractionConfig,
		Enabled:             r.Enabled,
		DiscoveryConfidence: r.DiscoveryConfidence,
		Description:         r.Description,
		Namespace:           r.Namespace,
		ObjectCount:         r.ObjectCount,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}

// RelationshipSchema represents a relationship type schema from memory schema JSON.
// Supports multiple field naming conventions for source/target types:
//   - sourceTypes / targetTypes (camelCase arrays)
//   - fromTypes / toTypes (alternative camelCase arrays)
//   - source_types / target_types (snake_case arrays)
//   - source / target (singular strings)
//
// RelationshipPropertyDef defines a single property in a relationship schema.
type RelationshipPropertyDef struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type RelationshipSchema struct {
	Label            string                             `json:"label,omitempty"`
	InverseLabel     string                             `json:"inverseLabel,omitempty"`
	InverseType      string                             `json:"inverseType,omitempty"` // When set, auto-creates inverse relationship (e.g. PARENT_OF -> CHILD_OF)
	Description      string                             `json:"description,omitempty"`
	FromTypes        []string                           `json:"fromTypes,omitempty"`
	SourceTypes      []string                           `json:"sourceTypes,omitempty"`
	ToTypes          []string                           `json:"toTypes,omitempty"`
	TargetTypes      []string                           `json:"targetTypes,omitempty"`
	SnakeSourceTypes []string                           `json:"source_types,omitempty"`
	SnakeTargetTypes []string                           `json:"target_types,omitempty"`
	Source           string                             `json:"source,omitempty"`
	Target           string                             `json:"target,omitempty"`
	Properties       map[string]RelationshipPropertyDef `json:"properties,omitempty"`
	Required         []string                           `json:"required,omitempty"`
}

// GetSourceTypes returns source types from any supported field name.
func (rs *RelationshipSchema) GetSourceTypes() []string {
	if len(rs.SourceTypes) > 0 {
		return rs.SourceTypes
	}
	if len(rs.FromTypes) > 0 {
		return rs.FromTypes
	}
	if len(rs.SnakeSourceTypes) > 0 {
		return rs.SnakeSourceTypes
	}
	if rs.Source != "" {
		return []string{rs.Source}
	}
	return nil
}

// GetTargetTypes returns target types from any supported field name.
func (rs *RelationshipSchema) GetTargetTypes() []string {
	if len(rs.TargetTypes) > 0 {
		return rs.TargetTypes
	}
	if len(rs.ToTypes) > 0 {
		return rs.ToTypes
	}
	if len(rs.SnakeTargetTypes) > 0 {
		return rs.SnakeTargetTypes
	}
	if rs.Target != "" {
		return []string{rs.Target}
	}
	return nil
}

// CreateTypeRequest is the request to register a custom object type for a project
type CreateTypeRequest struct {
	TypeName         string          `json:"type_name"`
	Description      *string         `json:"description,omitempty"`
	Namespace        *string         `json:"namespace,omitempty"`
	JSONSchema       json.RawMessage `json:"json_schema"`
	UIConfig         json.RawMessage `json:"ui_config,omitempty"`
	ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
	Enabled          *bool           `json:"enabled,omitempty"` // defaults to true
}

// UpdateTypeRequest is the request to update a registered type
type UpdateTypeRequest struct {
	Description      *string         `json:"description,omitempty"`
	Namespace        *string         `json:"namespace,omitempty"`
	JSONSchema       json.RawMessage `json:"json_schema,omitempty"`
	UIConfig         json.RawMessage `json:"ui_config,omitempty"`
	ExtractionConfig json.RawMessage `json:"extraction_config,omitempty"`
	Enabled          *bool           `json:"enabled,omitempty"`
}

// CreateRelationshipTypeRequest is the request body for creating or updating a relationship type.
type CreateRelationshipTypeRequest struct {
	Name         string `json:"name"`
	Label        string `json:"label,omitempty"`
	InverseLabel string `json:"inverse_label,omitempty"`
	Description  string `json:"description,omitempty"`
	SourceType   string `json:"source_type,omitempty"`
	TargetType   string `json:"target_type,omitempty"`
}
