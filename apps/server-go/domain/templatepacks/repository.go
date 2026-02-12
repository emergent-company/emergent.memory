package templatepacks

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for template packs
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new template packs repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("templatepacks.repo")),
	}
}

// GetCompiledTypesByProject returns compiled object and relationship types for a project
func (r *Repository) GetCompiledTypesByProject(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	// Get all active template packs for the project with their schemas
	var projectPacks []ProjectTemplatePack
	err := r.db.NewSelect().
		Model(&projectPacks).
		Relation("TemplatePack").
		Where("ptp.project_id = ?", projectID).
		Where("ptp.active = true").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get project template packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	response := &CompiledTypesResponse{
		ObjectTypes:       []ObjectTypeSchema{},
		RelationshipTypes: []RelationshipTypeSchema{},
	}

	// Compile types from all active packs
	for _, pp := range projectPacks {
		if pp.TemplatePack == nil {
			continue
		}

		tp := pp.TemplatePack

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
		if len(tp.RelationshipTypeSchemas) > 0 {
			var relTypes []RelationshipTypeSchema
			if err := json.Unmarshal(tp.RelationshipTypeSchemas, &relTypes); err != nil {
				r.log.Warn("failed to parse relationship type schemas",
					slog.String("packId", tp.ID),
					logger.Error(err))
			} else {
				// Add pack info to each type
				for i := range relTypes {
					relTypes[i].PackID = tp.ID
					relTypes[i].PackName = tp.Name
				}
				response.RelationshipTypes = append(response.RelationshipTypes, relTypes...)
			}
		}
	}

	return response, nil
}

// GetAvailablePacks returns template packs available for a project to install
func (r *Repository) GetAvailablePacks(ctx context.Context, projectID string) ([]TemplatePackListItem, error) {
	// Get IDs of packs already installed for this project
	var installedIDs []string
	err := r.db.NewSelect().
		Model((*ProjectTemplatePack)(nil)).
		Column("template_pack_id").
		Where("project_id = ?", projectID).
		Scan(ctx, &installedIDs)
	if err != nil {
		r.log.Error("failed to get installed pack IDs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Get all packs not installed for this project
	var packs []TemplatePackListItem
	q := r.db.NewSelect().
		Model((*GraphTemplatePack)(nil)).
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
		return []TemplatePackListItem{}, nil
	}
	return packs, nil
}

// GetInstalledPacks returns template packs installed for a project
func (r *Repository) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledPackItem, error) {
	var results []struct {
		ID             string                 `bun:"id"`
		TemplatePackID string                 `bun:"template_pack_id"`
		Name           string                 `bun:"name"`
		Version        string                 `bun:"version"`
		Description    *string                `bun:"description"`
		Active         bool                   `bun:"active"`
		InstalledAt    time.Time              `bun:"installed_at"`
		Customizations map[string]interface{} `bun:"customizations,type:jsonb"`
	}

	err := r.db.NewRaw(`
		SELECT ptp.id, ptp.template_pack_id, gtp.name, gtp.version, gtp.description,
			   ptp.active, ptp.installed_at, ptp.customizations
		FROM kb.project_template_packs ptp
		JOIN kb.graph_template_packs gtp ON gtp.id = ptp.template_pack_id
		WHERE ptp.project_id = ?
		ORDER BY ptp.installed_at DESC
	`, projectID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to get installed packs", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	packs := make([]InstalledPackItem, len(results))
	for i, r := range results {
		packs[i] = InstalledPackItem{
			ID:             r.ID,
			TemplatePackID: r.TemplatePackID,
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

// AssignPack assigns a template pack to a project
func (r *Repository) AssignPack(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*ProjectTemplatePack, error) {
	// Check if template pack exists
	packExists, err := r.db.NewSelect().
		Model((*GraphTemplatePack)(nil)).
		Where("id = ?", req.TemplatePackID).
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check template pack existence", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if !packExists {
		return nil, apperror.ErrNotFound.WithMessage("template pack not found")
	}

	// Check if already assigned
	exists, err := r.db.NewSelect().
		Model((*ProjectTemplatePack)(nil)).
		Where("project_id = ?", projectID).
		Where("template_pack_id = ?", req.TemplatePackID).
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check pack assignment", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if exists {
		return nil, apperror.ErrBadRequest.WithMessage("template pack already assigned to project")
	}

	assignment := &ProjectTemplatePack{
		ProjectID:      projectID,
		TemplatePackID: req.TemplatePackID,
		Active:         true,
		InstalledAt:    time.Now(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_, err = r.db.NewInsert().Model(assignment).Returning("id").Exec(ctx)
	if err != nil {
		r.log.Error("failed to assign pack", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return assignment, nil
}

// UpdateAssignment updates a pack assignment (e.g., active status)
func (r *Repository) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) error {
	q := r.db.NewUpdate().
		Model((*ProjectTemplatePack)(nil)).
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
		Model((*ProjectTemplatePack)(nil)).
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

// CreatePack creates a new template pack in the global registry
func (r *Repository) CreatePack(ctx context.Context, req *CreatePackRequest) (*GraphTemplatePack, error) {
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

	pack := &GraphTemplatePack{
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
		r.log.Error("failed to create template pack", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return pack, nil
}

// GetPack returns a template pack by ID
func (r *Repository) GetPack(ctx context.Context, packID string) (*GraphTemplatePack, error) {
	var pack GraphTemplatePack
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", packID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get template pack", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("template pack not found")
	}
	return &pack, nil
}

// DeletePack deletes a template pack from the global registry.
// Returns an error if the pack is assigned to any projects.
func (r *Repository) DeletePack(ctx context.Context, packID string) error {
	// Check if assigned to any projects
	assignedCount, err := r.db.NewSelect().
		Model((*ProjectTemplatePack)(nil)).
		Where("template_pack_id = ?", packID).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to check pack assignments", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	if assignedCount > 0 {
		return apperror.ErrBadRequest.WithMessage("cannot delete template pack that is assigned to projects")
	}

	result, err := r.db.NewDelete().
		Model((*GraphTemplatePack)(nil)).
		Where("id = ?", packID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete template pack", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("template pack not found")
	}

	return nil
}

// AssignPackWithTypes assigns a template pack to a project AND populates the type registry.
// This fixes the bug where the REST AssignPack endpoint did not register types.
func (r *Repository) AssignPackWithTypes(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*ProjectTemplatePack, error) {
	// Get the full template pack with schemas
	var pack GraphTemplatePack
	err := r.db.NewSelect().
		Model(&pack).
		Where("id = ?", req.TemplatePackID).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to get template pack", logger.Error(err))
		return nil, apperror.ErrNotFound.WithMessage("template pack not found")
	}

	// Check if already assigned
	exists, err := r.db.NewSelect().
		Model((*ProjectTemplatePack)(nil)).
		Where("project_id = ?", projectID).
		Where("template_pack_id = ?", req.TemplatePackID).
		Exists(ctx)
	if err != nil {
		r.log.Error("failed to check pack assignment", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if exists {
		return nil, apperror.ErrBadRequest.WithMessage("template pack already assigned to project")
	}

	// Parse object type schemas to get type names
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

	now := time.Now()
	assignment := &ProjectTemplatePack{
		ProjectID:      projectID,
		TemplatePackID: req.TemplatePackID,
		Active:         true,
		InstalledAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Run in a transaction to ensure atomicity of assignment + type registration
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Create the assignment
		_, err := tx.NewInsert().Model(assignment).Returning("id").Exec(ctx)
		if err != nil {
			return err
		}

		// Register types from the pack into project_object_type_registry
		if objectTypeSchemas != nil {
			for typeName, schema := range objectTypeSchemas {
				// Check if type already exists for this project (skip if so)
				typeExists, err := tx.NewSelect().
					TableExpr("kb.project_object_type_registry").
					Where("project_id = ?", projectID).
					Where("type_name = ?", typeName).
					Exists(ctx)
				if err != nil {
					return err
				}
				if typeExists {
					r.log.Info("type already registered, skipping",
						slog.String("typeName", typeName),
						slog.String("projectId", projectID))
					continue
				}

				schemaJSON := string(schema)
				uiConfigJSON := "{}"
				if uiConfigs != nil {
					if cfg, ok := uiConfigs[typeName]; ok {
						uiConfigJSON = string(cfg)
					}
				}
				extractionConfigJSON := "{}"
				if extractionPrompts != nil {
					if cfg, ok := extractionPrompts[typeName]; ok {
						extractionConfigJSON = string(cfg)
					}
				}

				_, err = tx.NewRaw(`
					INSERT INTO kb.project_object_type_registry 
					(project_id, type_name, source, template_pack_id, json_schema, ui_config, extraction_config, enabled, created_by)
					VALUES (?, ?, 'template', ?, ?, ?, ?, true, ?)
				`, projectID, typeName, req.TemplatePackID, schemaJSON, uiConfigJSON, extractionConfigJSON, userID).Exec(ctx)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		r.log.Error("failed to assign pack with types", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return assignment, nil
}
