package schemaregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Repository handles database operations for the schema registry
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new schema registry repository
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// GetProjectTypes returns all types for a project with optional filtering
func (r *Repository) GetProjectTypes(ctx context.Context, projectID string, query ListTypesQuery) ([]SchemaRegistryEntryDTO, error) {
	// Validate project exists and get org context
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	// Build the query with GROUP BY for object counts
	sql := `
		SELECT 
			ptr.id,
			ptr.type_name as type,
			ptr.source,
			ptr.schema_id,
			ptr.schema_version,
			ptr.json_schema,
			ptr.ui_config,
			ptr.extraction_config,
			ptr.enabled,
			ptr.discovery_confidence,
			ptr.description,
			ptr.created_by,
			ptr.created_at,
			ptr.updated_at,
			tp.name as schema_name,
			COUNT(go.id) FILTER (WHERE go.deleted_at IS NULL) as object_count
		FROM kb.project_object_schema_registry ptr
		LEFT JOIN kb.graph_schemas tp ON ptr.schema_id = tp.id
		LEFT JOIN kb.graph_objects go ON go.type = ptr.type_name 
			AND go.project_id = ptr.project_id 
			AND go.deleted_at IS NULL
		WHERE ptr.project_id = ?
	`

	args := []interface{}{projectID}
	argIdx := 2

	if query.EnabledOnly {
		sql += " AND ptr.enabled = true"
	}

	if query.Source != "" && query.Source != "all" {
		sql += fmt.Sprintf(" AND ptr.source = $%d", argIdx)
		args = append(args, query.Source)
		argIdx++
	}

	if query.Search != "" {
		sql += fmt.Sprintf(" AND (ptr.type_name ILIKE $%d OR ptr.description ILIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+query.Search+"%")
		argIdx++
	}

	sql += `
		GROUP BY ptr.id, ptr.type_name, ptr.source, ptr.schema_id, 
			ptr.schema_version, ptr.json_schema, ptr.ui_config, 
			ptr.extraction_config, ptr.enabled, ptr.discovery_confidence, 
			ptr.description, ptr.created_by, ptr.created_at, ptr.updated_at, tp.name
		ORDER BY ptr.type_name
	`

	var rows []SchemaRegistryRowDTO
	_, err = r.db.NewRaw(sql, args...).Exec(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("failed to query types: %w", err)
	}

	// Convert to DTOs
	result := make([]SchemaRegistryEntryDTO, len(rows))
	for i, row := range rows {
		result[i] = row.ToDTO()
	}

	// Also pull types from installed schema packs (blueprint-installed schemas
	// write to kb.project_schemas but may not populate project_object_schema_registry).
	// Merge in any types not already present in the registry result.
	packTypes, packErr := r.getTypesFromInstalledPacks(ctx, projectID)
	if packErr == nil && len(packTypes) > 0 {
		existing := make(map[string]bool, len(result))
		for _, r := range result {
			existing[r.Type] = true
		}

		// Collect new type names to fetch counts in one query
		var newTypes []string
		for _, pt := range packTypes {
			if !existing[pt.Type] {
				newTypes = append(newTypes, pt.Type)
			}
		}

		// Fetch object counts for new types in one query
		objectCounts := make(map[string]int)
		if len(newTypes) > 0 {
			type countRow struct {
				Type  string `bun:"type"`
				Count int    `bun:"count"`
			}
			var counts []countRow
			_, _ = r.db.NewRaw(`
				SELECT type, COUNT(*) as count
				FROM kb.graph_objects
				WHERE project_id = ? AND type = ANY(?) AND deleted_at IS NULL
				GROUP BY type
			`, projectID, newTypes).Exec(ctx, &counts)
			for _, c := range counts {
				objectCounts[c.Type] = c.Count
			}
		}

		for _, pt := range packTypes {
			if !existing[pt.Type] {
				pt.ObjectCount = objectCounts[pt.Type]
				result = append(result, pt)
			}
		}

		// Re-sort by type name
		for i := 1; i < len(result); i++ {
			for j := i; j > 0 && result[j].Type < result[j-1].Type; j-- {
				result[j], result[j-1] = result[j-1], result[j]
			}
		}
	}

	return result, nil
}

// getTypesFromInstalledPacks returns SchemaRegistryEntryDTOs derived from
// schema packs installed on the project (via kb.project_schemas).
// These are types that exist in the schema definition but may not have been
// individually registered in kb.project_object_schema_registry.
func (r *Repository) getTypesFromInstalledPacks(ctx context.Context, projectID string) ([]SchemaRegistryEntryDTO, error) {
	type packRow struct {
		SchemaID          string          `bun:"schema_id"`
		SchemaName        string          `bun:"schema_name"`
		ObjectTypeSchemas json.RawMessage `bun:"object_type_schemas"`
		UIConfigs         json.RawMessage `bun:"ui_configs"`
	}

	var packs []packRow
	_, err := r.db.NewRaw(`
		SELECT ps.schema_id, gs.name as schema_name,
		       gs.object_type_schemas, gs.ui_configs
		FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON gs.id = ps.schema_id
		WHERE ps.project_id = ? AND ps.active = true
	`, projectID).Exec(ctx, &packs)
	if err != nil {
		return nil, err
	}

	// objectTypeSchema is the per-type definition stored in the pack JSON array
	type objectTypeSchema struct {
		Name        string                 `json:"name"`
		Label       string                 `json:"label"`
		Description string                 `json:"description"`
		Properties  map[string]interface{} `json:"properties"`
	}

	var result []SchemaRegistryEntryDTO
	now := time.Now()

	for _, pack := range packs {
		if len(pack.ObjectTypeSchemas) == 0 {
			continue
		}

		// Parse ui_configs map: typeName → {color, icon, ...}
		var uiConfigs map[string]map[string]interface{}
		if len(pack.UIConfigs) > 0 {
			_ = json.Unmarshal(pack.UIConfigs, &uiConfigs)
		}

		// Object type schemas may be a JSON array or a JSON object (map)
		var objectTypes []objectTypeSchema
		if err := json.Unmarshal(pack.ObjectTypeSchemas, &objectTypes); err != nil {
			// Try as map
			var objectMap map[string]objectTypeSchema
			if err2 := json.Unmarshal(pack.ObjectTypeSchemas, &objectMap); err2 == nil {
				for name, ot := range objectMap {
					ot.Name = name
					objectTypes = append(objectTypes, ot)
				}
			}
		}

		schemaID := pack.SchemaID
		schemaName := pack.SchemaName

		for _, ot := range objectTypes {
			if ot.Name == "" {
				continue
			}

			uiConfig := map[string]interface{}{}
			if cfg, ok := uiConfigs[ot.Name]; ok {
				uiConfig = cfg
			}

			// Build a minimal JSON schema from the type definition
			jsonSchema, _ := json.Marshal(map[string]interface{}{
				"type":        "object",
				"description": ot.Description,
				"properties":  ot.Properties,
			})

			desc := ot.Description
			result = append(result, SchemaRegistryEntryDTO{
				Type:          ot.Name,
				Source:        "template",
				SchemaID:      &schemaID,
				SchemaName:    &schemaName,
				SchemaVersion: 1,
				JSONSchema:    json.RawMessage(jsonSchema),
				UIConfig:      uiConfig,
				Enabled:       true,
				Description:   &desc,
				CreatedAt:     now,
				UpdatedAt:     now,
			})
		}
	}

	return result, nil
}

// GetTypeByName returns a specific type by name with relationships
func (r *Repository) GetTypeByName(ctx context.Context, projectID, typeName string) (*SchemaRegistryEntryDTO, error) {
	// Validate project exists
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	sql := `
		SELECT 
			ptr.id,
			ptr.type_name as type,
			ptr.source,
			ptr.schema_id,
			ptr.schema_version,
			ptr.json_schema,
			ptr.ui_config,
			ptr.extraction_config,
			ptr.enabled,
			ptr.discovery_confidence,
			ptr.description,
			ptr.created_by,
			ptr.created_at,
			ptr.updated_at,
			tp.name as schema_name,
			COUNT(go.id) FILTER (WHERE go.deleted_at IS NULL) as object_count
		FROM kb.project_object_schema_registry ptr
		LEFT JOIN kb.graph_schemas tp ON ptr.schema_id = tp.id
		LEFT JOIN kb.graph_objects go ON go.type = ptr.type_name 
			AND go.project_id = ptr.project_id 
			AND go.deleted_at IS NULL
		WHERE ptr.project_id = ? AND ptr.type_name = ?
		GROUP BY ptr.id, ptr.type_name, ptr.source, ptr.schema_id, 
			ptr.schema_version, ptr.json_schema, ptr.ui_config, 
			ptr.extraction_config, ptr.enabled, ptr.discovery_confidence, 
			ptr.description, ptr.created_by, ptr.created_at, ptr.updated_at, tp.name
	`

	var rows []SchemaRegistryRowDTO
	_, err = r.db.NewRaw(sql, projectID, typeName).Exec(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("failed to query type: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("type not found: %s", typeName)
	}

	dto := rows[0].ToDTO()

	// Get relationships for this type
	outgoing, incoming, err := r.getRelationshipsForType(ctx, projectID, typeName)
	if err != nil {
		// Log but don't fail - relationships are optional
		fmt.Printf("Warning: failed to get relationships for type %s: %v\n", typeName, err)
	}
	dto.OutgoingRelationships = outgoing
	dto.IncomingRelationships = incoming

	return &dto, nil
}

// getRelationshipsForType returns the outgoing and incoming relationships for a type
func (r *Repository) getRelationshipsForType(ctx context.Context, projectID, typeName string) ([]RelationshipTypeInfo, []RelationshipTypeInfo, error) {
	// Get all active memory schemas for this project with their relationship schemas
	sql := `
		SELECT tp.relationship_type_schemas
		FROM kb.project_schemas ptp
		JOIN kb.graph_schemas tp ON ptp.schema_id = tp.id
		WHERE ptp.project_id = ? AND ptp.active = true
	`

	var results []struct {
		RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas"`
	}
	_, err := r.db.NewRaw(sql, projectID).Exec(ctx, &results)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query memory schemas: %w", err)
	}

	outgoing := []RelationshipTypeInfo{}
	incoming := []RelationshipTypeInfo{}
	seenOutgoing := make(map[string]bool)
	seenIncoming := make(map[string]bool)

	for _, row := range results {
		if row.RelationshipTypeSchemas == nil {
			continue
		}

		var schemas map[string]RelationshipSchema
		if err := json.Unmarshal(row.RelationshipTypeSchemas, &schemas); err != nil {
			continue
		}

		for relType, schema := range schemas {
			sourceTypes := schema.GetSourceTypes()
			targetTypes := schema.GetTargetTypes()

			// Check if this type is a source (outgoing relationships)
			if contains(sourceTypes, typeName) && !seenOutgoing[relType] {
				seenOutgoing[relType] = true
				info := RelationshipTypeInfo{
					Type:        relType,
					TargetTypes: targetTypes,
				}
				if schema.Label != "" {
					info.Label = &schema.Label
				}
				if schema.Description != "" {
					info.Description = &schema.Description
				}
				if schema.InverseType != "" {
					info.InverseType = &schema.InverseType
				}
				outgoing = append(outgoing, info)
			}

			// Check if this type is a target (incoming relationships)
			if contains(targetTypes, typeName) && !seenIncoming[relType] {
				seenIncoming[relType] = true
				info := RelationshipTypeInfo{
					Type:        relType,
					SourceTypes: sourceTypes,
				}
				if schema.Label != "" {
					info.Label = &schema.Label
				}
				if schema.InverseLabel != "" {
					info.InverseLabel = &schema.InverseLabel
				}
				if schema.Description != "" {
					info.Description = &schema.Description
				}
				incoming = append(incoming, info)
			}
		}
	}

	return outgoing, incoming, nil
}

// GetTypeStats returns statistics for a project's schema registry
func (r *Repository) GetTypeStats(ctx context.Context, projectID string) (*SchemaRegistryStats, error) {
	// Validate project exists
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	sql := `
		SELECT 
			COUNT(DISTINCT ptr.id) as total_types,
			COUNT(DISTINCT ptr.id) FILTER (WHERE ptr.enabled = true) as enabled_types,
			COUNT(DISTINCT ptr.id) FILTER (WHERE ptr.source = 'template') as template_types,
			COUNT(DISTINCT ptr.id) FILTER (WHERE ptr.source = 'custom') as custom_types,
			COUNT(DISTINCT ptr.id) FILTER (WHERE ptr.source = 'discovered') as discovered_types,
			COUNT(go.id) FILTER (WHERE go.deleted_at IS NULL) as total_objects,
			COUNT(DISTINCT go.type) FILTER (WHERE go.deleted_at IS NULL) as types_with_objects
		FROM kb.project_object_schema_registry ptr
		LEFT JOIN kb.graph_objects go ON go.type = ptr.type_name 
			AND go.project_id = ptr.project_id
		WHERE ptr.project_id = ?
	`

	var stats SchemaRegistryStats
	_, err = r.db.NewRaw(sql, projectID).Exec(ctx, &stats)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	return &stats, nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// CreateType registers a new custom object type for a project
func (r *Repository) CreateType(ctx context.Context, projectID, userID string, req *CreateTypeRequest) (*ProjectObjectSchemaRegistry, error) {
	// Validate project exists
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	// Check if type already exists for this project
	exists, err := r.db.NewSelect().
		Model((*ProjectObjectSchemaRegistry)(nil)).
		Where("project_id = ?", projectID).
		Where("type_name = ?", req.TypeName).
		Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check type existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("type already exists: %s", req.TypeName)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now()
	entry := &ProjectObjectSchemaRegistry{
		ProjectID:        projectID,
		TypeName:         req.TypeName,
		Source:           "custom",
		SchemaVersion:    1,
		JSONSchema:       req.JSONSchema,
		UIConfig:         req.UIConfig,
		ExtractionConfig: req.ExtractionConfig,
		Enabled:          enabled,
		Description:      req.Description,
		CreatedBy:        &userID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err = r.db.NewInsert().Model(entry).Returning("id, created_at, updated_at").Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create type: %w", err)
	}

	return entry, nil
}

// UpdateType updates an existing type in the project schema registry
func (r *Repository) UpdateType(ctx context.Context, projectID, typeName string, req *UpdateTypeRequest) (*ProjectObjectSchemaRegistry, error) {
	// Validate project exists
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}

	q := r.db.NewUpdate().
		Model((*ProjectObjectSchemaRegistry)(nil)).
		Where("project_id = ?", projectID).
		Where("type_name = ?", typeName).
		Set("updated_at = ?", time.Now())

	hasUpdates := false

	if req.Description != nil {
		q = q.Set("description = ?", *req.Description)
		hasUpdates = true
	}
	if len(req.JSONSchema) > 0 {
		q = q.Set("json_schema = ?", string(req.JSONSchema))
		q = q.Set("schema_version = schema_version + 1")
		hasUpdates = true
	}
	if len(req.UIConfig) > 0 {
		q = q.Set("ui_config = ?", string(req.UIConfig))
		hasUpdates = true
	}
	if len(req.ExtractionConfig) > 0 {
		q = q.Set("extraction_config = ?", string(req.ExtractionConfig))
		hasUpdates = true
	}
	if req.Enabled != nil {
		q = q.Set("enabled = ?", *req.Enabled)
		hasUpdates = true
	}

	if !hasUpdates {
		return nil, fmt.Errorf("no update fields provided")
	}

	result, err := q.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update type: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("type not found: %s", typeName)
	}

	// Fetch and return the updated entry
	var entry ProjectObjectSchemaRegistry
	err = r.db.NewSelect().
		Model(&entry).
		Where("project_id = ?", projectID).
		Where("type_name = ?", typeName).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated type: %w", err)
	}

	return &entry, nil
}

// DeleteType removes a type from the project schema registry
func (r *Repository) DeleteType(ctx context.Context, projectID, typeName string) error {
	// Validate project exists
	var project Project
	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectID)
	}

	result, err := r.db.NewDelete().
		Model((*ProjectObjectSchemaRegistry)(nil)).
		Where("project_id = ?", projectID).
		Where("type_name = ?", typeName).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete type: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("type not found: %s", typeName)
	}

	return nil
}
