package provider

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// --- Application-level cross-tenant access guards ---
//
// These helpers enforce that mutations only operate on resources belonging to
// the authenticated caller's organization / project. They are called by
// CredentialService write methods before any DB mutation.

// assertCallerOwnsOrg verifies that the authenticated caller is a member of the
// target organization. The caller's org is derived from real membership
// (kb.organization_memberships, keyed on the authenticated user's ID) — never
// from the :orgId path parameter, so an empty-context request can no longer
// self-satisfy the check (issue #841).
//
// Returns ErrUnauthorized when no authenticated user is present, ErrForbidden
// when the caller is not a member of orgID.
func assertCallerOwnsOrg(ctx context.Context, repo *Repository, orgID string) error {
	user, err := auth.RequireUser(ctx)
	if err != nil {
		return apperror.ErrUnauthorized.WithMessage("organization context required")
	}

	isMember, err := repo.IsUserOrgMember(ctx, orgID, user.ID)
	if err != nil {
		return err
	}
	if !isMember {
		return apperror.ErrForbidden.WithMessage("access to organization provider config denied")
	}
	return nil
}

// assertCallerOwnsProject verifies that the project's org matches the
// authenticated org in the context.
func (s *CredentialService) assertCallerOwnsProject(ctx context.Context, projectID string) error {
	if projectID == "" {
		return fmt.Errorf("project ID is required")
	}
	callerOrgID := auth.OrgIDFromContext(ctx)
	if callerOrgID == "" {
		return apperror.ErrUnauthorized.WithMessage("organization context required")
	}

	projectOrgID, err := s.repo.GetOrgIDForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if projectOrgID != callerOrgID {
		return apperror.ErrForbidden.WithMessage("access to project provider config denied")
	}
	return nil
}
