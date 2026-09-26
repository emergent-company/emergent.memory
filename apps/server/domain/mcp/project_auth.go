package mcp

import (
	"context"
	"database/sql"
	"errors"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// authorizeProjectClaim asserts the caller is authorized to address projectID.
//
// It is the handler-level counterpart to RequireProjectTokenScope +
// RequireProjectMember for the session-bound MCP endpoints (unified /api/mcp and
// legacy /api/mcp/rpc). Those endpoints resolve the project at initialize time
// from the client-supplied initialize params (with the X-Project-ID header as
// fallback) and stash it in the session — a value the middleware cannot see.
// This method reconciles that claim against the same contract the middleware
// enforces for the header/path-scoped routes (issue #868, the #864 class):
//
//   - API-token callers: the claimed project must match the token's bound
//     project (RequireProjectTokenScope already bound the header; this closes
//     the initialize-params bypass that could otherwise claim a foreign project).
//   - Session callers: the claimed project's owning org is resolved server-side
//     (kb.projects) and membership is checked against kb.organization_memberships,
//     so a client-supplied project_id can never self-satisfy the check.
//
// Fail closed: an empty claim passes (account-level callers), a nil db passes
// (unit-test posture without a database), an absent project is NotFound, a
// non-member is Forbidden.
func (s *Service) authorizeProjectClaim(ctx context.Context, user *auth.AuthUser, projectID string) error {
	// user is guaranteed non-nil by RequireAuth (handlers call auth.MustGetUser).
	if projectID == "" {
		return nil
	}
	if s.db == nil {
		return nil
	}

	// API-token callers are bound by RequireProjectTokenScope; reconcile the
	// initialize-claimed project against the token's project.
	if user.APITokenID != "" {
		if user.APITokenProjectID != "" && projectID != user.APITokenProjectID {
			return apperror.NewForbidden("API token is scoped to a different project")
		}
		return nil
	}

	// Session callers require a real identity and org membership.
	if user.ID == "" {
		return apperror.ErrUnauthorized
	}

	var orgID string
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NewNotFound("project", projectID)
		}
		return apperror.NewInternal("failed to resolve project organization", err)
	}

	exists, err := s.db.NewSelect().
		TableExpr("kb.organization_memberships").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", user.ID).
		Exists(ctx)
	if err != nil {
		return apperror.NewInternal("failed to check project membership", err)
	}
	if !exists {
		return apperror.NewForbidden("access to project denied")
	}
	return nil
}
