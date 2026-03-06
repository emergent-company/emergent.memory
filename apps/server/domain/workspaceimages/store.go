package workspaceimages

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Store handles database operations for workspace images.
type Store struct {
	db bun.IDB
}

// NewStore creates a new workspace images store.
func NewStore(db bun.IDB) *Store {
	return &Store{db: db}
}

// Create inserts a new workspace image record.
func (s *Store) Create(ctx context.Context, img *WorkspaceImage) (*WorkspaceImage, error) {
	_, err := s.db.NewInsert().
		Model(img).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// GetByID returns a workspace image by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*WorkspaceImage, error) {
	img := new(WorkspaceImage)
	err := s.db.NewSelect().
		Model(img).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return img, nil
}

// GetByName returns a workspace image by name within a project.
func (s *Store) GetByName(ctx context.Context, projectID, name string) (*WorkspaceImage, error) {
	img := new(WorkspaceImage)
	err := s.db.NewSelect().
		Model(img).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return img, nil
}

// ListByProject returns all workspace images for a project.
func (s *Store) ListByProject(ctx context.Context, projectID string) ([]*WorkspaceImage, error) {
	var images []*WorkspaceImage
	err := s.db.NewSelect().
		Model(&images).
		Where("project_id = ?", projectID).
		Order("type ASC", "name ASC"). // built_in first, then by name
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return images, nil
}

// Update saves changes to an existing workspace image.
func (s *Store) Update(ctx context.Context, img *WorkspaceImage) (*WorkspaceImage, error) {
	img.UpdatedAt = time.Now()
	_, err := s.db.NewUpdate().
		Model(img).
		WherePK().
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// UpdateStatus updates just the status and error fields of an image.
func (s *Store) UpdateStatus(ctx context.Context, id string, status ImageStatus, errMsg *string) error {
	q := s.db.NewUpdate().
		Model((*WorkspaceImage)(nil)).
		Set("status = ?", status).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", id)

	if errMsg != nil {
		q = q.Set("error_msg = ?", *errMsg)
	} else {
		q = q.Set("error_msg = NULL")
	}

	_, err := q.Exec(ctx)
	return err
}

// Delete removes a workspace image by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.NewDelete().
		Model((*WorkspaceImage)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// UpsertByName inserts or updates (on conflict with project_id + name).
// Used by auto-seed to idempotently register built-in images.
func (s *Store) UpsertByName(ctx context.Context, img *WorkspaceImage) (*WorkspaceImage, error) {
	_, err := s.db.NewInsert().
		Model(img).
		On("CONFLICT (project_id, name) DO UPDATE").
		Set("type = EXCLUDED.type").
		Set("provider = EXCLUDED.provider").
		Set("status = EXCLUDED.status").
		Set("docker_ref = EXCLUDED.docker_ref").
		Set("updated_at = CURRENT_TIMESTAMP").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return img, nil
}
