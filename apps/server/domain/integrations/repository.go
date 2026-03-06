package integrations

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
)

// Common errors
var (
	ErrIntegrationNotFound = errors.New("integration not found")
)

// Repository handles database operations for integrations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new integrations repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{db: db, log: log}
}

// List returns integrations for a project with optional filtering
func (r *Repository) List(ctx context.Context, projectID string, params *ListIntegrationsParams) ([]*Integration, error) {
	var integrations []*Integration

	query := r.db.NewSelect().
		Model(&integrations).
		Where("project_id = ?", projectID).
		Order("created_at DESC")

	if params != nil {
		if params.Name != nil && *params.Name != "" {
			query = query.Where("name = ?", *params.Name)
		}
		if params.Enabled != nil {
			query = query.Where("enabled = ?", *params.Enabled)
		}
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return integrations, nil
}

// GetByName returns an integration by name for a project
func (r *Repository) GetByName(ctx context.Context, projectID, name string) (*Integration, error) {
	var integration Integration

	err := r.db.NewSelect().
		Model(&integration).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIntegrationNotFound
		}
		return nil, err
	}

	return &integration, nil
}

// GetByID returns an integration by ID
func (r *Repository) GetByID(ctx context.Context, id string) (*Integration, error) {
	var integration Integration

	err := r.db.NewSelect().
		Model(&integration).
		Where("id = ?", id).
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIntegrationNotFound
		}
		return nil, err
	}

	return &integration, nil
}

// Create creates a new integration
func (r *Repository) Create(ctx context.Context, integration *Integration) error {
	_, err := r.db.NewInsert().
		Model(integration).
		Exec(ctx)

	if err != nil {
		return err
	}

	r.log.Debug("created integration",
		slog.String("id", integration.ID),
		slog.String("name", integration.Name))
	return nil
}

// Update updates an existing integration
func (r *Repository) Update(ctx context.Context, integration *Integration) error {
	integration.UpdatedAt = time.Now()

	_, err := r.db.NewUpdate().
		Model(integration).
		WherePK().
		Exec(ctx)

	if err != nil {
		return err
	}

	r.log.Debug("updated integration",
		slog.String("id", integration.ID),
		slog.String("name", integration.Name))
	return nil
}

// Delete deletes an integration by name for a project
func (r *Repository) Delete(ctx context.Context, projectID, name string) error {
	res, err := r.db.NewDelete().
		Model((*Integration)(nil)).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Exec(ctx)

	if err != nil {
		return err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrIntegrationNotFound
	}

	r.log.Debug("deleted integration",
		slog.String("project_id", projectID),
		slog.String("name", name))
	return nil
}

// ExistsByName checks if an integration with the given name exists for a project
func (r *Repository) ExistsByName(ctx context.Context, projectID, name string) (bool, error) {
	count, err := r.db.NewSelect().
		Model((*Integration)(nil)).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Count(ctx)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
