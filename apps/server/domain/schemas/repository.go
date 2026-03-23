package schemas

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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

		// Parse object type schemas (supports both array and map storage formats)
		if len(tp.ObjectTypeSchemas) > 0 {
			objectTypes := parseObjectTypeSchemas(tp.ObjectTypeSchemas, tp.ID, tp.Name, tp.Version)
			if objectTypes == nil {
				r.log.Warn("failed to parse object type schemas",
					slog.String("packId", tp.ID))
			} else {
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

// GetAvailablePacks returns schemas available for a project to install.
// Only returns schemas owned by this project, or org-visible schemas from the same org.
func (r *Repository) GetAvailablePacks(ctx context.Context, projectID, orgID string) ([]MemorySchemaListItem, error) {
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

	// Get packs not installed for this project, scoped to project or org
	var packs []MemorySchemaListItem
	q := r.db.NewSelect().
		Model((*GraphMemorySchema)(nil)).
		Column("id", "name", "version", "description", "author", "visibility", "blueprint_source")

	if len(installedIDs) > 0 {
		q = q.Where("id NOT IN (?)", bun.In(installedIDs))
	}

	// Ownership filter: project-owned OR (same org AND org-visible)
	q = q.Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID)

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

// GetInstalledPacks returns schemas installed for a project
func (r *Repository) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledSchemaItem, error) {
	var results []struct {
		ID              string                 `bun:"id"`
		SchemaID        string                 `bun:"schema_id"`
		Name            string                 `bun:"name"`
		Version         string                 `bun:"version"`
		Description     *string                `bun:"description"`
		Active          bool                   `bun:"active"`
		InstalledAt     time.Time              `bun:"installed_at"`
		Customizations  map[string]interface{} `bun:"customizations,type:jsonb"`
		BlueprintSource *string                `bun:"blueprint_source"`
	}

	err := r.db.NewRaw(`
		SELECT ptp.id, ptp.schema_id, gtp.name, gtp.version, gtp.description,
			   ptp.active, ptp.installed_at, ptp.customizations, gtp.blueprint_source
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
			ID:              r.ID,
			SchemaID:        r.SchemaID,
			Name:            r.Name,
			Version:         r.Version,
			Description:     r.Description,
			Active:          r.Active,
			InstalledAt:     r.InstalledAt,
			Customizations:  r.Customizations,
			BlueprintSource: r.BlueprintSource,
		}
	}
	return packs, nil
}

// GetAllPacks returns all schemas for a project — both installed and available —
// as a unified list. Installed schemas have Installed=true and a populated
// AssignmentID / InstalledAt. Available (not installed) schemas have Installed=false.
func (r *Repository) GetAllPacks(ctx context.Context, projectID, orgID string) ([]UnifiedSchemaItem, error) {
	var results []struct {
		ID              string     `bun:"id"`
		Name            string     `bun:"name"`
		Version         string     `bun:"version"`
		Description     *string    `bun:"description"`
		Author          *string    `bun:"author"`
		Visibility      string     `bun:"visibility"`
		Installed       bool       `bun:"installed"`
		InstalledAt     *time.Time `bun:"installed_at"`
		AssignmentID    *string    `bun:"assignment_id"`
		BlueprintSource *string    `bun:"blueprint_source"`
	}

	err := r.db.NewRaw(`
		SELECT gtp.id,
		       ptp.schema_id,
		       gtp.name,
		       gtp.version,
		       gtp.description,
		       gtp.author,
		       gtp.visibility,
		       (ptp.id IS NOT NULL) AS installed,
		       ptp.installed_at,
		       ptp.id AS assignment_id,
		       gtp.blueprint_source
		FROM kb.graph_schemas gtp
		LEFT JOIN kb.project_schemas ptp
		       ON ptp.schema_id = gtp.id
		      AND ptp.project_id = ?
		      AND ptp.removed_at IS NULL
		WHERE (gtp.project_id = ? OR (gtp.org_id = ? AND gtp.visibility = 'organization'))
		ORDER BY gtp.name ASC, gtp.version ASC
	`, projectID, projectID, orgID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to get all packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	items := make([]UnifiedSchemaItem, len(results))
	for i, res := range results {
		items[i] = UnifiedSchemaItem{
			ID:              res.ID,
			Name:            res.Name,
			Version:         res.Version,
			Description:     res.Description,
			Author:          res.Author,
			Visibility:      res.Visibility,
			Installed:       res.Installed,
			InstalledAt:     res.InstalledAt,
			AssignmentID:    res.AssignmentID,
			BlueprintSource: res.BlueprintSource,
		}
	}
	return items, nil
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
					WHERE project_id = ? AND type = ? AND properties ? ?
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
		}
		_ = json.Unmarshal(raw, &def)
		result = append(result, ObjectTypeSchema{
			Name:          typeName,
			Label:         def.Label,
			Description:   def.Description,
			Properties:    def.Properties,
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

// CreatePack creates a new schema scoped to the given project and org
func (r *Repository) CreatePack(ctx context.Context, projectID, orgID string, req *CreatePackRequest) (*GraphMemorySchema, error) {
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
		BlueprintSource:         req.BlueprintSource,
		ProjectID:               &projectID,
		OrgID:                   &orgID,
		Visibility:              req.Visibility,
		Draft:                   false,
		PublishedAt:             &now,
		CreatedAt:               now,
		UpdatedAt:               now,
	}

	if pack.Visibility == "" {
		pack.Visibility = "project"
	}

	_, err := r.db.NewInsert().Model(pack).Returning("id, created_at, updated_at, published_at").Exec(ctx)
	if err != nil {
		r.log.Error("failed to create schema", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return pack, nil
}

// GetPack returns a schema by ID if the caller has access (same project, same org with org visibility, or legacy)
func (r *Repository) GetPack(ctx context.Context, packID, projectID, orgID string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", packID).
		Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get schema", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}
	return &pack, nil
}

// UpdatePack partially updates a schema the caller owns.
// Only non-nil / non-empty fields in req are applied.
func (r *Repository) UpdatePack(ctx context.Context, packID, projectID, orgID string, req *UpdatePackRequest) (*GraphMemorySchema, error) {
	// Fetch current record with ownership check
	var pack GraphMemorySchema
	err := r.db.NewSelect().Model(&pack).Where("id = ?", packID).Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID).Scan(ctx)
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
func (r *Repository) DeletePack(ctx context.Context, packID, projectID, orgID string) error {
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
		Where("(project_id = ? OR (org_id = ? AND visibility = 'organization'))", projectID, orgID).
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
	type typeAction struct {
		name           string
		incomingSchema json.RawMessage
		action         string // "install", "skip", "merge"
		conflict       *SchemaConflict
		mergedSchema   json.RawMessage
	}

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
		ProjectID:   projectID,
		SchemaID:    req.SchemaID,
		Active:      true,
		InstalledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if !alreadyAssigned {
			if _, err := tx.NewInsert().Model(assignment).Returning("id").Exec(ctx); err != nil {
				return err
			}
		} else {
			assignment.ID = existingAssignment.ID
		}

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
				`, projectID, a.name, req.SchemaID, string(a.incomingSchema), uiConfigJSON, extractionConfigJSON, userID).Exec(ctx)
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

			case "skip":
				r.log.Info("type already registered, skipping",
					slog.String("typeName", a.name),
					slog.String("projectId", projectID))
			}
		}

		return nil
	})

	if err != nil {
		r.log.Error("failed to assign pack with types", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	result.AssignmentID = assignment.ID
	return result, nil
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

// GetInstalledSchemasByName returns all active installed schema records for a
// project that match the given schema name (across all versions).
// Task 4.3.
func (r *Repository) GetInstalledSchemasByName(ctx context.Context, projectID, schemaName string) ([]GraphMemorySchema, error) {
	var packs []GraphMemorySchema
	err := r.db.NewRaw(`
		SELECT gs.*
		FROM kb.graph_schemas gs
		JOIN kb.project_schemas ps ON ps.schema_id = gs.id
		WHERE ps.project_id = ?
		  AND ps.removed_at IS NULL
		  AND gs.name = ?
		ORDER BY ps.installed_at DESC
	`, projectID, schemaName).Scan(ctx, &packs)
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
		 objects_migrated, objects_failed, created_at)
		VALUES (uuid(?), uuid(?), uuid(?), ?, ?, ?,
		        ?, ?, ?)
		RETURNING id
	`, job.ProjectID, job.FromSchemaID, job.ToSchemaID, string(chainJSON),
		job.Status, job.RiskLevel,
		job.ObjectsMigrated, job.ObjectsFailed, job.CreatedAt).
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
