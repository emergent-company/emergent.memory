// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent/domain/extraction/agents"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/uptrace/bun"
)

// GraphTemplatePack represents a template pack from kb.graph_template_packs.
// Template packs are GLOBAL resources shared across all organizations.
type GraphTemplatePack struct {
	bun.BaseModel `bun:"kb.graph_template_packs,alias:gtp"`

	ID                      string     `bun:"id,pk,type:uuid"`
	Name                    string     `bun:"name,notnull"`
	Version                 string     `bun:"version,notnull"`
	ParentVersionID         *string    `bun:"parent_version_id,type:uuid"`
	Draft                   bool       `bun:"draft,default:false"`
	Description             *string    `bun:"description"`
	Author                  *string    `bun:"author"`
	License                 *string    `bun:"license"`
	RepositoryURL           *string    `bun:"repository_url"`
	DocumentationURL        *string    `bun:"documentation_url"`
	Source                  *string    `bun:"source"` // manual, discovered, imported, system
	DiscoveryJobID          *string    `bun:"discovery_job_id,type:uuid"`
	PendingReview           bool       `bun:"pending_review,default:false"`
	ObjectTypeSchemas       JSON       `bun:"object_type_schemas,type:jsonb,notnull"`
	RelationshipTypeSchemas JSON       `bun:"relationship_type_schemas,type:jsonb,default:'{}'"`
	UIConfigs               JSON       `bun:"ui_configs,type:jsonb,default:'{}'"`
	ExtractionPrompts       JSON       `bun:"extraction_prompts,type:jsonb,default:'{}'"`
	SQLViews                JSON       `bun:"sql_views,type:jsonb,default:'[]'"`
	Signature               *string    `bun:"signature"`
	Checksum                *string    `bun:"checksum"`
	PublishedAt             time.Time  `bun:"published_at,default:now()"`
	DeprecatedAt            *time.Time `bun:"deprecated_at"`
	SupersededBy            *string    `bun:"superseded_by"`
	CreatedAt               time.Time  `bun:"created_at,default:now()"`
	UpdatedAt               time.Time  `bun:"updated_at,default:now()"`
}

// ProjectTemplatePack represents a template pack installation for a project.
// Maps kb.project_template_packs table.
type ProjectTemplatePack struct {
	bun.BaseModel `bun:"kb.project_template_packs,alias:ptp"`

	ID             string                      `bun:"id,pk,type:uuid"`
	ProjectID      string                      `bun:"project_id,notnull,type:uuid"`
	TemplatePackID string                      `bun:"template_pack_id,notnull,type:uuid"`
	InstalledAt    time.Time                   `bun:"installed_at,default:now()"`
	InstalledBy    *string                     `bun:"installed_by,type:uuid"`
	Active         bool                        `bun:"active,default:true"`
	Customizations *TemplatePackCustomizations `bun:"customizations,type:jsonb,default:'{}'"`
	CreatedAt      time.Time                   `bun:"created_at,default:now()"`
	UpdatedAt      time.Time                   `bun:"updated_at,default:now()"`

	// Joined fields
	TemplatePack *GraphTemplatePack `bun:"rel:belongs-to,join:template_pack_id=id"`
}

// TemplatePackCustomizations holds installation-specific customizations.
type TemplatePackCustomizations struct {
	EnabledTypes    []string       `json:"enabledTypes,omitempty"`
	DisabledTypes   []string       `json:"disabledTypes,omitempty"`
	SchemaOverrides map[string]any `json:"schemaOverrides,omitempty"`
}

// Scan implements sql.Scanner for TemplatePackCustomizations.
func (c *TemplatePackCustomizations) Scan(value any) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	default:
		return fmt.Errorf("unsupported type for TemplatePackCustomizations: %T", value)
	}
}

// Value implements driver.Valuer for TemplatePackCustomizations.
func (c TemplatePackCustomizations) Value() (any, error) {
	return json.Marshal(c)
}

// TemplatePackSchemaProvider implements SchemaProvider by loading schemas
// from template packs assigned to a project.
type TemplatePackSchemaProvider struct {
	db  bun.IDB
	log *slog.Logger
}

// NewTemplatePackSchemaProvider creates a new template pack schema provider.
func NewTemplatePackSchemaProvider(db bun.IDB, log *slog.Logger) *TemplatePackSchemaProvider {
	return &TemplatePackSchemaProvider{
		db:  db,
		log: log.With(logger.Scope("template-pack-schema-provider")),
	}
}

// GetProjectSchemas loads and merges schemas from all active template packs for a project.
// Later packs override earlier ones for the same type.
func (p *TemplatePackSchemaProvider) GetProjectSchemas(
	ctx context.Context,
	projectID string,
) (*ExtractionSchemas, error) {
	// Get all active template pack assignments for this project
	var assignments []ProjectTemplatePack
	err := p.db.NewSelect().
		Model(&assignments).
		Relation("TemplatePack").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return &ExtractionSchemas{
				ObjectSchemas:       make(map[string]agents.ObjectSchema),
				RelationshipSchemas: make(map[string]agents.RelationshipSchema),
			}, nil
		}
		return nil, fmt.Errorf("query project template packs: %w", err)
	}

	if len(assignments) == 0 {
		return &ExtractionSchemas{
			ObjectSchemas:       make(map[string]agents.ObjectSchema),
			RelationshipSchemas: make(map[string]agents.RelationshipSchema),
		}, nil
	}

	// Merge all schemas from all packs
	objectSchemas := make(map[string]agents.ObjectSchema)
	relationshipSchemas := make(map[string]agents.RelationshipSchema)

	for _, assignment := range assignments {
		if assignment.TemplatePack == nil {
			continue
		}

		pack := assignment.TemplatePack

		// Apply customizations
		enabledTypes := make(map[string]bool)
		disabledTypes := make(map[string]bool)
		var schemaOverrides map[string]any

		if assignment.Customizations != nil {
			for _, t := range assignment.Customizations.EnabledTypes {
				enabledTypes[t] = true
			}
			for _, t := range assignment.Customizations.DisabledTypes {
				disabledTypes[t] = true
			}
			schemaOverrides = assignment.Customizations.SchemaOverrides
		}

		// Merge object type schemas
		objSchemas := parseObjectTypeSchemas(pack.ObjectTypeSchemas)
		for typeName, schema := range objSchemas {
			// Skip disabled types
			if disabledTypes[typeName] {
				continue
			}
			// If enabledTypes is set, only include those types
			if len(enabledTypes) > 0 && !enabledTypes[typeName] {
				continue
			}

			// Apply schema overrides if present
			if overrides, ok := schemaOverrides[typeName]; ok {
				schema = applySchemaOverrides(schema, overrides)
			}

			objectSchemas[typeName] = schema
		}

		// Merge relationship type schemas
		relSchemas := parseRelationshipTypeSchemas(pack.RelationshipTypeSchemas)
		for typeName, schema := range relSchemas {
			// Skip disabled types
			if disabledTypes[typeName] {
				continue
			}
			// If enabledTypes is set, only include those types
			if len(enabledTypes) > 0 && !enabledTypes[typeName] {
				continue
			}

			relationshipSchemas[typeName] = schema
		}

		p.log.Debug("merged template pack schemas",
			slog.String("pack_name", pack.Name),
			slog.String("pack_version", pack.Version),
			slog.Int("object_types", len(objSchemas)),
			slog.Int("relationship_types", len(relSchemas)))
	}

	p.log.Info("loaded project schemas",
		slog.String("project_id", projectID),
		slog.Int("template_packs", len(assignments)),
		slog.Int("object_types", len(objectSchemas)),
		slog.Int("relationship_types", len(relationshipSchemas)))

	return &ExtractionSchemas{
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	}, nil
}

// parseObjectTypeSchemas converts JSON object_type_schemas to map of ObjectSchema.
func parseObjectTypeSchemas(raw JSON) map[string]agents.ObjectSchema {
	schemas := make(map[string]agents.ObjectSchema)
	if raw == nil {
		return schemas
	}

	for typeName, schemaRaw := range raw {
		schemaMap, ok := schemaRaw.(map[string]any)
		if !ok {
			continue
		}

		schema := agents.ObjectSchema{
			Name: typeName,
		}

		if desc, ok := schemaMap["description"].(string); ok {
			schema.Description = desc
		}

		if props, ok := schemaMap["properties"].(map[string]any); ok {
			schema.Properties = make(map[string]agents.PropertyDef)
			for propName, propRaw := range props {
				propMap, ok := propRaw.(map[string]any)
				if !ok {
					continue
				}
				propDef := agents.PropertyDef{}
				if t, ok := propMap["type"].(string); ok {
					propDef.Type = t
				}
				if d, ok := propMap["description"].(string); ok {
					propDef.Description = d
				}
				schema.Properties[propName] = propDef
			}
		}

		if req, ok := schemaMap["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					schema.Required = append(schema.Required, s)
				}
			}
		}

		// Check for extraction_guidelines in extraction_prompts
		if guidelines, ok := schemaMap["extraction_guidelines"].(string); ok {
			schema.ExtractionGuidelines = guidelines
		}

		schemas[typeName] = schema
	}

	return schemas
}

// parseRelationshipTypeSchemas converts JSON relationship_type_schemas to map of RelationshipSchema.
// Handles multiple field naming conventions for source/target types:
//   - source_types / target_types (snake_case)
//   - sourceTypes / targetTypes (camelCase)
//   - fromTypes / toTypes (alternative camelCase)
//   - source / target (singular string)
func parseRelationshipTypeSchemas(raw JSON) map[string]agents.RelationshipSchema {
	schemas := make(map[string]agents.RelationshipSchema)
	if raw == nil {
		return schemas
	}

	for typeName, schemaRaw := range raw {
		schemaMap, ok := schemaRaw.(map[string]any)
		if !ok {
			continue
		}

		schema := agents.RelationshipSchema{
			Name: typeName,
		}

		if desc, ok := schemaMap["description"].(string); ok {
			schema.Description = desc
		}

		// Parse source types from any supported field name
		schema.SourceTypes = parseTypesField(schemaMap, "source_types", "sourceTypes", "fromTypes", "source")

		// Parse target types from any supported field name
		schema.TargetTypes = parseTypesField(schemaMap, "target_types", "targetTypes", "toTypes", "target")

		if guidelines, ok := schemaMap["extraction_guidelines"].(string); ok {
			schema.ExtractionGuidelines = guidelines
		}

		schemas[typeName] = schema
	}

	return schemas
}

// parseTypesField extracts a []string from a schema map, trying multiple field names.
// Supports both array fields ([]any) and singular string fields.
func parseTypesField(schemaMap map[string]any, keys ...string) []string {
	for _, key := range keys {
		val, ok := schemaMap[key]
		if !ok {
			continue
		}
		// Array of strings
		if arr, ok := val.([]any); ok {
			var result []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
		// Singular string
		if s, ok := val.(string); ok && s != "" {
			return []string{s}
		}
	}
	return nil
}

// applySchemaOverrides merges overrides into the base schema.
func applySchemaOverrides(base agents.ObjectSchema, overrides any) agents.ObjectSchema {
	overrideMap, ok := overrides.(map[string]any)
	if !ok {
		return base
	}

	// Override description
	if desc, ok := overrideMap["description"].(string); ok {
		base.Description = desc
	}

	// Merge properties
	if props, ok := overrideMap["properties"].(map[string]any); ok {
		if base.Properties == nil {
			base.Properties = make(map[string]agents.PropertyDef)
		}
		for propName, propRaw := range props {
			propMap, ok := propRaw.(map[string]any)
			if !ok {
				continue
			}
			propDef := base.Properties[propName]
			if t, ok := propMap["type"].(string); ok {
				propDef.Type = t
			}
			if d, ok := propMap["description"].(string); ok {
				propDef.Description = d
			}
			base.Properties[propName] = propDef
		}
	}

	// Override required
	if req, ok := overrideMap["required"].([]any); ok {
		base.Required = nil
		for _, r := range req {
			if s, ok := r.(string); ok {
				base.Required = append(base.Required, s)
			}
		}
	}

	// Override extraction guidelines
	if guidelines, ok := overrideMap["extraction_guidelines"].(string); ok {
		base.ExtractionGuidelines = guidelines
	}

	return base
}
