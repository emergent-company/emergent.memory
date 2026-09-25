package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// RequireProjectMembership asserts the authenticated user is a member of the
// organization that owns projectID. It is the handler-layer counterpart to
// Middleware.RequireProjectMember for route groups whose addressed project is
// supplied via a source the middleware does not inspect (?project_id query
// parameter or a request-body field). Both sides are resolved server-side: the
// caller identity comes from the authenticated user (never a request-controlled
// value) and the owning org from kb.projects, so a client-supplied project ID
// can never self-satisfy the check (issue #913).
//
// Returns:
//   - 401 when no authenticated user is present;
//   - 403 when the caller is an API token bound to a different project, or is
//     not a member of the project's owning org;
//   - 404 when the addressed project does not exist (no existence oracle);
//   - internal errors unwrapped.
//
// This mirrors Middleware.RequireProjectMember's pass-through rules for the two
// bounded classes (project-bound emt_* tokens, ownerless server-minted tokens).
func RequireProjectMembership(ctx context.Context, db bun.IDB, projectID string) error {
	user := UserFromContext(ctx)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Project-bound emt_* tokens are bound to their project at mint time; they
	// must address their own project. The token-scope guard is absent from
	// query/body-scoped groups, so enforce the binding here.
	if user.APITokenProjectID != "" {
		if projectID != "" && projectID != user.APITokenProjectID {
			return apperror.NewForbidden("API token is scoped to a different project")
		}
		return nil
	}

	// Ownerless server-minted tokens (user_id = NULL) have no owning user to
	// resolve membership for; pass through (unreachable via user-facing mint).
	if user.APITokenID != "" && user.ID == "" {
		return nil
	}

	// Session and account-token callers require a real user identity.
	if user.ID == "" {
		return apperror.ErrUnauthorized
	}
	if projectID == "" {
		return nil
	}

	orgID, err := projectOrg(ctx, db, projectID)
	if err != nil {
		return err
	}
	if orgID == "" {
		return apperror.NewNotFound("project", projectID)
	}

	isMember, err := isOrgMember(ctx, db, orgID, user.ID)
	if err != nil {
		return err
	}
	if !isMember {
		return apperror.NewForbidden("access to project denied")
	}
	return nil
}

// projectOrg resolves the owning organization of a project. A missing project
// returns ("", nil) — no trusted org context.
func projectOrg(ctx context.Context, db bun.IDB, projectID string) (string, error) {
	if db == nil {
		return "", errors.New("auth: no database available for project org lookup")
	}
	var orgID string
	err := db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Limit(1).
		Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return orgID, nil
}

// isOrgMember reports whether the user holds any membership role in the given
// organization.
func isOrgMember(ctx context.Context, db bun.IDB, orgID, userID string) (bool, error) {
	if db == nil {
		return false, errors.New("auth: no database available for org membership lookup")
	}
	var ok bool
	err := db.NewRaw(`
		SELECT EXISTS(
			SELECT 1 FROM kb.organization_memberships
			WHERE organization_id = ? AND user_id = ?
		)
	`, orgID, userID).Scan(ctx, &ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}
