package templatepacks

import (
	"context"
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
