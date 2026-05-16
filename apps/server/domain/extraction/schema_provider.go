// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/uptrace/bun"
)

// SchemaExtractionPrompts holds domain-specific extraction guidance stored with a schema pack.
// Written by the discovery agent after schema creation (Phase 7).
type SchemaExtractionPrompts struct {
	// DomainContext is injected into the entity extractor system prompt.
	DomainContext string `json:"domainContext,omitempty"`
	// TypeHints are per-type extraction hints keyed by type name.
	TypeHints map[string]string `json:"typeHints,omitempty"`
	// RelationshipHints are per-relationship-type extraction hints.
	RelationshipHints map[string]string `json:"relationshipHints,omitempty"`
	// NegativeExamples describes what NOT to extract (reduces false positives).
	NegativeExamples []string `json:"negativeExamples,omitempty"`
}

// ClassificationSignals are written back to a document after domain classification.
type ClassificationSignals struct {
	// MatchedSchemaID is the schema pack that matched (if any).
	MatchedSchemaID *string `json:"matchedSchemaId,omitempty"`
	// MatchedSchemaName is the name of the matched schema pack.
	MatchedSchemaName *string `json:"matchedSchemaName,omitempty"`
	// HeuristicKeywords are keywords that contributed to classification.
	HeuristicKeywords []string `json:"heuristicKeywords,omitempty"`
	// LLMReason is a short explanation from the LLM classifier.
	LLMReason string `json:"llmReason,omitempty"`
	// ClassifiedAt is the time the classification was performed.
	ClassifiedAt string `json:"classifiedAt,omitempty"`
}

// Scan implements sql.Scanner for SchemaExtractionPrompts.
func (p *SchemaExtractionPrompts) Scan(value any) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, p)
	case string:
		return json.Unmarshal([]byte(v), p)
	default:
		return fmt.Errorf("unsupported type for SchemaExtractionPrompts: %T", value)
	}
}

// Value implements driver.Valuer for SchemaExtractionPrompts.
func (p SchemaExtractionPrompts) Value() (any, error) {
	return json.Marshal(p)
}

// GraphMemorySchema represents a memory schema from kb.graph_schemas.
// Memory schemas are GLOBAL resources shared across all organizations.
type GraphMemorySchema struct {
	bun.BaseModel `bun:"kb.graph_schemas,alias:gtp"`

	ID                      string                   `bun:"id,pk,type:uuid"`
	Name                    string                   `bun:"name,notnull"`
	Version                 string                   `bun:"version,notnull"`
	ParentVersionID         *string                  `bun:"parent_version_id,type:uuid"`
	Draft                   bool                     `bun:"draft,default:false"`
	Description             *string                  `bun:"description"`
	Author                  *string                  `bun:"author"`
	License                 *string                  `bun:"license"`
	RepositoryURL           *string                  `bun:"repository_url"`
	DocumentationURL        *string                  `bun:"documentation_url"`
	Source                  *string                  `bun:"source"` // manual, discovered, imported, system
	DiscoveryJobID          *string                  `bun:"discovery_job_id,type:uuid"`
	PendingReview           bool                     `bun:"pending_review,default:false"`
	ObjectTypeSchemas       json.RawMessage          `bun:"object_type_schemas,type:jsonb,notnull"`
	RelationshipTypeSchemas json.RawMessage          `bun:"relationship_type_schemas,type:jsonb,default:'{}'"`
	UIConfigs               JSON                     `bun:"ui_configs,type:jsonb,default:'{}'"`
	ExtractionPrompts       *SchemaExtractionPrompts `bun:"extraction_prompts,type:jsonb,default:'{}'"`
	SQLViews                JSONArray                `bun:"sql_views,type:jsonb,default:'[]'"`
	Signature               *string                  `bun:"signature"`
	Checksum                *string                  `bun:"checksum"`
	PublishedAt             time.Time                `bun:"published_at,default:now()"`
	DeprecatedAt            *time.Time               `bun:"deprecated_at"`
	SupersededBy            *string                  `bun:"superseded_by"`
	CreatedAt               time.Time                `bun:"created_at,default:now()"`
	UpdatedAt               time.Time                `bun:"updated_at,default:now()"`
}

// ProjectMemorySchema represents a memory schema installation for a project.
// Maps kb.project_schemas table.
type ProjectMemorySchema struct {
	bun.BaseModel `bun:"kb.project_schemas,alias:ptp"`

	ID             string                      `bun:"id,pk,type:uuid"`
	ProjectID      string                      `bun:"project_id,notnull,type:uuid"`
	SchemaID       string                      `bun:"schema_id,notnull,type:uuid"`
	InstalledAt    time.Time                   `bun:"installed_at,default:now()"`
	InstalledBy    *string                     `bun:"installed_by,type:uuid"`
	Active         bool                        `bun:"active,default:true"`
	Customizations *MemorySchemaCustomizations `bun:"customizations,type:jsonb,default:'{}'"`
	CreatedAt      time.Time                   `bun:"created_at,default:now()"`
	UpdatedAt      time.Time                   `bun:"updated_at,default:now()"`

	// Joined fields
	MemorySchema *GraphMemorySchema `bun:"rel:belongs-to,join:schema_id=id"`
}

// MemorySchemaCustomizations holds installation-specific customizations.
type MemorySchemaCustomizations struct {
	EnabledTypes    []string       `json:"enabledTypes,omitempty"`
	DisabledTypes   []string       `json:"disabledTypes,omitempty"`
	SchemaOverrides map[string]any `json:"schemaOverrides,omitempty"`
}

// Scan implements sql.Scanner for MemorySchemaCustomizations.
func (c *MemorySchemaCustomizations) Scan(value any) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	default:
		return fmt.Errorf("unsupported type for MemorySchemaCustomizations: %T", value)
	}
}

// Value implements driver.Valuer for MemorySchemaCustomizations.
func (c MemorySchemaCustomizations) Value() (any, error) {
	return json.Marshal(c)
}

// MemorySchemaProvider implements SchemaProvider by loading schemas
// from memory schemas assigned to a project.
type MemorySchemaProvider struct {
	db               bun.IDB
	embeddingService EmbeddingService
	log              *slog.Logger
}

// NewMemorySchemaProvider creates a new memory schema provider.
func NewMemorySchemaProvider(db bun.IDB, log *slog.Logger) *MemorySchemaProvider {
	return &MemorySchemaProvider{
		db:  db,
		log: log.With(logger.Scope("memory-schema-provider")),
	}
}

// WithEmbeddingService sets the embedding service used to pre-compute schema
// pack embeddings during GetInstalledSchemaSummaries.
func (p *MemorySchemaProvider) WithEmbeddingService(svc EmbeddingService) *MemorySchemaProvider {
	p.embeddingService = svc
	return p
}

// GetProjectSchemas loads and merges schemas from all active memory schemas for a project.
// Later schemas override earlier ones for the same type.
func (p *MemorySchemaProvider) GetProjectSchemas(
	ctx context.Context,
	projectID string,
) (*ExtractionSchemas, error) {
	// Get all active memory schema assignments for this project
	var assignments []ProjectMemorySchema
	err := p.db.NewSelect().
		Model(&assignments).
		Relation("MemorySchema").
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
		return nil, fmt.Errorf("query project memory schemas: %w", err)
	}

	if len(assignments) == 0 {
		return &ExtractionSchemas{
			ObjectSchemas:       make(map[string]agents.ObjectSchema),
			RelationshipSchemas: make(map[string]agents.RelationshipSchema),
		}, nil
	}

	// Merge all schemas from all assigned schemas
	objectSchemas := make(map[string]agents.ObjectSchema)
	relationshipSchemas := make(map[string]agents.RelationshipSchema)

	for _, assignment := range assignments {
		if assignment.MemorySchema == nil {
			continue
		}

		schema := assignment.MemorySchema

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
		objSchemas := parseObjectTypeSchemas(schema.ObjectTypeSchemas)
		for typeName, objSchema := range objSchemas {
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
				objSchema = applySchemaOverrides(objSchema, overrides)
			}

			objectSchemas[typeName] = objSchema
		}

		// Merge relationship type schemas
		relSchemas := parseRelationshipTypeSchemas(schema.RelationshipTypeSchemas)
		for typeName, relSchema := range relSchemas {
			// Skip disabled types
			if disabledTypes[typeName] {
				continue
			}
			// If enabledTypes is set, only include those types
			if len(enabledTypes) > 0 && !enabledTypes[typeName] {
				continue
			}

			relationshipSchemas[typeName] = relSchema
		}

		p.log.Debug("merged memory schema",
			slog.String("schema_name", schema.Name),
			slog.String("schema_version", schema.Version),
			slog.Int("object_types", len(objSchemas)),
			slog.Int("relationship_types", len(relSchemas)))
	}

	p.log.Info("loaded project schemas",
		slog.String("project_id", projectID),
		slog.Int("memory_schemas", len(assignments)),
		slog.Int("object_types", len(objectSchemas)),
		slog.Int("relationship_types", len(relationshipSchemas)))

	return &ExtractionSchemas{
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	}, nil
}

// GetInstalledSchemaSummaries returns lightweight summaries of installed schema packs
// for use by the DocumentClassifier.
func (p *MemorySchemaProvider) GetInstalledSchemaSummaries(
	ctx context.Context,
	projectID string,
) ([]InstalledSchemaSummary, error) {
	var assignments []ProjectMemorySchema
	err := p.db.NewSelect().
		Model(&assignments).
		Relation("MemorySchema").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query schema summaries: %w", err)
	}

	summaries := make([]InstalledSchemaSummary, 0, len(assignments))
	for _, a := range assignments {
		if a.MemorySchema == nil {
			continue
		}
		ms := a.MemorySchema
		desc := ""
		if ms.Description != nil {
			desc = *ms.Description
		}
		// Build keyword list from schema name and installed object type names.
		keywords := buildKeywordsFromSchema(ms)
		var ep *SchemaExtractionPrompts
		if ms.ExtractionPrompts != nil {
			ep = ms.ExtractionPrompts
		}
		// Collect object type names for LLM prompt and per-type embeddings.
		typeNames := buildTypeNamesFromSchema(ms)
		summary := InstalledSchemaSummary{
			ID:                ms.ID,
			Name:              ms.Name,
			Description:       desc,
			Keywords:          keywords,
			TypeNames:         typeNames,
			ExtractionPrompts: ep,
		}
		// Compute pack-level embedding and per-type embeddings when available.
		if p.embeddingService != nil && p.embeddingService.IsEnabled() {
			embText := buildSchemaEmbeddingText(ms)
			if embText != "" {
				emb, embErr := p.embeddingService.EmbedQuery(ctx, embText)
				if embErr != nil {
					p.log.Warn("schema embedding failed, skipping vector classification for pack",
						slog.String("pack", ms.Name),
						logger.Error(embErr),
					)
				} else {
					summary.Embedding = emb
				}
			}
			// Embed each object type name individually for Fix 3 (type-similarity matching).
			if len(typeNames) > 0 {
				typeEmbs := make(map[string][]float32, len(typeNames))
				for _, tn := range typeNames {
					te, teErr := p.embeddingService.EmbedQuery(ctx, tn)
					if teErr != nil {
						p.log.Warn("type embedding failed",
							slog.String("pack", ms.Name),
							slog.String("type", tn),
							logger.Error(teErr),
						)
						continue
					}
					typeEmbs[tn] = te
				}
				if len(typeEmbs) > 0 {
					summary.TypeEmbeddings = typeEmbs
				}
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// buildTypeNamesFromSchema returns the object type names defined in a schema pack.
// Used to populate InstalledSchemaSummary.TypeNames for the LLM classification prompt.
func buildTypeNamesFromSchema(ms *GraphMemorySchema) []string {
	seen := map[string]bool{}
	var names []string
	for typeName := range parseObjectTypeSchemas(ms.ObjectTypeSchemas) {
		if !seen[typeName] {
			seen[typeName] = true
			names = append(names, typeName)
		}
	}
	// Also include type hint keys from extraction prompts (may differ from schema types).
	if ms.ExtractionPrompts != nil {
		for k := range ms.ExtractionPrompts.TypeHints {
			if !seen[k] {
				seen[k] = true
				names = append(names, k)
			}
		}
	}
	return names
}

// buildKeywordsFromSchema extracts keyword signals from a schema pack's name and type list.
func buildKeywordsFromSchema(ms *GraphMemorySchema) []string {
	seen := map[string]bool{}
	var kws []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			kws = append(kws, s)
		}
	}
	// Schema name words.
	for _, word := range strings.Fields(ms.Name) {
		add(word)
	}
	// Object type names.
	for typeName := range parseObjectTypeSchemas(ms.ObjectTypeSchemas) {
		add(typeName)
		// Also add individual words for compound types like "MedicalRecord".
		for _, w := range splitCamelCase(typeName) {
			add(w)
		}
	}
	return kws
}

// buildSchemaEmbeddingText builds a text representation of a schema pack
// suitable for embedding. Uses domainContext + type hint names for semantic richness.
func buildSchemaEmbeddingText(ms *GraphMemorySchema) string {
	var parts []string
	parts = append(parts, ms.Name)
	if ms.Description != nil && *ms.Description != "" {
		parts = append(parts, *ms.Description)
	}
	if ms.ExtractionPrompts != nil {
		if ms.ExtractionPrompts.DomainContext != "" {
			parts = append(parts, ms.ExtractionPrompts.DomainContext)
		}
		typeNames := make([]string, 0, len(ms.ExtractionPrompts.TypeHints))
		for k := range ms.ExtractionPrompts.TypeHints {
			typeNames = append(typeNames, k)
		}
		if len(typeNames) > 0 {
			parts = append(parts, "Types: "+strings.Join(typeNames, ", "))
		}
	}
	// Also include object type names from the schema definition.
	for typeName := range parseObjectTypeSchemas(ms.ObjectTypeSchemas) {
		parts = append(parts, typeName)
	}
	return strings.Join(parts, ". ")
}

// splitCamelCase splits a CamelCase or snake_case string into lowercase words.
func splitCamelCase(s string) []string {
	// Replace underscores/hyphens with space, then split on uppercase boundaries.
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	var result []string
	var cur strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			if cur.Len() > 0 {
				result = append(result, strings.ToLower(cur.String()))
				cur.Reset()
			}
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		result = append(result, strings.ToLower(cur.String()))
	}
	return result
}

// parseObjectTypeSchemas converts object_type_schemas JSONB to a map of ObjectSchema.
// The column can be stored in two formats:
//   - Map format (blueprint seeds / epf-engine v3): {"TypeName": {label, description, properties, ...}, ...}
//   - Array format (user YAML / epf-cli generate): [{"name": "TypeName", ...}, ...]
func parseObjectTypeSchemas(raw json.RawMessage) map[string]agents.ObjectSchema {
	schemas := make(map[string]agents.ObjectSchema)
	if len(raw) == 0 {
		return schemas
	}

	// Normalise to map format regardless of which storage format the DB contains.
	normalized := normalizeObjectTypeSchemasToMap(raw)
	if normalized == nil {
		return schemas
	}

	for typeName, schemaMap := range normalized {
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

		if guidelines, ok := schemaMap["extraction_guidelines"].(string); ok {
			schema.ExtractionGuidelines = guidelines
		}

		schemas[typeName] = schema
	}

	return schemas
}

// normalizeObjectTypeSchemasToMap converts either storage format to map[typeName]map[string]any.
// Array format: [{"name": "TypeName", ...}, ...] → {"TypeName": {...}, ...}
// Map format:   {"TypeName": {...}, ...}          → unchanged
func normalizeObjectTypeSchemasToMap(raw json.RawMessage) map[string]map[string]any {
	if len(raw) == 0 {
		return nil
	}

	// Try array format first.
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		result := make(map[string]map[string]any, len(arr))
		for _, item := range arr {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			result[name] = item
		}
		return result
	}

	// Try map format.
	var m map[string]map[string]any
	if json.Unmarshal(raw, &m) == nil {
		return m
	}

	return nil
}

// parseRelationshipTypeSchemas converts relationship_type_schemas JSONB to a map of RelationshipSchema.
// Handles multiple field naming conventions for source/target types:
//   - source_types / target_types (snake_case)
//   - sourceTypes / targetTypes (camelCase)
//   - fromTypes / toTypes (alternative camelCase)
//   - source / target (singular string)
//
// The column can be stored in map format ({"TypeName": {...}}) or array format
// ([{"name": "TypeName", ...}]); both are handled.
func parseRelationshipTypeSchemas(raw json.RawMessage) map[string]agents.RelationshipSchema {
	schemas := make(map[string]agents.RelationshipSchema)
	if len(raw) == 0 {
		return schemas
	}

	normalized := normalizeRelTypeSchemasToMap(raw)
	if normalized == nil {
		return schemas
	}

	for typeName, schemaMap := range normalized {
		schema := agents.RelationshipSchema{
			Name: typeName,
		}

		if desc, ok := schemaMap["description"].(string); ok {
			schema.Description = desc
		}

		schema.SourceTypes = parseTypesField(schemaMap, "source_types", "sourceTypes", "fromTypes", "source")
		schema.TargetTypes = parseTypesField(schemaMap, "target_types", "targetTypes", "toTypes", "target")

		if guidelines, ok := schemaMap["extraction_guidelines"].(string); ok {
			schema.ExtractionGuidelines = guidelines
		}

		schemas[typeName] = schema
	}

	return schemas
}

// normalizeRelTypeSchemasToMap converts either storage format to map[typeName]map[string]any.
func normalizeRelTypeSchemasToMap(raw json.RawMessage) map[string]map[string]any {
	if len(raw) == 0 {
		return nil
	}

	// Try array format.
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		result := make(map[string]map[string]any, len(arr))
		for _, item := range arr {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			result[name] = item
		}
		return result
	}

	// Try map format.
	var m map[string]map[string]any
	if json.Unmarshal(raw, &m) == nil {
		return m
	}

	return nil
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
