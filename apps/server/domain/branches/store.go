package branches

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
)

// Store handles database operations for branches
type Store struct {
	db bun.IDB
}

// NewStore creates a new branches store
func NewStore(db bun.IDB) *Store {
	return &Store{db: db}
}

// List returns all branches, optionally filtered by project_id
func (s *Store) List(ctx context.Context, projectID *string) ([]*Branch, error) {
	var branches []*Branch
	q := s.db.NewSelect().Model(&branches).Order("created_at ASC")

	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}

	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return branches, nil
}

// GetByID returns a branch by ID
func (s *Store) GetByID(ctx context.Context, id string) (*Branch, error) {
	branch := new(Branch)
	err := s.db.NewSelect().
		Model(branch).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return branch, nil
}

// GetByNameAndProject returns a branch by name and project_id (null-safe)
func (s *Store) GetByNameAndProject(ctx context.Context, name string, projectID *string) (*Branch, error) {
	branch := new(Branch)
	q := s.db.NewSelect().
		Model(branch).
		Where("name = ?", name)

	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	} else {
		q = q.Where("project_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return branch, nil
}

// Create creates a new branch and returns it
func (s *Store) Create(ctx context.Context, branch *Branch) (*Branch, error) {
	_, err := s.db.NewInsert().
		Model(branch).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return branch, nil
}

// Update updates a branch by ID
func (s *Store) Update(ctx context.Context, id string, name string) (*Branch, error) {
	branch := new(Branch)
	_, err := s.db.NewUpdate().
		Model(branch).
		Set("name = ?", name).
		Where("id = ?", id).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	// Check if any row was actually updated
	if branch.ID == "" {
		return nil, nil
	}

	return branch, nil
}

// Delete deletes a branch by ID, returns true if deleted
func (s *Store) Delete(ctx context.Context, id string) (bool, error) {
	result, err := s.db.NewDelete().
		Model((*Branch)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// EnsureBranchLineage ensures lineage records exist for a branch
// This copies the NestJS behavior for maintaining the branch_lineage table
func (s *Store) EnsureBranchLineage(ctx context.Context, branchID string, parentBranchID *string) error {
	// Insert self lineage (depth=0)
	_, err := s.db.NewRaw(`
		INSERT INTO kb.branch_lineage(branch_id, ancestor_branch_id, depth) 
		VALUES (?, ?, 0) 
		ON CONFLICT DO NOTHING
	`, branchID, branchID).Exec(ctx)
	if err != nil {
		// Ignore errors if table doesn't exist (older schema)
		return nil
	}

	if parentBranchID != nil {
		// Ensure parent self row exists (defensive)
		_, _ = s.db.NewRaw(`
			INSERT INTO kb.branch_lineage(branch_id, ancestor_branch_id, depth) 
			VALUES (?, ?, 0) 
			ON CONFLICT DO NOTHING
		`, *parentBranchID, *parentBranchID).Exec(ctx)

		// Copy parent lineage rows with depth+1
		_, _ = s.db.NewRaw(`
			INSERT INTO kb.branch_lineage(branch_id, ancestor_branch_id, depth)
			SELECT ?, ancestor_branch_id, depth + 1 
			FROM kb.branch_lineage 
			WHERE branch_id = ?
			ON CONFLICT (branch_id, ancestor_branch_id) DO NOTHING
		`, branchID, *parentBranchID).Exec(ctx)

		// Add direct parent (depth=1) if not already captured
		_, _ = s.db.NewRaw(`
			INSERT INTO kb.branch_lineage(branch_id, ancestor_branch_id, depth)
			VALUES (?, ?, 1) 
			ON CONFLICT (branch_id, ancestor_branch_id) DO NOTHING
		`, branchID, *parentBranchID).Exec(ctx)
	}

	return nil
}

// DeleteBranchLineage removes all lineage records for a branch
func (s *Store) DeleteBranchLineage(ctx context.Context, branchID string) error {
	_, err := s.db.NewRaw(`
		DELETE FROM kb.branch_lineage WHERE branch_id = ?
	`, branchID).Exec(ctx)
	// Ignore errors if table doesn't exist
	return err
}
