package branches

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent/pkg/apperror"
)

// Service handles business logic for branches
type Service struct {
	store *Store
}

// NewService creates a new branches service
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// List returns all branches, optionally filtered by project_id
func (s *Service) List(ctx context.Context, projectID *string) ([]*BranchResponse, error) {
	branches, err := s.store.List(ctx, projectID)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	return ToResponseList(branches), nil
}

// GetByID returns a branch by ID
func (s *Service) GetByID(ctx context.Context, id string) (*BranchResponse, error) {
	branch, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if branch == nil {
		return nil, apperror.ErrNotFound.WithMessage("branch not found")
	}

	return ToResponse(branch), nil
}

// Create creates a new branch
func (s *Service) Create(ctx context.Context, req *CreateBranchRequest) (*BranchResponse, error) {
	// Validate name
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperror.ErrBadRequest.WithMessage("name is required")
	}

	// Check for duplicate name within the same project
	existing, err := s.store.GetByNameAndProject(ctx, name, req.ProjectID)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if existing != nil {
		return nil, apperror.ErrConflict.WithMessage("branch name already exists in this project")
	}

	// If parent_branch_id provided, verify it exists
	if req.ParentBranchID != nil {
		parent, err := s.store.GetByID(ctx, *req.ParentBranchID)
		if err != nil {
			return nil, apperror.ErrInternal.WithInternal(err)
		}
		if parent == nil {
			return nil, apperror.ErrNotFound.WithMessage("parent branch not found")
		}
	}

	// Create the branch
	branch := &Branch{
		ProjectID:      req.ProjectID,
		Name:           name,
		ParentBranchID: req.ParentBranchID,
	}

	created, err := s.store.Create(ctx, branch)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Ensure branch lineage is populated (best-effort, don't fail if it errors)
	_ = s.store.EnsureBranchLineage(ctx, created.ID, req.ParentBranchID)

	return ToResponse(created), nil
}

// Update updates a branch by ID
func (s *Service) Update(ctx context.Context, id string, req *UpdateBranchRequest) (*BranchResponse, error) {
	// Check if branch exists
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if existing == nil {
		return nil, apperror.ErrNotFound.WithMessage("branch not found")
	}

	// Validate name if provided
	if req.Name == nil {
		return nil, apperror.ErrBadRequest.WithMessage("name is required")
	}

	name := strings.TrimSpace(*req.Name)
	if name == "" {
		return nil, apperror.ErrBadRequest.WithMessage("name cannot be empty")
	}

	// Check for duplicate name within the same project (if name changed)
	if name != existing.Name {
		duplicate, err := s.store.GetByNameAndProject(ctx, name, existing.ProjectID)
		if err != nil {
			return nil, apperror.ErrInternal.WithInternal(err)
		}
		if duplicate != nil && duplicate.ID != id {
			return nil, apperror.ErrConflict.WithMessage("branch name already exists in this project")
		}
	}

	// Update the branch
	updated, err := s.store.Update(ctx, id, name)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if updated == nil {
		return nil, apperror.ErrNotFound.WithMessage("branch not found")
	}

	return ToResponse(updated), nil
}

// Delete deletes a branch by ID
func (s *Service) Delete(ctx context.Context, id string) error {
	// Delete lineage first (best-effort)
	_ = s.store.DeleteBranchLineage(ctx, id)

	deleted, err := s.store.Delete(ctx, id)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("branch not found")
	}

	return nil
}
