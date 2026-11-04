package branches

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Service handles business logic for branches
type Service struct {
	store *Store
}

// NewService creates a new branches service
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// RequireProjectMember asserts the authenticated caller is a member of the org
// that owns projectID, resolving the owning org server-side (issue #913). The
// project ID is client-supplied via ?project_id / body / header, so it must
// never be treated as authorization truth on its own.
func (s *Service) RequireProjectMember(ctx context.Context, projectID string) error {
	return auth.RequireProjectMembership(ctx, s.store.db, projectID)
}

// List returns all branches, optionally filtered by project_id
func (s *Service) List(ctx context.Context, projectID *string) ([]*BranchResponse, error) {
	branches, err := s.store.List(ctx, projectID)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	return ToResponseList(branches), nil
}

// GetByID returns a branch by ID, scoped to the given project.
func (s *Service) GetByID(ctx context.Context, id, projectID string) (*BranchResponse, error) {
	branch, err := s.store.GetByID(ctx, id, projectID)
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
	// Branches must always be project-scoped. A project_id is mandatory so
	// that org-global (project_id IS NULL) branches can never be created.
	if req.ProjectID == nil || strings.TrimSpace(*req.ProjectID) == "" {
		return nil, apperror.ErrBadRequest.WithMessage("project_id is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(*req.ProjectID)); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("invalid project_id format")
	}

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
		parent, err := s.store.GetByID(ctx, *req.ParentBranchID, *req.ProjectID)
		if err != nil {
			return nil, apperror.ErrInternal.WithInternal(err)
		}
		if parent == nil {
			return nil, apperror.ErrNotFound.WithMessage("parent branch not found")
		}
	} else if req.ProjectID != nil {
		// Auto-assign the project's main branch as parent so callers can
		// discover the main branch ID from the response. If no main branch
		// exists yet this is the first branch in the project — it becomes
		// the main branch and correctly has no parent.
		if main, err := s.store.GetMainBranch(ctx, *req.ProjectID); err == nil && main != nil {
			req.ParentBranchID = &main.ID
		}
	}

	// Create the branch
	branch := &Branch{
		ProjectID:      req.ProjectID,
		Name:           name,
		Description:    req.Description,
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

// Update updates a branch by ID, scoped to the given project.
func (s *Service) Update(ctx context.Context, id, projectID string, req *UpdateBranchRequest) (*BranchResponse, error) {
	// Check if branch exists within the project
	existing, err := s.store.GetByID(ctx, id, projectID)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if existing == nil {
		return nil, apperror.ErrNotFound.WithMessage("branch not found")
	}

	// Validate that at least one field is being updated
	if req.Name == nil && req.Description == nil {
		return nil, apperror.ErrBadRequest.WithMessage("at least one of name or description is required")
	}

	// Use existing name if not changing it
	name := existing.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
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
	}

	// Update the branch
	updated, err := s.store.Update(ctx, id, projectID, name, req.Description)
	if err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}
	if updated == nil {
		return nil, apperror.ErrNotFound.WithMessage("branch not found")
	}

	return ToResponse(updated), nil
}

// Delete deletes a branch by ID, scoped to the given project.
func (s *Service) Delete(ctx context.Context, id, projectID string) error {
	// Delete lineage first (best-effort)
	_ = s.store.DeleteBranchLineage(ctx, id)

	deleted, err := s.store.Delete(ctx, id, projectID)
	if err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("branch not found")
	}

	return nil
}
