package datasource

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// Common errors for integrations
var (
	ErrIntegrationNotFound = errors.New("data source integration not found")
)

// Repository handles data source integration data operations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new datasource Repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("datasource.repository")),
	}
}

// List returns all integrations for a project with optional filters
func (r *Repository) List(ctx context.Context, projectID string, params *ListIntegrationsParams) ([]*DataSourceIntegration, error) {
	var integrations []*DataSourceIntegration

	q := r.db.NewSelect().
		Model(&integrations).
		Where("project_id = ?", projectID).
		OrderExpr("created_at DESC")

	if params != nil {
		if params.ProviderType != nil {
			q = q.Where("provider_type = ?", *params.ProviderType)
		}
		if params.SourceType != nil {
			q = q.Where("source_type = ?", *params.SourceType)
		}
		if params.Status != nil {
			q = q.Where("status = ?", *params.Status)
		}
	}

	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return integrations, nil
}

// GetByID retrieves an integration by ID
func (r *Repository) GetByID(ctx context.Context, projectID, id string) (*DataSourceIntegration, error) {
	integration := &DataSourceIntegration{}
	err := r.db.NewSelect().
		Model(integration).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIntegrationNotFound
		}
		return nil, err
	}
	return integration, nil
}

// GetByIDWithoutProject retrieves an integration by ID without project constraint
// Used for internal operations like worker processing
func (r *Repository) GetByIDWithoutProject(ctx context.Context, id string) (*DataSourceIntegration, error) {
	integration := &DataSourceIntegration{}
	err := r.db.NewSelect().
		Model(integration).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIntegrationNotFound
		}
		return nil, err
	}
	return integration, nil
}

// Create creates a new integration
func (r *Repository) Create(ctx context.Context, integration *DataSourceIntegration) error {
	_, err := r.db.NewInsert().
		Model(integration).
		Exec(ctx)
	if err != nil {
		return err
	}
	r.log.Debug("created data source integration",
		slog.String("id", integration.ID),
		slog.String("provider_type", integration.ProviderType))
	return nil
}

// Update updates an existing integration
func (r *Repository) Update(ctx context.Context, integration *DataSourceIntegration) error {
	_, err := r.db.NewUpdate().
		Model(integration).
		WherePK().
		Exec(ctx)
	if err != nil {
		return err
	}
	r.log.Debug("updated data source integration",
		slog.String("id", integration.ID))
	return nil
}

// Delete deletes an integration by ID
func (r *Repository) Delete(ctx context.Context, projectID, id string) error {
	res, err := r.db.NewDelete().
		Model((*DataSourceIntegration)(nil)).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrIntegrationNotFound
	}

	r.log.Debug("deleted data source integration", slog.String("id", id))
	return nil
}

// ExistsByName checks if an integration with the given name exists in the project
func (r *Repository) ExistsByName(ctx context.Context, projectID, name string) (bool, error) {
	count, err := r.db.NewSelect().
		Model((*DataSourceIntegration)(nil)).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetSourceTypeCounts returns document counts by source type for a project
func (r *Repository) GetSourceTypeCounts(ctx context.Context, projectID string) ([]SourceTypeDTO, error) {
	var results []SourceTypeDTO

	// Query documents table grouped by source_type
	err := r.db.NewSelect().
		TableExpr("kb.documents d").
		ColumnExpr("d.source_type").
		ColumnExpr("COUNT(*) as document_count").
		Where("d.project_id = ?", projectID).
		Where("d.source_type IS NOT NULL").
		GroupExpr("d.source_type").
		OrderExpr("document_count DESC").
		Scan(ctx, &results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// ListSyncJobs returns sync jobs for an integration with pagination
func (r *Repository) ListSyncJobs(ctx context.Context, integrationID string, params *ListSyncJobsParams) ([]*DataSourceSyncJob, int, error) {
	var jobs []*DataSourceSyncJob

	q := r.db.NewSelect().
		Model(&jobs).
		Where("integration_id = ?", integrationID)

	if params != nil && params.Status != nil {
		q = q.Where("status = ?", *params.Status)
	}

	count, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	q = q.OrderExpr("created_at DESC")

	if params != nil {
		if params.Limit > 0 {
			q = q.Limit(params.Limit)
		} else {
			q = q.Limit(20) // Default limit
		}
		if params.Offset > 0 {
			q = q.Offset(params.Offset)
		}
	} else {
		q = q.Limit(20)
	}

	err = q.Scan(ctx)
	if err != nil {
		return nil, 0, err
	}

	return jobs, count, nil
}

// GetLatestSyncJob returns the most recent sync job for an integration
func (r *Repository) GetLatestSyncJob(ctx context.Context, integrationID string) (*DataSourceSyncJob, error) {
	job := &DataSourceSyncJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("integration_id = ?", integrationID).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No jobs exist yet
		}
		return nil, err
	}
	return job, nil
}

// GetSyncJob returns a specific sync job
func (r *Repository) GetSyncJob(ctx context.Context, integrationID, jobID string) (*DataSourceSyncJob, error) {
	job := &DataSourceSyncJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("id = ?", jobID).
		Where("integration_id = ?", integrationID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return job, nil
}
