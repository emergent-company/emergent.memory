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

// assertCallerOwnsOrg verifies that the authenticated org in the context matches
// the provided orgID. Returns ErrForbidden if the check fails.
func assertCallerOwnsOrg(ctx context.Context, orgID string) error {
	callerOrgID := auth.OrgIDFromContext(ctx)
	if callerOrgID == "" {
		return apperror.ErrUnauthorized.WithMessage("organization context required")
	}
	if callerOrgID != orgID {
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
