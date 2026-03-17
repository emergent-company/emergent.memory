package schemas

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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
	// Get all active schemas for the project
	var projectPacks []ProjectMemorySchema
	err := r.db.NewSelect().
		Model(&projectPacks).
		Relation("MemorySchema").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get project schemas", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	response := &CompiledTypesResponse{
		ObjectTypes:       []ObjectTypeSchema{},
		RelationshipTypes: []RelationshipTypeSchema{},
	}

	// Compile types from all active packs
	for _, pp := range projectPacks {
		if pp.MemorySchema == nil {
			continue
		}

		tp := pp.MemorySchema

		// Parse object type schemas
		if len(tp.ObjectTypeSchemas) > 0 {
			var objectTypes []ObjectTypeSchema
			if err := json.Unmarshal(tp.ObjectTypeSchemas, &objectTypes); err != nil {
				r.log.Warn("failed to parse object type schemas",
					slog.String("packId", tp.ID),
					logger.Error(err))
			} else {
				// Add pack info to each type
				for i := range objectTypes {
					objectTypes[i].PackID = tp.ID
					objectTypes[i].PackName = tp.Name
				}
				response.ObjectTypes = append(response.ObjectTypes, objectTypes...)
			}
		}

		// Parse relationship type schemas
		// Seeds store these as a JSON object (map of name → definition), but
		// we also accept a JSON array for forward compat. Each definition may
		// use different field names for source/target types:
		//   sourceTypes / fromTypes / source / source_types  (and same for target)
		if len(tp.RelationshipTypeSchemas) > 0 {
			relTypes := parseRelationshipTypeSchemas(tp.RelationshipTypeSchemas, tp.ID, tp.Name)
			if relTypes == nil {
				r.log.Warn("failed to parse relationship type schemas",
					slog.String("packId", tp.ID))
			} else {
				response.RelationshipTypes = append(response.RelationshipTypes, relTypes...)
			}
		}
	}

	return response, nil
}

// GetAvailablePacks returns schemas available for a project to install
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

	// Get all packs not installed for this project
	var packs []MemorySchemaListItem
	q := r.db.NewSelect().
		Model((*GraphMemorySchema)(nil)).
		Column("id", "name", "version", "description", "author")

	if len(installedIDs) > 0 {
		q = q.Where("id NOT IN (?)", bun.In(installedIDs))
	}

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
		ID             string                 `bun:"id"`
		SchemaID       string                 `bun:"schema_id"`
		Name           string                 `bun:"name"`
		Version        string                 `bun:"version"`
		Description    *string                `bun:"description"`
		Active         bool                   `bun:"active"`
		InstalledAt    time.Time              `bun:"installed_at"`
		Customizations map[string]interface{} `bun:"customizations,type:jsonb"`
	}

	err := r.db.NewRaw(`
		SELECT ptp.id, ptp.schema_id, gtp.name, gtp.version, gtp.description,
			   ptp.active, ptp.installed_at, ptp.customizations
		FROM kb.project_schemas ptp
		JOIN kb.graph_schemas gtp ON gtp.id = ptp.schema_id
		WHERE ptp.project_id = ?
		ORDER BY ptp.installed_at DESC
	`, projectID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to get installed packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	packs := make([]InstalledSchemaItem, len(results))
	for i, r := range results {
		packs[i] = InstalledSchemaItem{
			ID:             r.ID,
			SchemaID:       r.SchemaID,
			Name:           r.Name,
			Version:        r.Version,
			Description:    r.Description,
			Active:         r.Active,
			InstalledAt:    r.InstalledAt,
			Customizations: r.Customizations,
		}
	}
	return packs, nil
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

	// Check if already assigned
	exists, err := r.db.NewSelect().
		Model((*ProjectMemorySchema)(nil)).
		Where("project_id = ?", projectID).
		Where("schema_id = ?", req.SchemaID).
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

// parseRelationshipTypeSchemas parses relationship_type_schemas JSON which may
// be stored as either a JSON object (map of name → definition) or a JSON array.
// It normalises the various source/target field naming conventions into the
// canonical SourceType / TargetType (singular) compiled output.
func parseRelationshipTypeSchemas(data json.RawMessage, packID, packName string) []RelationshipTypeSchema {
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
				Name:        name,
				Label:       def.Label,
				Description: def.Description,
				SourceType:  firstNonEmpty(joinTypes(def.SourceTypes), joinTypes(def.FromTypes), joinTypes(def.SnakeSourceTypes), def.Source),
				TargetType:  firstNonEmpty(joinTypes(def.TargetTypes), joinTypes(def.ToTypes), joinTypes(def.SnakeTargetTypes), def.Target),
				PackID:      packID,
				PackName:    packName,
			})
		}
		return result
	}

	// Try JSON array format
	var arr []RelationshipTypeSchema
	if err := json.Unmarshal(data, &arr); err == nil {
		for i := range arr {
			arr[i].PackID = packID
			arr[i].PackName = packName
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

// DeleteAssignment removes a pack assignment from a project
func (r *Repository) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	result, err := r.db.NewDelete().
		Model((*ProjectMemorySchema)(nil)).
		Where("id = ?", assignmentID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete assignment", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("assignment not found")
	}

	return nil
}

// CreatePack creates a new schema in the global registry
func (r *Repository) CreatePack(ctx context.Context, req *CreatePackRequest) (*GraphMemorySchema, error) {
	// Compute checksum from schemas
	checksumContent := map[string]json.RawMessage{
		"object_type_schemas":       req.ObjectTypeSchemas,
		"relationship_type_schemas": req.RelationshipTypeSchemas,
		"ui_configs":                req.UIConfigs,
		"extraction_prompts":        req.ExtractionPrompts,
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
		ObjectTypeSchemas:       req.ObjectTypeSchemas,
		RelationshipTypeSchemas: req.RelationshipTypeSchemas,
		UIConfigs:               req.UIConfigs,
		ExtractionPrompts:       req.ExtractionPrompts,
		Checksum:                &checksum,
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

// GetPack returns a schema by ID
func (r *Repository) GetPack(ctx context.Context, packID string) (*GraphMemorySchema, error) {
	var pack GraphMemorySchema
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", packID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get schema", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("schema not found")
	}
	return &pack, nil
}

// UpdatePack partially updates a schema in the global registry.
// Only non-nil / non-empty fields in req are applied.
func (r *Repository) UpdatePack(ctx context.Context, packID string, req *UpdatePackRequest) (*GraphMemorySchema, error) {
	// Fetch current record first to ensure it exists and to return full pack.
	var pack GraphMemorySchema
	err := r.db.NewSelect().Model(&pack).Where("id = ?", packID).Scan(ctx)
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

	if _, err := q.Returning("updated_at").Exec(ctx); err != nil {
		r.log.Error("failed to update schema", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &pack, nil
}

// DeletePack deletes a schema from the global registry.
// Returns an error if the pack is assigned to any projects.
func (r *Repository) DeletePack(ctx context.Context, packID string) error {
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

	// Check if already assigned
	var existingAssignment ProjectMemorySchema
	alreadyAssigned := false
	err = r.db.NewSelect().
		Model(&existingAssignment).
		Where("project_id = ?", projectID).
		Where("schema_id = ?", req.SchemaID).
		Scan(ctx)
	if err == nil {
		alreadyAssigned = true
	}

	if alreadyAssigned && !req.Merge && !req.DryRun {
		return nil, apperror.ErrBadRequest.WithMessage("schema already assigned to project")
	}

	// Parse object type schemas
	var objectTypeSchemas map[string]json.RawMessage
	if len(pack.ObjectTypeSchemas) > 0 {
		if err := json.Unmarshal(pack.ObjectTypeSchemas, &objectTypeSchemas); err != nil {
			r.log.Warn("failed to parse object type schemas for type registration", logger.Error(err))
			objectTypeSchemas = nil
		}
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
