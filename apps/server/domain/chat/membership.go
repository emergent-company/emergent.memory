package chat

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// requireProjectAccess enforces project authorization for the header-scoped
// /api/chat group (issue #864). The reusable auth.Middleware.RequireProjectMember /
// RequireProjectTokenScope pair keys on the :projectId URL path parameter, which
// this group does not carry — its project is the X-Project-ID header, surfaced
// as user.ProjectID by RequireAuth. This chat-local guard mirrors those
// middlewares' semantics against the header-derived project ID so the group is
// protected for every caller type:
//
//   - no authenticated user           → 401
//   - project-bound emt_* token whose
//     header mismatches its project   → 403 (token binding)
//   - emt_* token caller              → pass through (bound above)
//   - session caller, project absent  → 404 (no existence oracle)
//   - session caller, non-member      → 403
func (h *Handler) requireProjectAccess(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		u := auth.GetUser(c)
		if u == nil {
			return apperror.ErrUnauthorized
		}

		// Token binding: a project-bound emt_* token must not reach another
		// project via the X-Project-ID header (mirrors RequireProjectTokenScope).
		if u.APITokenProjectID != "" && u.ProjectID != "" && u.ProjectID != u.APITokenProjectID {
			return apperror.NewForbidden("API token is scoped to a different project")
		}

		// emt_* tokens are already bound to their project by the check above;
		// their owner is not necessarily an org member (account-level/admin tokens).
		if u.APITokenID != "" {
			return next(c)
		}

		// Session caller: require a real user identity.
		if u.ID == "" {
			return apperror.ErrUnauthorized
		}

		projectID := u.ProjectID
		if projectID == "" {
			// RequireProjectID has already enforced a non-empty project header.
			return next(c)
		}

		orgID, err := h.svc.repo.GetOrgIDForProject(c.Request().Context(), projectID)
		if err != nil {
			return err
		}
		if orgID == "" {
			return apperror.NewNotFound("project", projectID)
		}

		isMember, err := h.svc.repo.IsUserOrgMember(c.Request().Context(), orgID, u.ID)
		if err != nil {
			return err
		}
		if !isMember {
			return apperror.NewForbidden("access to project chat denied")
		}
		return next(c)
	}
}
