package schemas

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles database operations for schemas
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new schemas repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("schemas.repo")),
	}
}

// GetCompiledTypesByProject returns compiled object and relationship types for a project
func (r *Repository) GetCompiledTypesByProject(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	// Get all active schemas for the project, ordered by install date ascending
	// (so later installs have higher priority; shadowing detected by seeing a name twice)
	var projectPacks []ProjectMemorySchema
	err := r.db.NewSelect().
		Model(&projectPacks).
		Relation("MemorySchema").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Where("ptp.removed_at IS NULL").
		Order("ptp.installed_at ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get project schemas", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	response := &CompiledTypesResponse{
		ObjectTypes:       []ObjectTypeSchema{},
		RelationshipTypes: []RelationshipTypeSchema{},
	}

	// Track seen type names; later installs override earlier ones (mark earlier as shadowed)
	seenObjIdx := map[string]int{} // typeName → index in response.ObjectTypes
	// For relationships, the key is "name|sourceType|targetType" because the same relationship
	// name can legitimately appear multiple times with different source/target type pairs.
	// Cross-pack shadowing only applies to the exact same (name, source, target) triple.
	seenRelIdx := map[string]int{} // "name|sourceType|targetType" → index in response.RelationshipTypes

	// Compile types from all active packs
	for _, pp := range projectPacks {
		if pp.MemorySchema == nil {
			continue
		}

		tp := pp.MemorySchema

		// Type-level UI config may be declared either inline per type (a "ui"
		// key inside the object type schema, as blueprint manifests write it)
		// or in the pack's top-level ui_configs map (typeName → {icon, color}),
		// which the schema registry surfaces today. Prefer the inline form and
		// fall back to ui_configs so both conventions reach the compiled view.
		uiConfigs := map[string]json.RawMessage{}
		if len(tp.UIConfigs) > 0 {
			if err := json.Unmarshal(tp.UIConfigs, &uiConfigs); err != nil {
				uiConfigs = map[string]json.RawMessage{}
				r.log.Warn("failed to parse ui_configs", logger.Error(err))
			}
		}

		// Parse object type schemas (supports both array and map storage formats)
		if len(tp.ObjectTypeSchemas) > 0 {
			objectTypes := parseObjectTypeSchemas(tp.ObjectTypeSchemas, tp.ID, tp.Name, tp.Version)
			if objectTypes == nil {
				r.log.Warn("failed to parse object type schemas",
					slog.String("packId", tp.ID))
			} else {
				applyUIConfigFallback(objectTypes, uiConfigs)
				for i := range objectTypes {
					if prevIdx, seen := seenObjIdx[objectTypes[i].Name]; seen {
						// Mark the earlier one as shadowed
						response.ObjectTypes[prevIdx].Shadowed = true
					}
					seenObjIdx[objectTypes[i].Name] = len(response.ObjectTypes)
					response.ObjectTypes = append(response.ObjectTypes, objectTypes[i])
				}
			}
		}

		// Parse relationship type schemas
		if len(tp.RelationshipTypeSchemas) > 0 {
			relTypes := parseRelationshipTypeSchemas(tp.RelationshipTypeSchemas, tp.ID, tp.Name, tp.Version)
			if relTypes == nil {
				r.log.Warn("failed to parse relationship type schemas",
					slog.String("packId", tp.ID))
			} else {
				for i := range relTypes {
					relKey := relTypes[i].Name + "|" + relTypes[i].SourceType + "|" + relTypes[i].TargetType
					if prevIdx, seen := seenRelIdx[relKey]; seen {
						response.RelationshipTypes[prevIdx].Shadowed = true
					}
					seenRelIdx[relKey] = len(response.RelationshipTypes)
					response.RelationshipTypes = append(response.RelationshipTypes, relTypes[i])
				}
			}
		}
	}

	return response, nil
}

// applyUIConfigFallback fills each object type's ui metadata from the pack's
// top-level ui_configs map (typeName → {icon, color}) when the type declares no
// inline ui of its own. ui_configs entries whose raw value is a JSON null are
// treated as absent.
func applyUIConfigFallback(objectTypes []ObjectTypeSchema, uiConfigs map[string]json.RawMessage) {
	for i := range objectTypes {
		if len(objectTypes[i].UI) == 0 {
			if cfg, ok := uiConfigs[objectTypes[i].Name]; ok && !isNullJSON(cfg) {
				objectTypes[i].UI = cfg
			}
		}
	}
}

// isNullJSON reports whether raw is the JSON literal null, ignoring surrounding
// whitespace. An explicit "ui": null must be treated as absent so it neither
// blocks the ui_configs fallback nor leaks "ui":null into the compiled output.
func isNullJSON(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// GetAvailablePacks returns schemas available for a project to install.
// Only returns schemas owned by this project that are not yet installed.
func (r *Repository) GetAvailablePacks(ctx context.Context, projectID string) ([]MemorySchemaListItem, error) {
	// Get IDs of packs already installed for this project
	var installedIDs []string
	err := r.db.NewSelect().
		Model((*ProjectMemorySchema)(nil)).
		Column("schema_id").
		Where("project_id = ?", projectID).
		Scan(ctx, &installedIDs)
	if err != nil {
		r.log.Error("failed to get installed pack IDs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Get packs not installed for this project, scoped to project
	var packs []MemorySchemaListItem
	q := r.db.NewSelect().
		Model((*GraphMemorySchema)(nil)).
		Column("id", "name", "version", "description", "author")

	if len(installedIDs) > 0 {
		q = q.Where("id NOT IN (?)", bun.In(installedIDs))
	}

	q = q.Where("project_id = ?", projectID)

	err = q.Order("name ASC").Scan(ctx, &packs)
	if err != nil {
		r.log.Error("failed to get available packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if packs == nil {
		return []MemorySchemaListItem{}, nil
	}
	return packs, nil
}

// ListSchemaPacks returns the schema catalog strictly scoped to a project —
// only rows whose project_id equals projectID. Rows with a NULL project_id
// (builtin/shared schemas) are never returned; they surface only through the
// installed-packs join. This mirrors the MCP schema-list tool so REST consumers
// can bypass the MCP handshake. Rows are ordered by updated_at DESC and
// paginated; total ignores limit/offset.
func (r *Repository) ListSchemaPacks(ctx context.Context, projectID, search string, limit, offset int) ([]SchemaListInfo, int, error) {
	if projectID == "" {
		return nil, 0, apperror.NewBadRequest("projectId is required")
	}

	rows := make([]SchemaListInfo, 0)
	q := r.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Column("id", "name", "version", "description", "project_id", "source", "created_at", "updated_at").
		Where("project_id = ?", projectID)
	if search != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if err := q.Order("updated_at DESC").Limit(limit).Offset(offset).Scan(ctx, &rows); err != nil {
		r.log.Error("failed to list schema packs", logger.Error(err))
		return nil, 0, apperror.NewInternal("failed to list schema packs", err)
	}

	var total int
	countQ := r.db.NewSelect().
		TableExpr("kb.graph_schemas").
		Where("project_id = ?", projectID)
	if search != "" {
		countQ = countQ.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if err := countQ.ColumnExpr("COUNT(*)").Scan(ctx, &total); err != nil {
		r.log.Warn("failed to count schema packs", logger.Error(err))
		total = len(rows)
	}

	return rows, total, nil
}

// GetInstalledPacks returns schemas installed for a project
func (r *Repository) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledSchemaItem, error) {
	var results []struct {
		ID                string                 `bun:"id"`
		SchemaID          string                 `bun:"schema_id"`
		Name              string                 `bun:"name"`
		Version           string                 `bun:"version"`
		Description       *string                `bun:"description"`
		Active            bool                   `bun:"active"`
		InstalledAt       time.Time              `bun:"installed_at"`
		Customizations    map[string]interface{} `bun:"customizations,type:jsonb"`
		ExtractionPrompts json.RawMessage        `bun:"extraction_prompts,type:jsonb"`
	}

	err := r.db.NewRaw(`
		SELECT ptp.id, ptp.schema_id, gtp.name, gtp.version, gtp.description,
			   ptp.active, ptp.installed_at, ptp.customizations, gtp.extraction_prompts
		FROM kb.project_schemas ptp
		JOIN kb.graph_schemas gtp ON gtp.id = ptp.schema_id
		WHERE ptp.project_id = ?
		  AND ptp.removed_at IS NULL
		ORDER BY ptp.installed_at DESC
	`, projectID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to get installed packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	packs := make([]InstalledSchemaItem, len(results))
	for i, r := range results {
		packs[i] = InstalledSchemaItem{
			ID:                r.ID,
			SchemaID:          r.SchemaID,
			Name:              r.Name,
			Version:           r.Version,
			Description:       r.Description,
			Active:            r.Active,
			InstalledAt:       r.InstalledAt,
			Customizations:    r.Customizations,
			ExtractionPrompts: r.ExtractionPrompts,
		}
	}
	return packs, nil
}

// GetAssignmentHistory returns all schema assignments for a project (including removed ones).
func (r *Repository) GetAssignmentHistory(ctx context.Context, projectID string) ([]SchemaHistoryItem, error) {
	var results []struct {
		ID          string     `bun:"id"`
		SchemaID    string     `bun:"schema_id"`
		Name        string     `bun:"name"`
		Version     string     `bun:"version"`
		Active      bool       `bun:"active"`
		InstalledAt time.Time  `bun:"installed_at"`
		RemovedAt   *time.Time `bun:"removed_at"`
	}

	err := r.db.NewRaw(`
		SELECT ptp.id, ptp.schema_id, gtp.name, gtp.version, ptp.active,
			   ptp.installed_at, ptp.removed_at
		FROM kb.project_schemas ptp
		JOIN kb.graph_schemas gtp ON gtp.id = ptp.schema_id
		WHERE ptp.project_id = ?
		ORDER BY ptp.installed_at DESC
	`, projectID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to get assignment history", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	items := make([]SchemaHistoryItem, len(results))
	for i, r := range results {
		items[i] = SchemaHistoryItem{
			ID:          r.ID,
			SchemaID:    r.SchemaID,
			Name:        r.Name,
			Version:     r.Version,
			Active:      r.Active,
			InstalledAt: r.InstalledAt,
			RemovedAt:   r.RemovedAt,
		}
	}
	return items, nil
}

// MigrateTypes renames object/edge types and/or property keys across live graph data.
// When req.DryRun is true the transaction is rolled back after counting affected rows.
func (r *Repository) MigrateTypes(ctx context.Context, projectID string, req *MigrateRequest) (*MigrateResponse, error) {
	resp := &MigrateResponse{DryRun: req.DryRun}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Process type renames
		for _, tr := range req.TypeRenames {
			// Update kb.graph_objects.type
			var objCount int
			err := tx.NewRaw(`
				WITH updated AS (
					UPDATE kb.graph_objects
					SET type = ?, updated_at = NOW()
					WHERE project_id = ? AND type = ?
					RETURNING 1
				) SELECT COUNT(*) FROM updated
			`, tr.To, projectID, tr.From).Scan(ctx, &objCount)
			if err != nil {
				return fmt.Errorf("rename type %s objects: %w", tr.From, err)
			}

			// Update kb.graph_relationships.type
			var edgeCount int
			err = tx.NewRaw(`
				WITH updated AS (
					UPDATE kb.graph_relationships
					SET type = ?
					WHERE project_id = ? AND type = ?
					RETURNING 1
				) SELECT COUNT(*) FROM updated
			`, tr.To, projectID, tr.From).Scan(ctx, &edgeCount)
			if err != nil {
				return fmt.Errorf("rename type %s edges: %w", tr.From, err)
			}

			resp.TypeRenameResults = append(resp.TypeRenameResults, TypeRenameResult{
				From:            tr.From,
				To:              tr.To,
				ObjectsAffected: objCount,
				EdgesAffected:   edgeCount,
			})
		}

		// Process property renames
		for _, pr := range req.PropertyRenames {
			var objCount int
			err := tx.NewRaw(`
				WITH updated AS (
					UPDATE kb.graph_objects
					SET properties = (properties - ?) || jsonb_build_object(?, properties->?),
					    updated_at = NOW()
					WHERE project_id = ? AND type = ? AND properties->? IS NOT NULL
					RETURNING 1
				) SELECT COUNT(*) FROM updated
			`, pr.From, pr.To, pr.From, projectID, pr.TypeName, pr.From).Scan(ctx, &objCount)
			if err != nil {
				return fmt.Errorf("rename property %s.%s: %w", pr.TypeName, pr.From, err)
			}

			resp.PropertyRenameResults = append(resp.PropertyRenameResults, PropertyRenameResult{
				TypeName:        pr.TypeName,
				From:            pr.From,
				To:              pr.To,
				ObjectsAffected: objCount,
			})
		}

		if req.DryRun {
			return fmt.Errorf("dry_run_rollback")
		}
		return nil
	})

	if err != nil && err.Error() == "dry_run_rollback" {
		return resp, nil
	}
	if err != nil {
		r.log.Error("failed to migrate types", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return resp, nil
}

// AssignPack assigns a schema to a project
func (r *Repository) AssignPack(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*ProjectMemorySchema, error) {
	// Check if schema exists
	packExists, err := r.db.NewSelect().
		Model((*GraphMemorySchema)(nil)).
		Where("id = ?", req.SchemaID).
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check schema existence", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if !packExists {
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}

	// Check if already assigned (only non-removed assignments count)
	exists, err := r.db.NewSelect().
		Model((*ProjectMemorySchema)(nil)).
		Where("project_id = ?", projectID).
		Where("schema_id = ?", req.SchemaID).
		Where("removed_at IS NULL").
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check pack assignment", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if exists {
		return nil, apperror.ErrBadRequest.WithMessage("schema already assigned to project")
	}

	assignment := &ProjectMemorySchema{
		ProjectID:   projectID,
		SchemaID:    req.SchemaID,
		Active:      true,
		InstalledAt: time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err = r.db.NewInsert().Model(assignment).Returning("id").Exec(ctx)
	if err != nil {
		r.log.Error("failed to assign pack", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return assignment, nil
}

// parseObjectTypeSchemas parses objectTypeSchemas JSON (array or map format) into
// a slice of ObjectTypeSchema, setting SchemaID/Name/Version on each entry.
// It delegates format detection to parseObjectTypeSchemasToMap, then extracts the
// label/description fields from each entry's raw JSON into the typed struct.
func parseObjectTypeSchemas(data json.RawMessage, packID, packName, packVersion string) []ObjectTypeSchema {
	typeMap := parseObjectTypeSchemasToMap(data)
	if typeMap == nil {
		return nil
	}

	result := make([]ObjectTypeSchema, 0, len(typeMap))
	for typeName, raw := range typeMap {
		var def struct {
			Label       string          `json:"label"`
			Description string          `json:"description"`
			Properties  json.RawMessage `json:"properties"`
			UI          json.RawMessage `json:"ui"`
		}
		_ = json.Unmarshal(raw, &def)
		if isNullJSON(def.UI) {
			def.UI = nil
		}
		result = append(result, ObjectTypeSchema{
			Name:          typeName,
			Label:         def.Label,
			Description:   def.Description,
			Properties:    def.Properties,
			UI:            def.UI,
			SchemaID:      packID,
			SchemaName:    packName,
			SchemaVersion: packVersion,
		})
	}
	return result
}

// parseObjectTypeSchemasToMap converts the stored objectTypeSchemas JSON into a
// map of typeName → raw JSON schema, supporting both storage formats:
//
//   - Array format (user files): [{name, label, description, properties, ...}, ...]
//     The "properties" sub-object becomes the registered json_schema for each type.
//
//   - Map format (blueprint seeds): {typeName: {label, description, properties, ...}, ...}
func parseObjectTypeSchemasToMap(data json.RawMessage) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}

	// Try array format first (natural user file format).
	var arr []struct {
		Name        string          `json:"name"`
		Label       string          `json:"label"`
		Description string          `json:"description"`
		Properties  json.RawMessage `json:"properties"`
		UI          json.RawMessage `json:"ui"`
	}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		result := make(map[string]json.RawMessage, len(arr))
		for _, item := range arr {
			if item.Name == "" {
				continue
			}
			// Reconstruct a JSON Schema-style object for this type so that
			// mergeSchemas and the registry can work with it uniformly.
			schema := map[string]json.RawMessage{}
			if len(item.Properties) > 0 {
				schema["properties"] = item.Properties
			}
			if len(item.UI) > 0 && !isNullJSON(item.UI) {
				schema["ui"] = item.UI
			}
			if item.Label != "" {
				lb, _ := json.Marshal(item.Label)
				schema["label"] = lb
			}
			if item.Description != "" {
				desc, _ := json.Marshal(item.Description)
				schema["description"] = desc
			}
			schemaBytes, err := json.Marshal(schema)
			if err != nil {
				continue
			}
			result[item.Name] = schemaBytes
		}
		if len(result) > 0 {
			return result
		}
	}

	// Fall back to map format (blueprint seeds).
	var objMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objMap); err == nil && len(objMap) > 0 {
		return objMap
	}

	return nil
}

// parseRelationshipTypeSchemas parses relationship_type_schemas JSON which may
// be stored as either a JSON object (map of name → definition) or a JSON array.
// It normalises the various source/target field naming conventions into the
// canonical SourceType / TargetType (singular) compiled output.
func parseRelationshipTypeSchemas(data json.RawMessage, packID, packName, packVersion string) []RelationshipTypeSchema {
	// Try JSON object format first (most common in seeds)
	var objMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objMap); err == nil && len(objMap) > 0 {
		var result []RelationshipTypeSchema
		for name, raw := range objMap {
			var def relTypeRaw
			if err := json.Unmarshal(raw, &def); err != nil {
				continue
			}
			result = append(result, RelationshipTypeSchema{
				Name:          name,
				Label:         def.Label,
				Description:   def.Description,
				SourceType:    firstNonEmpty(joinTypes(def.SourceTypes), joinTypes(def.FromTypes), joinTypes(def.SnakeSourceTypes), def.Source),
				TargetType:    firstNonEmpty(joinTypes(def.TargetTypes), joinTypes(def.ToTypes), joinTypes(def.SnakeTargetTypes), def.Target),
				SchemaID:      packID,
				SchemaName:    packName,
				SchemaVersion: packVersion,
			})
		}
		return result
	}

	// Try JSON array format
	var arr []RelationshipTypeSchema
	if err := json.Unmarshal(data, &arr); err == nil {
		for i := range arr {
			arr[i].SchemaID = packID
			arr[i].SchemaName = packName
			arr[i].SchemaVersion = packVersion
		}
		return arr
	}

	return nil
}

// relTypeRaw captures all known field-name variants for relationship type schemas.
type relTypeRaw struct {
	Label            string   `json:"label"`
	Description      string   `json:"description"`
	SourceTypes      []string `json:"sourceTypes"`
	TargetTypes      []string `json:"targetTypes"`
	FromTypes        []string `json:"fromTypes"`
	ToTypes          []string `json:"toTypes"`
	Source           string   `json:"source"`
	Target           string   `json:"target"`
	SnakeSourceTypes []string `json:"source_types"`
	SnakeTargetTypes []string `json:"target_types"`
}

// joinTypes returns the first element of a string slice, or empty string.
// Compiled output uses singular SourceType/TargetType so we take the first.
func joinTypes(types []string) string {
	if len(types) > 0 {
		return types[0]
	}
	return ""
}

// firstNonEmpty returns the first non-empty string argument.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// UpdateAssignment updates a pack assignment (e.g., active status)
func (r *Repository) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) error {
	q := r.db.NewUpdate().
		Model((*ProjectMemorySchema)(nil)).
		Where("id = ?", assignmentID).
		Where("project_id = ?", projectID).
		Set("updated_at = ?", time.Now())

	if req.Active != nil {
		q = q.Set("active = ?", *req.Active)
	}

	result, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to update assignment", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("assignment not found")
	}

	return nil
}

// DeleteAssignment soft-deletes a pack assignment from a project by setting removed_at.
func (r *Repository) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	result, err := r.db.NewUpdate().
		Model((*ProjectMemorySchema)(nil)).
		Set("removed_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", assignmentID).
		Where("project_id = ?", projectID).
		Where("removed_at IS NULL").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to soft-delete assignment", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("assignment not found")
	}

	return nil
}

// GetActiveAssignment returns the active (non-removed) pack assignment for a
// project and schema, or nil when no active assignment exists.
func (r *Repository) GetActiveAssignment(ctx context.Context, projectID, schemaID string) (*ProjectMemorySchema, error) {
	var a ProjectMemorySchema
	err := r.db.NewSelect().
		Model(&a).
		Where("project_id = ?", projectID).
		Where("schema_id = ?", schemaID).
		Where("removed_at IS NULL").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get active assignment", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &a, nil
}

// DeleteAssignmentIfOwned soft-deletes a pack assignment only when this
// blueprint is the effective owner and no other applied blueprint still claims
// the pack. Returns true when the assignment was removed, false when left in
// place (manual, shared, or already removed).
func (r *Repository) DeleteAssignmentIfOwned(ctx context.Context, projectID, schemaID, blueprintID string) (bool, error) {
	res, err := r.db.NewRaw(`
		UPDATE kb.project_schemas ps
		SET removed_at = now(), updated_at = now()
		WHERE ps.project_id = ? AND ps.schema_id = ? AND ps.removed_at IS NULL
		  AND ps.source_blueprint_id IS NOT NULL
		  AND (
		      ps.source_blueprint_id = ?
		      OR NOT EXISTS (
		          SELECT 1 FROM kb.blueprint_applications bpa
		          WHERE bpa.project_id = ps.project_id
		            AND bpa.blueprint_id = ps.source_blueprint_id
		            AND bpa.status = 'applied'
		      )
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.blueprint_pack_claims bpc
		      WHERE bpc.project_id = ps.project_id
		        AND bpc.schema_id = ps.schema_id
		  )
	`, projectID, schemaID, blueprintID).Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete assignment if owned", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CreatePack creates a new schema scoped to the given project
func (r *Repository) CreatePack(ctx context.Context, projectID string, req *CreatePackRequest) (*GraphMemorySchema, error) {
	objectTypeSchemas := req.GetObjectTypeSchemas()
	relationshipTypeSchemas := req.GetRelationshipTypeSchemas()
	uiConfigs := req.GetUIConfigs()
	extractionPrompts := req.GetExtractionPrompts()

	// Compute checksum from schemas
	checksumContent := map[string]json.RawMessage{
		"object_type_schemas":       objectTypeSchemas,
		"relationship_type_schemas": relationshipTypeSchemas,
		"ui_configs":                uiConfigs,
		"extraction_prompts":        extractionPrompts,
	}
	checksumBytes, _ := json.Marshal(checksumContent)
	checksumHash := md5.Sum(checksumBytes)
	checksum := hex.EncodeToString(checksumHash[:])

	source := "manual"
	now := time.Now()

	pack := &GraphMemorySchema{
		Name:                    req.Name,
		Version:                 req.Version,
		Description:             req.Description,
		Author:                  req.Author,
		Source:                  &source,
		License:                 req.License,
		RepositoryURL:           req.RepositoryURL,
		DocumentationURL:        req.DocumentationURL,
		ObjectTypeSchemas:       objectTypeSchemas,
		RelationshipTypeSchemas: relationshipTypeSchemas,
		UIConfigs:               uiConfigs,
		ExtractionPrompts:       extractionPrompts,
		Migrations:              req.Migrations,
		Checksum:                &checksum,
		ProjectID:               &projectID,
		Draft:                   false,
		PublishedAt:             &now,
		CreatedAt:               now,
		UpdatedAt:               now,
	}

	_, err := r.db.NewInsert().Model(pack).Returning("id, created_at, updated_at, published_at").Exec(ctx)
	if err != nil {
		r.log.Error("failed to create schema", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return pack, nil
}

// GetPack returns a schema by ID if the caller owns it (same project)
func (r *Repository) GetPack(ctx context.Context, packID, projectID string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", packID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get schema", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}
	return &pack, nil
}

// UpdatePack partially updates a schema the caller owns.
// Only non-nil / non-empty fields in req are applied.
func (r *Repository) UpdatePack(ctx context.Context, packID, projectID string, req *UpdatePackRequest) (*GraphMemorySchema, error) {
	// Fetch current record with ownership check
	var pack GraphMemorySchema
	err := r.db.NewSelect().Model(&pack).Where("id = ?", packID).Where("project_id = ?", projectID).Scan(ctx)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}

	q := r.db.NewUpdate().Model(&pack).Where("id = ?", packID).Set("updated_at = ?", time.Now())

	if req.Name != nil {
		pack.Name = *req.Name
		q = q.Set("name = ?", *req.Name)
	}
	if req.Version != nil {
		pack.Version = *req.Version
		q = q.Set("version = ?", *req.Version)
	}
	if req.Description != nil {
		pack.Description = req.Description
		q = q.Set("description = ?", *req.Description)
	}
	if req.Author != nil {
		pack.Author = req.Author
		q = q.Set("author = ?", *req.Author)
	}
	if req.License != nil {
		pack.License = req.License
		q = q.Set("license = ?", *req.License)
	}
	if req.RepositoryURL != nil {
		pack.RepositoryURL = req.RepositoryURL
		q = q.Set("repository_url = ?", *req.RepositoryURL)
	}
	if req.DocumentationURL != nil {
		pack.DocumentationURL = req.DocumentationURL
		q = q.Set("documentation_url = ?", *req.DocumentationURL)
	}
	if len(req.ObjectTypeSchemas) > 0 {
		pack.ObjectTypeSchemas = req.ObjectTypeSchemas
		q = q.Set("object_type_schemas = ?", req.ObjectTypeSchemas)
	}
	if len(req.RelationshipTypeSchemas) > 0 {
		pack.RelationshipTypeSchemas = req.RelationshipTypeSchemas
		q = q.Set("relationship_type_schemas = ?", req.RelationshipTypeSchemas)
	}
	if len(req.UIConfigs) > 0 {
		pack.UIConfigs = req.UIConfigs
		q = q.Set("ui_configs = ?", req.UIConfigs)
	}
	if len(req.ExtractionPrompts) > 0 {
		pack.ExtractionPrompts = req.ExtractionPrompts
		q = q.Set("extraction_prompts = ?", req.ExtractionPrompts)
	}
	if req.Migrations != nil {
		pack.Migrations = req.Migrations
		q = q.Set("migrations = ?", req.Migrations)
	}

	if _, err := q.Returning("updated_at").Exec(ctx); err != nil {
		r.log.Error("failed to update schema", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &pack, nil
}

// DeletePack deletes a schema the caller owns from the registry.
// Returns an error if the pack is assigned to any projects.
func (r *Repository) DeletePack(ctx context.Context, packID, projectID string) error {
	// Check if assigned to any projects
	assignedCount, err := r.db.NewSelect().
		Model((*ProjectMemorySchema)(nil)).
		Where("schema_id = ?", packID).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to check pack assignments", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	if assignedCount > 0 {
		return apperror.ErrBadRequest.WithMessage("cannot delete schema that is assigned to projects")
	}

	result, err := r.db.NewDelete().
		Model((*GraphMemorySchema)(nil)).
		Where("id = ?", packID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete schema", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("schema not found")
	}

	return nil
}

// typeAction is a precomputed registry write for a single object type. It is
// shared by AssignPackWithTypes and RestoreTypeRegistryTx so the install/merge
// SQL lives in exactly one place.
type typeAction struct {
	name           string
	incomingSchema json.RawMessage
	action         string // "install", "skip", "merge", or "restore"
	conflict       *SchemaConflict
	mergedSchema   json.RawMessage
	// ownedSchemaIDs scopes a "restore" action to registry rows owned by one of
	// these schema IDs, so types installed by unrelated packs are left alone.
	ownedSchemaIDs []string
}

// nullableUUID renders an empty user ID as SQL NULL so inserts into a uuid
// column succeed when no user is available (e.g. a programmatic rollback).
func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// AssignPackWithTypes assigns a schema to a project AND populates the type registry.
// When req.DryRun is true, no database changes are made — only the preview is returned.
// When req.Merge is true, incoming type schemas are additively merged into existing types
// instead of being silently skipped.
func (r *Repository) AssignPackWithTypes(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*AssignPackResult, error) {
	// Get the full schema
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", req.SchemaID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get schema", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}

	// Check if already assigned (only consider active/non-removed assignments)
	var existingAssignment ProjectMemorySchema
	alreadyAssigned := false
	err = r.db.NewSelect().
		Model(&existingAssignment).
		Where("project_id = ?", projectID).
		Where("schema_id = ?", req.SchemaID).
		Where("removed_at IS NULL").
		Scan(ctx)
	if err == nil {
		alreadyAssigned = true
	}

	if alreadyAssigned && !req.Merge && !req.DryRun {
		return nil, apperror.ErrBadRequest.WithMessage("schema already assigned to project")
	}

	// Look for a soft-deleted assignment to reactivate (re-assign after unapply
	// must update the existing row rather than insert, since the (project_id,
	// schema_id) unique constraint does not cover removed rows).
	var softDeleted ProjectMemorySchema
	hasSoftDeleted := false
	if !alreadyAssigned {
		err = r.db.NewSelect().
			Model(&softDeleted).
			Where("project_id = ?", projectID).
			Where("schema_id = ?", req.SchemaID).
			Where("removed_at IS NOT NULL").
			Order("updated_at DESC").
			Limit(1).
			Scan(ctx)
		if err == nil {
			hasSoftDeleted = true
		}
	}

	// Parse object type schemas.
	// Schemas stored from user files use an array of {name, label, properties, ...}.
	// Schemas stored from blueprint seeds use a map of name → definition.
	// Both formats are supported here.
	objectTypeSchemas := parseObjectTypeSchemasToMap(pack.ObjectTypeSchemas)
	if objectTypeSchemas == nil && len(pack.ObjectTypeSchemas) > 0 {
		r.log.Warn("failed to parse object type schemas for type registration",
			slog.String("packId", pack.ID))
	}

	// Parse ui_configs and extraction_prompts
	var uiConfigs map[string]json.RawMessage
	if len(pack.UIConfigs) > 0 {
		if err := json.Unmarshal(pack.UIConfigs, &uiConfigs); err != nil {
			r.log.Warn("failed to parse ui_configs", logger.Error(err))
		}
	}
	var extractionPrompts map[string]json.RawMessage
	if len(pack.ExtractionPrompts) > 0 {
		if err := json.Unmarshal(pack.ExtractionPrompts, &extractionPrompts); err != nil {
			r.log.Warn("failed to parse extraction_prompts", logger.Error(err))
		}
	}

	// Build result scaffold
	result := &AssignPackResult{
		DryRun:           req.DryRun,
		SchemaID:         pack.ID,
		SchemaName:       pack.Name,
		InstalledTypes:   []string{},
		SkippedTypes:     []string{},
		MergedTypes:      []string{},
		Conflicts:        []SchemaConflict{},
		AlreadyInstalled: alreadyAssigned,
	}

	// If already installed and merge=true and not dry-run, return early with the existing assignment.
	if alreadyAssigned && req.Merge && !req.DryRun {
		result.AssignmentID = existingAssignment.ID
		return result, nil
	}

	// Pre-compute per-type actions: new / skip / merge
	var actions []typeAction

	for typeName, incomingSchema := range objectTypeSchemas {
		// Read existing registry entry (if any)
		var existingJSON string
		scanErr := r.db.NewRaw(`
			SELECT json_schema FROM kb.project_object_schema_registry
			WHERE project_id = ? AND type_name = ?
		`, projectID, typeName).Scan(ctx, &existingJSON)

		typeExists := scanErr == nil

		if !typeExists {
			actions = append(actions, typeAction{
				name:           typeName,
				incomingSchema: incomingSchema,
				action:         "install",
			})
			continue
		}

		// Type exists — compute conflict diff regardless (needed for dry-run + merge output)
		existing := json.RawMessage(existingJSON)
		merged, added, propConflicts, mergeErr := mergeSchemas(existing, incomingSchema)
		conflict := SchemaConflict{
			TypeName:              typeName,
			ExistingSchema:        existing,
			IncomingSchema:        incomingSchema,
			ConflictingProperties: propConflicts,
			AddedProperties:       added,
		}
		if mergeErr == nil && (req.Merge || req.DryRun) {
			conflict.MergedSchema = merged
		}

		if req.Merge {
			actions = append(actions, typeAction{
				name:           typeName,
				incomingSchema: incomingSchema,
				action:         "merge",
				conflict:       &conflict,
				mergedSchema:   merged,
			})
		} else {
			actions = append(actions, typeAction{
				name:           typeName,
				incomingSchema: incomingSchema,
				action:         "skip",
				conflict:       &conflict,
			})
		}
	}

	// Populate result summary from actions
	for _, a := range actions {
		switch a.action {
		case "install":
			result.InstalledTypes = append(result.InstalledTypes, a.name)
		case "skip":
			result.SkippedTypes = append(result.SkippedTypes, a.name)
			result.Conflicts = append(result.Conflicts, *a.conflict)
		case "merge":
			result.MergedTypes = append(result.MergedTypes, a.name)
			result.Conflicts = append(result.Conflicts, *a.conflict)
		}
	}

	// Dry-run: return preview without touching the database
	if req.DryRun {
		return result, nil
	}

	// Execute in a transaction
	now := time.Now()
	assignment := &ProjectMemorySchema{
		ProjectID:         projectID,
		SchemaID:          req.SchemaID,
		Active:            true,
		InstalledAt:       now,
		SourceBlueprintID: req.SourceBlueprintID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		switch {
		case alreadyAssigned:
			assignment.ID = existingAssignment.ID
		case hasSoftDeleted:
			if _, err := tx.NewUpdate().
				Model((*ProjectMemorySchema)(nil)).
				Set("removed_at = ?", nil).
				Set("active = ?", true).
				Set("installed_at = ?", now).
				Set("updated_at = ?", now).
				Set("source_blueprint_id = ?", req.SourceBlueprintID).
				Where("id = ?", softDeleted.ID).
				Exec(ctx); err != nil {
				return err
			}
			assignment.ID = softDeleted.ID
		default:
			if _, err := tx.NewInsert().Model(assignment).Returning("id").Exec(ctx); err != nil {
				return err
			}
		}

		return r.assignPackWithTypesTx(ctx, tx, projectID, userID, req.SchemaID, actions, uiConfigs, extractionPrompts, now)
	})

	if err != nil {
		r.log.Error("failed to assign pack with types", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	result.AssignmentID = assignment.ID
	return result, nil
}

// assignPackWithTypesTx applies precomputed per-type registry actions inside an
// existing transaction. AssignPackWithTypes and RestoreTypeRegistryTx share it
// so install/merge/restore semantics live in one place. Callers own the
// surrounding transaction; this never opens a nested one.
func (r *Repository) assignPackWithTypesTx(
	ctx context.Context,
	tx bun.Tx,
	projectID, userID, schemaID string,
	actions []typeAction,
	uiConfigs, extractionPrompts map[string]json.RawMessage,
	now time.Time,
) error {
	for _, a := range actions {
		uiConfigJSON := "{}"
		if uiConfigs != nil {
			if cfg, ok := uiConfigs[a.name]; ok {
				uiConfigJSON = string(cfg)
			}
		}
		extractionConfigJSON := "{}"
		if extractionPrompts != nil {
			if cfg, ok := extractionPrompts[a.name]; ok {
				extractionConfigJSON = string(cfg)
			}
		}

		switch a.action {
		case "install":
			_, err := tx.NewRaw(`
				INSERT INTO kb.project_object_schema_registry
				(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled, created_by)
				VALUES (?, ?, 'template', ?, ?, ?, ?, true, ?)
			`, projectID, a.name, schemaID, string(a.incomingSchema), uiConfigJSON, extractionConfigJSON, nullableUUID(userID)).Exec(ctx)
			if err != nil {
				return err
			}

		case "merge":
			if len(a.mergedSchema) > 0 {
				_, err := tx.NewRaw(`
					UPDATE kb.project_object_schema_registry
					SET json_schema = ?, updated_at = ?
					WHERE project_id = ? AND type_name = ?
				`, string(a.mergedSchema), now, projectID, a.name).Exec(ctx)
				if err != nil {
					return err
				}
			}

		case "restore":
			// Rewrite the row only when it belongs to one of the migration's
			// schemas; a type installed by an unrelated pack must not be
			// clobbered.
			updated, err := restoreOwnedRegistryRowTx(ctx, tx, projectID, schemaID, a, uiConfigJSON, extractionConfigJSON, now)
			if err != nil {
				return err
			}
			if updated {
				continue
			}
			// No owned row exists. Insert only when the type is not registered
			// at all, so restoring never creates a duplicate type_name row or
			// shadows another pack's type.
			var count int
			if err := tx.NewRaw(`
				SELECT COUNT(*) FROM kb.project_object_schema_registry
				WHERE project_id = ? AND type_name = ?
			`, projectID, a.name).Scan(ctx, &count); err != nil {
				return err
			}
			if count == 0 {
				_, err := tx.NewRaw(`
					INSERT INTO kb.project_object_schema_registry
					(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled, created_by)
					VALUES (?, ?, 'template', ?, ?, ?, ?, true, ?)
				`, projectID, a.name, schemaID, string(a.incomingSchema), uiConfigJSON, extractionConfigJSON, nullableUUID(userID)).Exec(ctx)
				if err != nil {
					return err
				}
			}

		case "skip":
			r.log.Info("type already registered, skipping",
				slog.String("typeName", a.name),
				slog.String("projectId", projectID))
		}
	}
	return nil
}

// restoreOwnedRegistryRowTx updates the registry row for a.name when it is
// owned by one of the action's schemas. Returns true when a row was rewritten.
func restoreOwnedRegistryRowTx(
	ctx context.Context,
	tx bun.Tx,
	projectID, schemaID string,
	a typeAction,
	uiConfigJSON, extractionConfigJSON string,
	now time.Time,
) (bool, error) {
	ids := make([]string, 0, len(a.ownedSchemaIDs))
	for _, id := range a.ownedSchemaIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	res, err := tx.NewRaw(`
		UPDATE kb.project_object_schema_registry
		SET json_schema = ?,
		    ui_config = ?,
		    extraction_config = ?,
		    schema_id = ?,
		    enabled = true,
		    updated_at = ?
		WHERE project_id = ?
		  AND type_name = ?
		  AND schema_id IN (?)
	`, string(a.incomingSchema), uiConfigJSON, extractionConfigJSON, schemaID, now, projectID, a.name, bun.In(ids)).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RestoreTypeRegistryTx restores the project's type registry to the from-pack
// state for the types owned by this from/to schema pair.
//
// Reconciliation is driven by schema ownership, not by name alone:
//
//  1. Every template registry row owned by FROM or TO (schema_id IN
//     {from.ID, to.ID}) whose type_name is not part of the from-pack is
//     deleted. This removes to-only additions AND rows left behind by an
//     in-place type rename, where MigrateTypes changes the row's type_name
//     while leaving its schema_id pointing at the original owner. The old
//     name+to-pack-ID predicate could not see such a row: the from-pack
//     restore looked it up under the old name, and the row's (renamed) name
//     was not in the to-only set.
//  2. The from-pack's types are (re)written via the shared restore writer,
//     adopting any owned row that already carries the from name and inserting
//     only when the name is otherwise unregistered.
//
// It is transaction-aware: the caller MUST invoke it inside the same
// transaction as the property-data restore so the rollback is atomic — this
// method never opens its own transaction. Rows owned by other packs/schemas
// are never modified or removed.
func (r *Repository) RestoreTypeRegistryTx(ctx context.Context, tx bun.Tx, projectID, userID string, fromPack, toPack *GraphMemorySchema) error {
	if fromPack == nil || toPack == nil {
		return fmt.Errorf("from and to packs are required")
	}

	now := time.Now()

	actions, fromTypeNames := buildRestoreTypeRegistryPlan(fromPack, toPack)

	var uiConfigs map[string]json.RawMessage
	if len(fromPack.UIConfigs) > 0 {
		if err := json.Unmarshal(fromPack.UIConfigs, &uiConfigs); err != nil {
			r.log.Warn("failed to parse ui_configs during registry restore", logger.Error(err))
		}
	}
	var extractionPrompts map[string]json.RawMessage
	if len(fromPack.ExtractionPrompts) > 0 {
		if err := json.Unmarshal(fromPack.ExtractionPrompts, &extractionPrompts); err != nil {
			r.log.Warn("failed to parse extraction_prompts during registry restore", logger.Error(err))
		}
	}

	// Step 1: remove owned rows that are not part of the from-pack. Done before
	// the restore so a renamed row cannot linger alongside the re-created
	// original.
	if err := deleteOwnedRegistryRowsTx(ctx, tx, projectID, fromPack.ID, toPack.ID, fromTypeNames); err != nil {
		return fmt.Errorf("remove superseded registry rows: %w", err)
	}

	// Step 2: (re)write the from-pack's types. Rows owned by other packs that
	// share a name are never touched (the restore writer is scoped to
	// ownedSchemaIDs) and prevent an unsafe duplicate insert.
	if err := r.assignPackWithTypesTx(ctx, tx, projectID, userID, fromPack.ID, actions, uiConfigs, extractionPrompts, now); err != nil {
		return fmt.Errorf("restore from-pack types: %w", err)
	}

	return nil
}

// deleteOwnedRegistryRowsTx deletes template registry rows owned by either
// schema of the from/to pair whose type_name is not part of the from-pack.
// When fromTypeNames is empty every owned row is removed. Rows owned by other
// schemas are never touched.
func deleteOwnedRegistryRowsTx(ctx context.Context, tx bun.Tx, projectID, fromID, toID string, fromTypeNames []string) error {
	owned := bun.In([]string{fromID, toID})
	if len(fromTypeNames) == 0 {
		_, err := tx.NewRaw(`
			DELETE FROM kb.project_object_schema_registry
			WHERE project_id = ? AND schema_id IN (?) AND source = 'template'
		`, projectID, owned).Exec(ctx)
		return err
	}
	_, err := tx.NewRaw(`
		DELETE FROM kb.project_object_schema_registry
		WHERE project_id = ? AND schema_id IN (?) AND source = 'template' AND type_name NOT IN (?)
	`, projectID, owned, bun.In(fromTypeNames)).Exec(ctx)
	return err
}

// buildRestoreTypeRegistryPlan derives the registry writes needed to restore
// the from-pack state: one "restore" action per from-pack type, plus the list
// of from-pack type names that defines which owned rows survive the
// reconciliation delete. It is pure so the selection logic is unit-testable
// without a database.
func buildRestoreTypeRegistryPlan(fromPack, toPack *GraphMemorySchema) (actions []typeAction, fromTypeNames []string) {
	if fromPack == nil || toPack == nil {
		return nil, nil
	}
	fromTypes := parseObjectTypeSchemasToMap(fromPack.ObjectTypeSchemas)

	actions = make([]typeAction, 0, len(fromTypes))
	fromTypeNames = make([]string, 0, len(fromTypes))
	for name, schemaJSON := range fromTypes {
		actions = append(actions, typeAction{
			name:           name,
			incomingSchema: schemaJSON,
			action:         "restore",
			ownedSchemaIDs: []string{fromPack.ID, toPack.ID},
		})
		fromTypeNames = append(fromTypeNames, name)
	}
	return actions, fromTypeNames
}

// mergeSchemas additively merges incomingSchema properties into existingSchema.
// New properties from incoming are added; existing properties are never overwritten.
// Returns the merged schema, the list of added property names, and any property-level conflicts.
func mergeSchemas(existing, incoming json.RawMessage) (merged json.RawMessage, added []string, conflicts []PropertyConflict, err error) {
	var existingMap map[string]json.RawMessage
	var incomingMap map[string]json.RawMessage

	if err = json.Unmarshal(existing, &existingMap); err != nil {
		return nil, nil, nil, err
	}
	if err = json.Unmarshal(incoming, &incomingMap); err != nil {
		return nil, nil, nil, err
	}

	// Work on the "properties" sub-object if present (JSON Schema draft-07 style)
	existingProps, _ := extractProperties(existingMap)
	incomingProps, _ := extractProperties(incomingMap)

	mergedProps := make(map[string]json.RawMessage, len(existingProps))
	for k, v := range existingProps {
		mergedProps[k] = v
	}

	for propName, incomingDef := range incomingProps {
		if existingDef, exists := mergedProps[propName]; exists {
			// Conflict: same property name — existing wins
			conflicts = append(conflicts, PropertyConflict{
				Property:    propName,
				ExistingDef: existingDef,
				IncomingDef: incomingDef,
				Resolution:  "existing_wins",
			})
		} else {
			mergedProps[propName] = incomingDef
			added = append(added, propName)
		}
	}

	// Reconstruct merged schema
	mergedMap := make(map[string]json.RawMessage, len(existingMap))
	for k, v := range existingMap {
		mergedMap[k] = v
	}
	if len(mergedProps) > 0 {
		propsBytes, marshalErr := json.Marshal(mergedProps)
		if marshalErr != nil {
			return nil, nil, nil, marshalErr
		}
		mergedMap["properties"] = propsBytes
	}

	merged, err = json.Marshal(mergedMap)
	return merged, added, conflicts, err
}

// extractProperties pulls the "properties" key from a JSON Schema map.
// Returns nil map if key is absent or not an object.
func extractProperties(schemaMap map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	raw, ok := schemaMap["properties"]
	if !ok {
		return map[string]json.RawMessage{}, false
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(raw, &props); err != nil {
		return map[string]json.RawMessage{}, false
	}
	return props, true
}

// ---------------------------------------------------------------------------
// DB accessor (task 4.x prerequisite)
// ---------------------------------------------------------------------------

// DB returns the underlying database handle for use by cross-domain orchestrators.
func (r *Repository) DB() bun.IDB {
	return r.db
}

// ---------------------------------------------------------------------------
// Migration-aware pack accessors (tasks 4.1–4.2)
// ---------------------------------------------------------------------------

// GetPackByID returns a schema by ID without ownership checks.
// Used internally by migration orchestration where ownership is already verified.
func (r *Repository) GetPackByID(ctx context.Context, packID string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", packID).
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}
	return &pack, nil
}

// GetPackByNameVersion returns a schema by (name, version) from the global registry.
func (r *Repository) GetPackByNameVersion(ctx context.Context, name, version string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("name = ?", name).
		Where("version = ?", version).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}
	return &pack, nil
}

// SupersedeAssignments soft-deletes every other active assignment of the same
// pack name in the project, leaving only currentSchemaID active. Called when a
// newer version of a pack is installed (or a no-migration version bump) so the
// compiled schema view does not keep duplicated types from older versions.
// Mirror of blueprints.SupersedeApplications, at the schema-assignment level.
func (r *Repository) SupersedeAssignments(ctx context.Context, projectID, schemaName, currentSchemaID string) error {
	_, err := r.db.NewRaw(`
		UPDATE kb.project_schemas
		SET removed_at = NOW(), updated_at = NOW()
		WHERE project_id = ?
		  AND schema_id <> ?
		  AND removed_at IS NULL
		  AND schema_id IN (SELECT id FROM kb.graph_schemas WHERE name = ?)
	`, projectID, currentSchemaID, schemaName).Exec(ctx)
	if err != nil {
		r.log.Error("failed to supersede schema assignments", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// GetInstalledSchemasByName returns all active installed schema records for a
// project that match the given schema name (across all versions).
// Task 4.3.
func (r *Repository) GetInstalledSchemasByName(ctx context.Context, projectID, schemaName string) ([]GraphMemorySchema, error) {
	var packs []GraphMemorySchema
	// Use Model(&packs) so only the struct's mapped columns are selected —
	// graph_schemas has extra columns (e.g. discovery_job_id) not mapped on
	// GraphMemorySchema, which a SELECT gs.* would fail to scan.
	err := r.db.NewSelect().
		Model(&packs).
		Join("JOIN kb.project_schemas AS ps ON ps.schema_id = gtp.id").
		Where("ps.project_id = ?", projectID).
		Where("ps.removed_at IS NULL").
		Where("gtp.name = ?", schemaName).
		Order("ps.installed_at DESC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get installed schemas by name", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if packs == nil {
		return []GraphMemorySchema{}, nil
	}
	return packs, nil
}

// ---------------------------------------------------------------------------
// CreatePack / UpdatePack — Migrations column support (task 4.1)
// ---------------------------------------------------------------------------

// CreatePackWithMigrations creates a new schema including the optional Migrations JSONB column.
// This method is used internally; the public-facing CreatePack delegates here.
// NOTE: repository.CreatePack is updated to forward Migrations from the request.
// We achieve this by patching the pack struct after construction.

// ---------------------------------------------------------------------------
// Migration job CRUD (tasks 4.4–4.7)
// ---------------------------------------------------------------------------

// CreateMigrationJob inserts a new schema_migration_jobs record.
func (r *Repository) CreateMigrationJob(ctx context.Context, job *SchemaMigrationJob) error {
	chainJSON, err := json.Marshal(job.Chain)
	if err != nil {
		return fmt.Errorf("marshal chain: %w", err)
	}
	err = r.db.NewRaw(`
		INSERT INTO kb.schema_migration_jobs
		(project_id, from_schema_id, to_schema_id, chain, status, risk_level,
		 objects_migrated, objects_failed, auto_uninstall, created_at)
		VALUES (uuid(?), uuid(?), uuid(?), ?, ?, ?,
		        ?, ?, ?, ?)
		RETURNING id
	`, job.ProjectID, job.FromSchemaID, job.ToSchemaID, string(chainJSON),
		job.Status, job.RiskLevel,
		job.ObjectsMigrated, job.ObjectsFailed, job.AutoUninstall, job.CreatedAt).
		Scan(ctx, &job.ID)
	if err != nil {
		r.log.Error("failed to create migration job", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// GetMigrationJob returns a migration job by ID.
func (r *Repository) GetMigrationJob(ctx context.Context, jobID string) (*SchemaMigrationJob, error) {
	var job SchemaMigrationJob
	err := r.db.NewSelect().
		Model(&job).
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("migration job not found")
	}
	return &job, nil
}

// UpdateMigrationJob updates a migration job's mutable fields.
func (r *Repository) UpdateMigrationJob(ctx context.Context, job *SchemaMigrationJob) error {
	q := r.db.NewUpdate().
		Model(job).
		Where("id = ?", job.ID).
		Set("status = ?", job.Status).
		Set("objects_migrated = ?", job.ObjectsMigrated).
		Set("objects_failed = ?", job.ObjectsFailed)

	if job.Error != nil {
		q = q.Set("error = ?", *job.Error)
	}
	if job.StartedAt != nil {
		q = q.Set("started_at = ?", job.StartedAt)
	}
	if job.CompletedAt != nil {
		q = q.Set("completed_at = ?", job.CompletedAt)
	}
	if job.RiskLevel != "" {
		q = q.Set("risk_level = ?", job.RiskLevel)
	}

	_, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to update migration job", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ProvisionBuiltinSchemasToAllProjects installs every source='builtin' schema
// into every non-deleted project that lacks a project_schemas row for it.
// Idempotent; respects explicit uninstalls (any existing row, incl. removed).
// Runs at server startup outside any project/RLS context.
func (r *Repository) ProvisionBuiltinSchemasToAllProjects(ctx context.Context) error {
	_, err := r.db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		SELECT p.id, gs.id, true, now()
		FROM kb.projects p
		JOIN kb.graph_schemas gs ON gs.source = 'builtin'
		WHERE p.deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.project_schemas ps
		      WHERE ps.project_id = p.id AND ps.schema_id = gs.id
		  )
		ON CONFLICT (project_id, schema_id) DO NOTHING
	`).Exec(ctx)
	if err != nil {
		r.log.Error("failed to provision builtin schemas to projects", logger.Error(err))
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

// FindActiveMigrationJob returns the first pending or running migration job for
// the given project/from/to schema combination. Used for deduplication.
func (r *Repository) FindActiveMigrationJob(ctx context.Context, projectID, fromSchemaID, toSchemaID string) (*SchemaMigrationJob, error) {
	var job SchemaMigrationJob
	err := r.db.NewSelect().
		Model(&job).
		Where("project_id = ?", projectID).
		Where("from_schema_id = ?", fromSchemaID).
		Where("to_schema_id = ?", toSchemaID).
		Where("status IN (?)", bun.In([]string{"pending", "running"})).
		Order("created_at ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		// No active job found — not an error for the caller
		return nil, nil //nolint:nilerr
	}
	return &job, nil
}

// FindMigrationJobByToPackVersion returns the most recent migration job in the
// project whose target pack (kb.graph_schemas) carries the given human version.
// It returns (nil, nil) when no matching job exists.
//
// kb.schema_migration_jobs is the table that persists from_schema_id and
// to_schema_id. kb.schema_migration_runs stores human version strings instead,
// so it cannot resolve schema IDs on its own.
func (r *Repository) FindMigrationJobByToPackVersion(ctx context.Context, projectID, toVersion string) (*SchemaMigrationJob, error) {
	if toVersion == "" {
		return nil, nil
	}
	var job SchemaMigrationJob
	err := r.db.NewSelect().
		Model(&job).
		Join("JOIN kb.graph_schemas AS gs ON gs.id = smj.to_schema_id").
		Where("smj.project_id = ?", projectID).
		Where("gs.version = ?", toVersion).
		OrderExpr("(smj.status = 'completed') DESC").
		Order("smj.created_at DESC").
		Order("smj.id DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to find migration job by to-pack version", logger.Error(err))
		return nil, apperror.NewInternal("failed to find migration job by to-pack version", err)
	}
	return &job, nil
}

// FindMigrationRunFromVersion returns the from_version recorded by the most
// recent kb.schema_migration_runs row in the project whose to_version matches.
// It returns ("", nil) when no matching run exists.
func (r *Repository) FindMigrationRunFromVersion(ctx context.Context, projectID, toVersion string) (string, error) {
	if toVersion == "" {
		return "", nil
	}
	var fromVersion string
	err := r.db.NewRaw(`
		SELECT from_version
		FROM kb.schema_migration_runs
		WHERE project_id = ? AND to_version = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, projectID, toVersion).Scan(ctx, &fromVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		r.log.Error("failed to find migration run by to-version", logger.Error(err))
		return "", apperror.NewInternal("failed to find migration run by to-version", err)
	}
	return fromVersion, nil
}

// GetProjectPackByVersion returns an active pack assigned to the project whose
// human version matches. Used to resolve a migration's to-pack when only
// version-only run records are available.
func (r *Repository) GetProjectPackByVersion(ctx context.Context, projectID, version string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Join("JOIN kb.project_schemas AS ps ON ps.schema_id = gtp.id").
		Where("ps.project_id = ?", projectID).
		Where("ps.removed_at IS NULL").
		Where("gtp.version = ?", version).
		Order("ps.installed_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("pack version", version)
		}
		r.log.Error("failed to get project pack by version", logger.Error(err))
		return nil, apperror.NewInternal("failed to get project pack by version", err)
	}
	return &pack, nil
}
