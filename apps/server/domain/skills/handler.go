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
	// superadmin gates global skill creation; nil when the superadmin feature is
	// disabled (global create then falls back to authenticated-user-only).
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
	user := auth.MustGetUser(c)

	// Creating a global skill requires superadmin_full privileges. IsSuperadmin
	// returns the role alongside existence, so we enforce the same full/readonly
	// boundary the superadmin routes enforce via requireSuperadminRole; a
	// superadmin_readonly grant must not mint platform-global skills.
	if h.superadmin != nil {
		ok, role, err := h.superadmin.IsSuperadmin(c.Request().Context(), user.ID)
		if err != nil {
			return apperror.NewInternal("failed to check superadmin status", err)
		}
		if !ok || role != auth.RoleSuperadminFull {
			return apperror.ErrForbidden
		}
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

	return c.JSON(http.StatusOK, skill.ToDTO())
}

// UpdateSkill handles PATCH /api/skills/:id
// @Summary      Update a skill
// @Description  Partially update a skill. Regenerates embedding if description changes.
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        id   path string true "Skill ID (UUID)"
// @Param        body body UpdateSkillDTO true "Fields to update"
// @Success      200 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/skills/{id} [patch]
// @Security     bearerAuth
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

// DeleteSkill handles DELETE /api/skills/:id
// @Summary      Delete a skill
// @Description  Delete a skill by ID
// @Tags         skills
// @Produce      json
// @Param        id path string true "Skill ID (UUID)"
// @Success      204
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/skills/{id} [delete]
// @Security     bearerAuth
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

	// Attempt to resolve org context (best-effort, non-fatal)
	orgID := auth.OrgIDFromContext(c.Request().Context())

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
	return h.DeleteSkill(c)
}

// --- Helpers ---

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
