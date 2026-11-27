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
//   - Project-bound API tokens: the claimed project must match the token's bound
//     project (RequireProjectTokenScope already bound the header; this closes the
//     initialize-params bypass that could otherwise claim a foreign project).
//     Exception (issue #1163): a token minted by the built-in OAuth authorization
//     server is a user-scoped connector credential — its consent selects the
//     initial project, but the signed-in user may switch the MCP session to any
//     project they are an org member of. Such a token falls through to the same
//     membership check; the grant lookup fails closed.
//   - Ownerless tokens (user_id = NULL): no owning user exists to resolve
//     membership for, so they pass through (mirroring RequireProjectMember).
//   - Session callers and account-level tokens with an owning user: the claimed
//     project's owning org is resolved server-side (kb.projects) and membership
//     is checked against kb.organization_memberships, so a client-supplied
//     project_id can never self-satisfy the check.
//
// Fail closed: an empty claim passes (project-less callers), a nil db passes
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

	// Mirror RequireProjectMember's caller classification (pkg/auth/middleware.go)
	// so the initialize-claimed project is held to the same contract as the
	// header/path-scoped project.
	//
	// 1. Project-bound tokens are already bound by RequireProjectTokenScope;
	//    reconcile the initialize-claimed project against the binding.
	if user.APITokenProjectID != "" {
		if projectID == user.APITokenProjectID {
			return nil
		}
		// An OAuth-minted token is a user-scoped connector credential: the
		// consent flow picks the initial project, but the session may switch to
		// another project the owning user belongs to (issue #1163). The grant
		// lookup is server-side and fails closed; a grantless project API token
		// keeps the strict binding. Membership is checked below either way.
		if user.ID == "" || user.APITokenID == "" {
			return apperror.NewForbidden("API token is scoped to a different project")
		}
		oauthGranted, err := s.tokenHasActiveOAuthGrant(ctx, user.APITokenID)
		if err != nil {
			return err
		}
		if !oauthGranted {
			return apperror.NewForbidden("API token is scoped to a different project")
		}
	}

	// 2. Ownerless tokens (user_id = NULL) have no owning user to resolve
	//    membership for; they are unreachable through user-facing mint paths.
	if user.APITokenID != "" && user.ID == "" {
		return nil
	}

	// 3. Session callers and account-level tokens with an owning user require a
	//    real identity and org membership. Previously an account-level token
	//    (project_id NULL) fell into the APITokenID branch above and returned
	//    nil here without a membership check, letting it claim any project via
	//    the initialize params (the #1019 repair closes that bypass).
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
