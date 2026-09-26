package backups

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// orgAdminRole is the canonical kb.organization_memberships role that owns
// org-scoped backup administration. It mirrors the role string used by
// pkg/auth.CanGrantAdminAll and pkg/auth.dbOrgAdmin.
const orgAdminRole = "org_admin"

// requireOrgAdmin authorizes an org-scoped backup operation. The caller's role
// is derived server-side from kb.organization_memberships (keyed on the
// authenticated user ID, never a client-supplied org id); only an org_admin of
// the addressed org passes. It returns the addressed org id on success.
func (h *Handler) requireOrgAdmin(c echo.Context) (string, error) {
	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return "", err
	}
	orgID := c.Param("orgId")
	if orgID == "" {
		return "", apperror.ErrBadRequest
	}
	role, err := h.service.repo.OrgMembershipRole(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return "", apperror.NewInternal("failed to resolve org membership", err)
	}
	if role != orgAdminRole {
		return "", apperror.ErrForbidden
	}
	return orgID, nil
}

// requireProjectBackupAuthority authorizes a project-scoped backup operation
// (create or overwrite-restore). It admits a project_admin of the addressed
// project OR an org_admin of the project's owning organization. Both sides are
// resolved server-side: the owning org from kb.projects, the caller's roles from
// kb.project_memberships / kb.organization_memberships. A caller-supplied
// :projectId can never self-satisfy the check (the #849/#850 class).
//
// Returns the addressed project id and its owning org id on success.
func (h *Handler) requireProjectBackupAuthority(c echo.Context) (projectID, orgID string, err error) {
	user, aerr := auth.RequireUser(c.Request().Context())
	if aerr != nil {
		return "", "", aerr
	}
	projectID = c.Param("projectId")
	if projectID == "" {
		return "", "", apperror.ErrBadRequest
	}
	orgID, err = h.service.repo.ProjectOrgID(c.Request().Context(), projectID)
	if err != nil {
		return "", "", apperror.NewInternal("failed to resolve project org", err)
	}
	if orgID == "" {
		return "", "", apperror.NewNotFound("project", projectID)
	}

	// org_admin of the owning org is the broader grant and short-circuits.
	orgRole, err := h.service.repo.OrgMembershipRole(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return "", "", apperror.NewInternal("failed to resolve org membership", err)
	}
	if orgRole == orgAdminRole {
		return projectID, orgID, nil
	}

	projectRole, err := h.service.repo.ProjectMembershipRole(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return "", "", apperror.NewInternal("failed to resolve project membership", err)
	}
	if projectRole != auth.RoleProjectAdmin {
		return "", "", apperror.ErrForbidden
	}
	return projectID, orgID, nil
}

// requireRestoreOwnership authorizes a restore-status read. It resolves the
// restore's owning organization server-side and requires org_admin of that org
// (the parent org tier). A missing restore — or one owned by a foreign org the
// caller does not administer — yields a 404, matching the not-found-on-foreign
// convention (#968/#974), so a caller cannot distinguish "absent" from
// "not yours".
func (h *Handler) requireRestoreOwnership(c echo.Context) (*Restore, error) {
	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return nil, err
	}
	restoreID := c.Param("restoreId")
	if restoreID == "" {
		return nil, apperror.ErrBadRequest
	}
	job, err := h.service.GetRestore(c.Request().Context(), restoreID)
	if err != nil {
		return nil, apperror.NewInternal("failed to get restore", err)
	}
	if job == nil {
		return nil, apperror.NewNotFound("restore", restoreID)
	}
	role, err := h.service.repo.OrgMembershipRole(c.Request().Context(), job.OrganizationID, user.ID)
	if err != nil {
		return nil, apperror.NewInternal("failed to resolve org membership", err)
	}
	if role != orgAdminRole {
		return nil, apperror.NewNotFound("restore", restoreID)
	}
	return job, nil
}
