package skills

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Handler handles HTTP requests for skills.
type Handler struct {
	repo *Repository
	log  *slog.Logger
	// superadmin gates global skill create/update/delete; nil when the superadmin
	// feature is disabled. A nil module FAILS CLOSED — absence of the dependency
	// is not permission, so global mutations are denied rather than let through.
	superadmin *superadmin.Repository
}

// NewHandler creates a new skills handler.
func NewHandler(repo *Repository, log *slog.Logger, superadmin *superadmin.Repository) *Handler {
	return &Handler{
		repo:       repo,
		log:        log.With(logger.Scope("skills.handler")),
		superadmin: superadmin,
	}
}

// --- Global skill endpoints ---

// ListGlobalSkills handles GET /api/skills
// @Summary      List global skills
// @Description  List all global (built-in, project- and org-independent) skills
// @Tags         skills
// @Produce      json
// @Success      200 {object} ListSkillsResponse
// @Failure      401 {object} apperror.Error
// @Router       /api/skills [get]
// @Security     bearerAuth
func (h *Handler) ListGlobalSkills(c echo.Context) error {

	skills, err := h.repo.FindAll(c.Request().Context(), nil, nil)
	if err != nil {
		return err
	}

	dtos := make([]*SkillDTO, 0, len(skills))
	for _, s := range skills {
		dtos = append(dtos, s.ToDTO())
	}
	return c.JSON(http.StatusOK, ListSkillsResponse{Data: dtos})
}

// CreateGlobalSkill handles POST /api/skills
// @Summary      Create a global skill
// @Description  Create a new global skill available to all agents. Superadmin only.
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        body body CreateSkillDTO true "Skill to create"
// @Success      201 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/skills [post]
// @Security     bearerAuth
func (h *Handler) CreateGlobalSkill(c echo.Context) error {
	if err := h.requireSuperadminFull(c); err != nil {
		return err
	}

	var dto CreateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := ValidateCreateSkill(dto, h.repo.MaxContentSize()); err != nil {
		return err
	}

	skill := &Skill{
		Name:        dto.Name,
		Description: dto.Description,
		Content:     dto.Content,
		Metadata:    dto.Metadata,
		ProjectID:   nil, // global
		OrgID:       nil,
	}

	if err := h.repo.Create(c.Request().Context(), skill); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, skill.ToDTO())
}

// GetSkill handles GET /api/skills/:id
// @Summary      Get a skill
// @Description  Get a skill by ID
// @Tags         skills
// @Produce      json
// @Param        id path string true "Skill ID (UUID)"
// @Success      200 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/skills/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetSkill(c echo.Context) error {

	id, err := parseSkillID(c)
	if err != nil {
		return err
	}

	skill, err := h.repo.FindByID(c.Request().Context(), id)
	if err != nil {
		return err
	}

	if err := h.authorizeSkillRead(c, skill); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, skill.ToDTO())
}

// UpdateGlobalSkill handles PATCH /api/skills/:id
// @Summary      Update a global skill
// @Description  Partially update a global skill. Superadmin_full only. Regenerates embedding if description changes.
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        id   path string true "Skill ID (UUID)"
// @Param        body body UpdateSkillDTO true "Fields to update"
// @Success      200 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/skills/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateGlobalSkill(c echo.Context) error {
	if err := h.requireSuperadminFull(c); err != nil {
		return err
	}
	return h.UpdateSkill(c)
}

// UpdateSkill implements the shared partial-update logic for org- and
// project-scoped skill endpoints.
func (h *Handler) UpdateSkill(c echo.Context) error {

	id, err := parseSkillID(c)
	if err != nil {
		return err
	}

	var dto UpdateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := ValidateUpdateSkill(dto, h.repo.MaxContentSize()); err != nil {
		return err
	}

	skill, err := h.repo.Update(c.Request().Context(), id, &dto)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, skill.ToDTO())
}

// DeleteGlobalSkill handles DELETE /api/skills/:id
// @Summary      Delete a global skill
// @Description  Delete a global skill by ID. Superadmin_full only.
// @Tags         skills
// @Produce      json
// @Param        id path string true "Skill ID (UUID)"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/skills/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteGlobalSkill(c echo.Context) error {
	if err := h.requireSuperadminFull(c); err != nil {
		return err
	}
	return h.DeleteSkill(c)
}

// DeleteSkill implements the shared delete logic for org- and project-scoped
// skill endpoints.
func (h *Handler) DeleteSkill(c echo.Context) error {

	id, err := parseSkillID(c)
	if err != nil {
		return err
	}

	if err := h.repo.Delete(c.Request().Context(), id); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// --- Org-scoped skill endpoints ---

// ListOrgSkills handles GET /api/orgs/:orgId/skills
// @Summary      List org skills
// @Description  List all org-scoped skills for the given organization
// @Tags         skills
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {object} ListSkillsResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/orgs/{orgId}/skills [get]
// @Security     bearerAuth
func (h *Handler) ListOrgSkills(c echo.Context) error {

	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("orgId is required")
	}

	if err := h.requireOrgMember(c, orgID); err != nil {
		return err
	}

	skills, err := h.repo.FindAll(c.Request().Context(), nil, &orgID)
	if err != nil {
		return err
	}

	dtos := make([]*SkillDTO, 0, len(skills))
	for _, s := range skills {
		dtos = append(dtos, s.ToDTO())
	}
	return c.JSON(http.StatusOK, ListSkillsResponse{Data: dtos})
}

// CreateOrgSkill handles POST /api/orgs/:orgId/skills
// @Summary      Create an org skill
// @Description  Create a skill scoped to the given organization
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        body  body CreateSkillDTO true "Skill to create"
// @Success      201 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/orgs/{orgId}/skills [post]
// @Security     bearerAuth
func (h *Handler) CreateOrgSkill(c echo.Context) error {

	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("orgId is required")
	}

	if err := h.requireOrgMember(c, orgID); err != nil {
		return err
	}

	var dto CreateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := ValidateCreateSkill(dto, h.repo.MaxContentSize()); err != nil {
		return err
	}

	skill := &Skill{
		Name:        dto.Name,
		Description: dto.Description,
		Content:     dto.Content,
		Metadata:    dto.Metadata,
		ProjectID:   nil,
		OrgID:       &orgID,
	}

	if err := h.repo.Create(c.Request().Context(), skill); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, skill.ToDTO())
}

// UpdateOrgSkill handles PATCH /api/orgs/:orgId/skills/:id
// @Summary      Update an org skill
// @Description  Partially update an org-scoped skill
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        id    path string true "Skill ID (UUID)"
// @Param        body  body UpdateSkillDTO true "Fields to update"
// @Success      200 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/orgs/{orgId}/skills/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateOrgSkill(c echo.Context) error {
	orgID := c.Param("orgId")
	if err := h.requireOrgMember(c, orgID); err != nil {
		return err
	}
	if _, err := h.requireOrgSkill(c, orgID); err != nil {
		return err
	}
	return h.UpdateSkill(c)
}

// DeleteOrgSkill handles DELETE /api/orgs/:orgId/skills/:id
// @Summary      Delete an org skill
// @Description  Delete an org-scoped skill
// @Tags         skills
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        id    path string true "Skill ID (UUID)"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/orgs/{orgId}/skills/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteOrgSkill(c echo.Context) error {
	orgID := c.Param("orgId")
	if err := h.requireOrgMember(c, orgID); err != nil {
		return err
	}
	if _, err := h.requireOrgSkill(c, orgID); err != nil {
		return err
	}
	return h.DeleteSkill(c)
}

// --- Project-scoped skill endpoints ---

// ListProjectSkills handles GET /api/projects/:projectId/skills
// @Summary      List project skills
// @Description  List all skills available to agents in the project (global + org + project-scoped, merged)
// @Tags         skills
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} ListSkillsResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/projects/{projectId}/skills [get]
// @Security     bearerAuth
func (h *Handler) ListProjectSkills(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	orgID, err := h.requireProjectMember(c, projectID)
	if err != nil {
		return err
	}

	skills, err := h.repo.FindForAgent(c.Request().Context(), projectID, orgID)
	if err != nil {
		return err
	}

	dtos := make([]*SkillDTO, 0, len(skills))
	for _, s := range skills {
		dtos = append(dtos, s.ToDTO())
	}
	return c.JSON(http.StatusOK, ListSkillsResponse{Data: dtos})
}

// CreateProjectSkill handles POST /api/projects/:projectId/skills
// @Summary      Create a project skill
// @Description  Create a skill scoped to the given project
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        body      body CreateSkillDTO true "Skill to create"
// @Success      201 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/projects/{projectId}/skills [post]
// @Security     bearerAuth
func (h *Handler) CreateProjectSkill(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}

	var dto CreateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := ValidateCreateSkill(dto, h.repo.MaxContentSize()); err != nil {
		return err
	}

	skill := &Skill{
		Name:        dto.Name,
		Description: dto.Description,
		Content:     dto.Content,
		Metadata:    dto.Metadata,
		ProjectID:   &projectID,
		OrgID:       nil,
	}

	if err := h.repo.Create(c.Request().Context(), skill); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, skill.ToDTO())
}

// UpdateProjectSkill handles PATCH /api/projects/:projectId/skills/:id
// @Summary      Update a project skill
// @Description  Partially update a project-scoped skill
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id        path string true "Skill ID (UUID)"
// @Param        body      body UpdateSkillDTO true "Fields to update"
// @Success      200 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/projects/{projectId}/skills/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateProjectSkill(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	if _, err := h.requireProjectSkill(c, projectID); err != nil {
		return err
	}
	return h.UpdateSkill(c)
}

// DeleteProjectSkill handles DELETE /api/projects/:projectId/skills/:id
// @Summary      Delete a project skill
// @Description  Delete a project-scoped skill
// @Tags         skills
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id        path string true "Skill ID (UUID)"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/projects/{projectId}/skills/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteProjectSkill(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	if _, err := h.requireProjectSkill(c, projectID); err != nil {
		return err
	}
	return h.DeleteSkill(c)
}

// --- Helpers ---

// requireOrgMember asserts the authenticated caller is a member of orgID.
// Returns 401 when no authenticated user is present, 403 when the caller is not
// a member. The caller's org is derived from real membership (issue #849), never
// from the :orgId path parameter.
func (h *Handler) requireOrgMember(c echo.Context, orgID string) error {
	ctx := c.Request().Context()
	user, err := auth.RequireUser(ctx)
	if err != nil {
		return apperror.ErrUnauthorized.WithMessage("authentication required")
	}
	ok, err := h.repo.IsUserOrgMember(ctx, orgID, user.ID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrForbidden.WithMessage("access to organization skills denied")
	}
	return nil
}

// requireProjectMember asserts the authenticated caller is a member of the org
// that owns projectID. Returns 401 when no user is present, 404 when the project
// does not exist, 403 when the caller is not a member. The owning org is looked
// up server-side (issue #849/#850), so a project addressed by a foreign caller
// can never self-satisfy the check. It returns the resolved owning org so
// callers (e.g. ListProjectSkills) can reuse it without a second lookup.
func (h *Handler) requireProjectMember(c echo.Context, projectID string) (string, error) {
	ctx := c.Request().Context()
	user, err := auth.RequireUser(ctx)
	if err != nil {
		return "", apperror.ErrUnauthorized.WithMessage("authentication required")
	}
	orgID, err := h.repo.GetOrgIDForProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	ok, err := h.repo.IsUserOrgMember(ctx, orgID, user.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", apperror.ErrForbidden.WithMessage("access to project skills denied")
	}
	return orgID, nil
}

// requireOrgSkill asserts the skill addressed by :id belongs to orgID. It
// returns 404 when the skill does not exist OR belongs to a different org, so
// the response does not leak the existence of a cross-tenant skill (issue #849).
func (h *Handler) requireOrgSkill(c echo.Context, orgID string) (*Skill, error) {
	id, err := parseSkillID(c)
	if err != nil {
		return nil, err
	}
	skill, err := h.repo.FindByID(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if skill.OrgID == nil || *skill.OrgID != orgID {
		return nil, apperror.NewNotFound("skill", id.String())
	}
	return skill, nil
}

// requireProjectSkill asserts the skill addressed by :id belongs to projectID.
// It returns 404 when the skill does not exist OR belongs to a different
// project, mirroring requireOrgSkill's existence-oracle avoidance.
func (h *Handler) requireProjectSkill(c echo.Context, projectID string) (*Skill, error) {
	id, err := parseSkillID(c)
	if err != nil {
		return nil, err
	}
	skill, err := h.repo.FindByID(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if skill.ProjectID == nil || *skill.ProjectID != projectID {
		return nil, apperror.NewNotFound("skill", id.String())
	}
	return skill, nil
}

// requireSuperadminFull denies the request unless the authenticated user holds
// an active superadmin_full grant. A nil superadmin module (feature disabled)
// fails closed: the absence of the dependency is not permission, so the request
// is denied rather than let through.
func (h *Handler) requireSuperadminFull(c echo.Context) error {
	if h.superadmin == nil {
		return apperror.ErrForbidden
	}
	user := auth.MustGetUser(c)
	ok, err := h.superadmin.IsSuperadminFull(c.Request().Context(), user.ID)
	if err != nil {
		return apperror.NewInternal("failed to check superadmin status", err)
	}
	if !ok {
		return apperror.ErrForbidden
	}
	return nil
}

// parseSkillID extracts and parses the :id path parameter.
func parseSkillID(c echo.Context) (uuid.UUID, error) {
	idStr := c.Param("id")
	if idStr == "" {
		return uuid.Nil, apperror.ErrBadRequest.WithMessage("id is required")
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, apperror.ErrBadRequest.WithMessage("invalid skill ID")
	}
	return id, nil
}

// authorizeSkillRead enforces scope-based read authority for a single skill
// addressed by id (GET /api/skills/:id). Skills are three-tiered: a global
// skill (project_id IS NULL AND org_id IS NULL) is platform catalogue readable
// by any authenticated caller — mirroring ListGlobalSkills and the blueprints
// global-catalogue read posture; an org-scoped skill requires membership of its
// org; a project-scoped skill requires membership of the project's owning org.
// A cross-tenant read returns 404 (not 403) so the response does not leak a
// skill's existence, mirroring requireOrgSkill/requireProjectSkill. 401 only
// when no authenticated user is present (the route's RequireAuth guard already
// guarantees one, so this is defense-in-depth).
func (h *Handler) authorizeSkillRead(c echo.Context, skill *Skill) error {
	if skill.ProjectID == nil && skill.OrgID == nil {
		return nil // global catalogue: readable by any authenticated caller
	}

	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return err
	}

	if skill.OrgID != nil {
		ok, err := h.repo.IsUserOrgMember(c.Request().Context(), *skill.OrgID, user.ID)
		if err != nil {
			return err
		}
		if !ok {
			return apperror.NewNotFound("skill", skill.ID.String())
		}
		return nil
	}

	// Project-scoped: authorize against the owning org resolved server-side
	// (never a caller-supplied path/header), mirroring requireProjectMember.
	orgID, err := h.repo.GetOrgIDForProject(c.Request().Context(), *skill.ProjectID)
	if err != nil {
		return err
	}
	ok, err := h.repo.IsUserOrgMember(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NewNotFound("skill", skill.ID.String())
	}
	return nil
}
