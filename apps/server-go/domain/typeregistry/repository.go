package typeregistry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uptrace/bun"
)

// Repository handles database operations for the type registry
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new type registry repository
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// GetProjectTypes returns all types for a project with optional filtering
func (r *Repository) GetProjectTypes(ctx context.Context, projectID string, query ListTypesQuery) ([]TypeRegistryEntryDTO, error) {
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
			ptr.template_pack_id,
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
			tp.name as template_pack_name,
			COUNT(go.id) FILTER (WHERE go.deleted_at IS NULL) as object_count
		FROM kb.project_object_type_registry ptr
		LEFT JOIN kb.graph_template_packs tp ON ptr.template_pack_id = tp.id
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
		GROUP BY ptr.id, ptr.type_name, ptr.source, ptr.template_pack_id, 
			ptr.schema_version, ptr.json_schema, ptr.ui_config, 
			ptr.extraction_config, ptr.enabled, ptr.discovery_confidence, 
			ptr.description, ptr.created_by, ptr.created_at, ptr.updated_at, tp.name
		ORDER BY ptr.type_name
	`

	var rows []TypeRegistryRowDTO
	_, err = r.db.NewRaw(sql, args...).Exec(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("failed to query types: %w", err)
	}

	// Convert to DTOs
	result := make([]TypeRegistryEntryDTO, len(rows))
	for i, row := range rows {
		result[i] = row.ToDTO()
	}

	return result, nil
}

// GetTypeByName returns a specific type by name with relationships
func (r *Repository) GetTypeByName(ctx context.Context, projectID, typeName string) (*TypeRegistryEntryDTO, error) {
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
			ptr.template_pack_id,
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
			tp.name as template_pack_name,
			COUNT(go.id) FILTER (WHERE go.deleted_at IS NULL) as object_count
		FROM kb.project_object_type_registry ptr
		LEFT JOIN kb.graph_template_packs tp ON ptr.template_pack_id = tp.id
		LEFT JOIN kb.graph_objects go ON go.type = ptr.type_name 
			AND go.project_id = ptr.project_id 
			AND go.deleted_at IS NULL
		WHERE ptr.project_id = ? AND ptr.type_name = ?
		GROUP BY ptr.id, ptr.type_name, ptr.source, ptr.template_pack_id, 
			ptr.schema_version, ptr.json_schema, ptr.ui_config, 
			ptr.extraction_config, ptr.enabled, ptr.discovery_confidence, 
			ptr.description, ptr.created_by, ptr.created_at, ptr.updated_at, tp.name
	`

	var rows []TypeRegistryRowDTO
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
	// Get all active template packs for this project with their relationship schemas
	sql := `
		SELECT tp.relationship_type_schemas
		FROM kb.project_template_packs ptp
		JOIN kb.graph_template_packs tp ON ptp.template_pack_id = tp.id
		WHERE ptp.project_id = ? AND ptp.active = true
	`

	var results []struct {
		RelationshipTypeSchemas json.RawMessage `bun:"relationship_type_schemas"`
	}
	_, err := r.db.NewRaw(sql, projectID).Exec(ctx, &results)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query template packs: %w", err)
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

// GetTypeStats returns statistics for a project's type registry
func (r *Repository) GetTypeStats(ctx context.Context, projectID string) (*TypeRegistryStats, error) {
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
		FROM kb.project_object_type_registry ptr
		LEFT JOIN kb.graph_objects go ON go.type = ptr.type_name 
			AND go.project_id = ptr.project_id
		WHERE ptr.project_id = ?
	`

	var stats TypeRegistryStats
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
