package discoveryjobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for discovery jobs
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new discovery jobs repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("discoveryjobs.repo")),
	}
}

// Create creates a new discovery job
func (r *Repository) Create(ctx context.Context, job *DiscoveryJob) error {
	_, err := r.db.NewInsert().Model(job).Exec(ctx)
	if err != nil {
		r.log.Error("failed to create discovery job", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// GetByID retrieves a discovery job by ID
func (r *Repository) GetByID(ctx context.Context, jobID uuid.UUID) (*DiscoveryJob, error) {
	job := &DiscoveryJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, apperror.ErrNotFound.WithMessage("discovery job not found")
		}
		r.log.Error("failed to get discovery job", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return job, nil
}

// ListByProject retrieves discovery jobs for a project
func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]*DiscoveryJob, error) {
	if limit <= 0 {
		limit = 20
	}

	var jobs []*DiscoveryJob
	err := r.db.NewSelect().
		Model(&jobs).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list discovery jobs", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return jobs, nil
}

// UpdateStatus updates the status and optional error message
func (r *Repository) UpdateStatus(ctx context.Context, jobID uuid.UUID, status string, errorMessage *string) error {
	q := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("status = ?", status).
		Set("updated_at = now()").
		Where("id = ?", jobID)

	if errorMessage != nil {
		q = q.Set("error_message = ?", *errorMessage)
	}

	_, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to update job status", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// UpdateProgress updates the job progress
func (r *Repository) UpdateProgress(ctx context.Context, jobID uuid.UUID, progress JSONMap) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("progress = ?", progress).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update job progress", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// MarkStarted marks the job as started
func (r *Repository) MarkStarted(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("started_at = now()").
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to mark job started", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// MarkCompleted marks the job as completed with results
func (r *Repository) MarkCompleted(ctx context.Context, jobID uuid.UUID, templatePackID *uuid.UUID, discoveredTypes, discoveredRelationships JSONArray) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("status = ?", StatusCompleted).
		Set("template_pack_id = ?", templatePackID).
		Set("discovered_types = ?", discoveredTypes).
		Set("discovered_relationships = ?", discoveredRelationships).
		Set("completed_at = now()").
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to mark job completed", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// CancelJob cancels a job if it's in a cancellable state
func (r *Repository) CancelJob(ctx context.Context, jobID uuid.UUID) error {
	result, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("status = ?", StatusCancelled).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Where("status IN (?)", bun.In([]string{StatusPending, StatusAnalyzingDocuments, StatusExtractingTypes, StatusRefiningSchemas})).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to cancel job", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return apperror.ErrBadRequest.WithMessage("job cannot be cancelled (already completed, failed, or cancelled)")
	}
	return nil
}

// GetProjectKBPurpose retrieves the KB purpose for a project
func (r *Repository) GetProjectKBPurpose(ctx context.Context, projectID uuid.UUID) (string, error) {
	var kbPurpose string
	err := r.db.NewSelect().
		Table("kb.projects").
		Column("kb_purpose").
		Where("id = ?", projectID).
		Scan(ctx, &kbPurpose)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", apperror.ErrNotFound.WithMessage("project not found")
		}
		r.log.Error("failed to get project KB purpose", logger.Error(err))
		return "", apperror.ErrInternal.WithInternal(err)
	}
	if kbPurpose == "" {
		kbPurpose = "General purpose knowledge base for project documentation and knowledge management."
	}
	return kbPurpose, nil
}

// CreateTypeCandidate creates a new type candidate
func (r *Repository) CreateTypeCandidate(ctx context.Context, candidate *DiscoveryTypeCandidate) error {
	_, err := r.db.NewInsert().Model(candidate).Exec(ctx)
	if err != nil {
		r.log.Error("failed to create type candidate", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// GetCandidatesByJob retrieves all candidates for a job
func (r *Repository) GetCandidatesByJob(ctx context.Context, jobID uuid.UUID, status string) ([]*DiscoveryTypeCandidate, error) {
	var candidates []*DiscoveryTypeCandidate
	q := r.db.NewSelect().
		Model(&candidates).
		Where("job_id = ?", jobID).
		Order("confidence DESC")

	if status != "" {
		q = q.Where("status = ?", status)
	}

	err := q.Scan(ctx)
	if err != nil {
		r.log.Error("failed to get type candidates", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return candidates, nil
}

// UpdateCandidateStatus updates the status of a type candidate
func (r *Repository) UpdateCandidateStatus(ctx context.Context, candidateID uuid.UUID, status string) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryTypeCandidate)(nil)).
		Set("status = ?", status).
		Set("updated_at = now()").
		Where("id = ?", candidateID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update candidate status", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// SaveDiscoveredTypes saves the discovered types to the job
func (r *Repository) SaveDiscoveredTypes(ctx context.Context, jobID uuid.UUID, types JSONArray) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("discovered_types = ?", types).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to save discovered types", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// SaveDiscoveredRelationships saves the discovered relationships to the job
func (r *Repository) SaveDiscoveredRelationships(ctx context.Context, jobID uuid.UUID, relationships JSONArray) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("discovered_relationships = ?", relationships).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to save discovered relationships", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// GetDocumentContents retrieves document contents for the given IDs
func (r *Repository) GetDocumentContents(ctx context.Context, documentIDs []uuid.UUID) ([]DocumentContent, error) {
	var docs []DocumentContent
	err := r.db.NewSelect().
		Table("kb.documents").
		Column("id", "content", "filename").
		Where("id IN (?)", bun.In(documentIDs)).
		Scan(ctx, &docs)
	if err != nil {
		r.log.Error("failed to get document contents", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return docs, nil
}

// DocumentContent represents document data needed for discovery
type DocumentContent struct {
	ID       uuid.UUID `bun:"id"`
	Content  string    `bun:"content"`
	Filename string    `bun:"filename"`
}

// CreateTemplatePack creates a new template pack from discovery results
func (r *Repository) CreateTemplatePack(ctx context.Context, params CreateTemplatePackParams) (uuid.UUID, error) {
	var packID uuid.UUID
	err := r.db.NewInsert().
		Table("kb.graph_template_packs").
		Value("name", "?", params.Name).
		Value("version", "?", params.Version).
		Value("description", "?", params.Description).
		Value("author", "?", params.Author).
		Value("object_type_schemas", "?", params.ObjectTypeSchemas).
		Value("relationship_type_schemas", "?", params.RelationshipTypeSchemas).
		Value("ui_configs", "?", params.UIConfigs).
		Value("source", "?", params.Source).
		Value("discovery_job_id", "?", params.DiscoveryJobID).
		Value("pending_review", "?", params.PendingReview).
		Returning("id").
		Scan(ctx, &packID)
	if err != nil {
		r.log.Error("failed to create template pack", logger.Error(err))
		return uuid.Nil, apperror.ErrInternal.WithInternal(err)
	}
	return packID, nil
}

// CreateTemplatePackParams contains parameters for creating a template pack
type CreateTemplatePackParams struct {
	Name                    string
	Version                 string
	Description             string
	Author                  string
	ObjectTypeSchemas       JSONMap
	RelationshipTypeSchemas JSONMap
	UIConfigs               JSONMap
	Source                  string
	DiscoveryJobID          *uuid.UUID
	PendingReview           bool
}

// GetTemplatePack retrieves a template pack by ID
func (r *Repository) GetTemplatePack(ctx context.Context, packID uuid.UUID) (*TemplatePack, error) {
	pack := &TemplatePack{}
	err := r.db.NewSelect().
		Table("kb.graph_template_packs").
		Column("id", "object_type_schemas", "relationship_type_schemas", "ui_configs").
		Where("id = ?", packID).
		Scan(ctx, pack)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, apperror.ErrNotFound.WithMessage("template pack not found")
		}
		r.log.Error("failed to get template pack", logger.Error(err))
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	return pack, nil
}

// TemplatePack represents a template pack (subset of fields)
type TemplatePack struct {
	ID                      uuid.UUID `bun:"id"`
	ObjectTypeSchemas       JSONMap   `bun:"object_type_schemas,type:jsonb"`
	RelationshipTypeSchemas JSONMap   `bun:"relationship_type_schemas,type:jsonb"`
	UIConfigs               JSONMap   `bun:"ui_configs,type:jsonb"`
}

// UpdateTemplatePack updates an existing template pack
func (r *Repository) UpdateTemplatePack(ctx context.Context, packID uuid.UUID, objectSchemas, relSchemas, uiConfigs JSONMap) error {
	_, err := r.db.NewUpdate().
		Table("kb.graph_template_packs").
		Set("object_type_schemas = ?", objectSchemas).
		Set("relationship_type_schemas = ?", relSchemas).
		Set("ui_configs = ?", uiConfigs).
		Set("updated_at = now()").
		Where("id = ?", packID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update template pack", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}

// SetJobTemplatePack sets the template pack ID on a job and marks it completed
func (r *Repository) SetJobTemplatePack(ctx context.Context, jobID, templatePackID uuid.UUID) error {
	_, err := r.db.NewUpdate().
		Model((*DiscoveryJob)(nil)).
		Set("template_pack_id = ?", templatePackID).
		Set("status = ?", StatusCompleted).
		Set("completed_at = now()").
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to set job template pack", logger.Error(err))
		return apperror.ErrInternal.WithInternal(err)
	}
	return nil
}
