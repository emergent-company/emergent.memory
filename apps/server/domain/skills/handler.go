package skills

import (
	"log/slog"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

var nameRegex = regexp.MustCompile(NamePattern)

// Handler handles HTTP requests for skills.
type Handler struct {
	repo          *Repository
	embeddingsSvc *embeddings.Service
	log           *slog.Logger
}

// NewHandler creates a new skills handler.
func NewHandler(repo *Repository, embeddingsSvc *embeddings.Service, log *slog.Logger) *Handler {
	return &Handler{
		repo:          repo,
		embeddingsSvc: embeddingsSvc,
		log:           log.With(logger.Scope("skills.handler")),
	}
}

// --- Global skill endpoints ---

// ListGlobalSkills handles GET /api/skills
// @Summary      List global skills
// @Description  List all global (project-independent) skills
// @Tags         skills
// @Produce      json
// @Success      200 {object} ListSkillsResponse
// @Failure      401 {object} apperror.Error
// @Router       /api/skills [get]
// @Security     bearerAuth
func (h *Handler) ListGlobalSkills(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	skills, err := h.repo.FindAll(c.Request().Context(), nil)
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
// @Description  Create a new global skill available to all agents
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        body body CreateSkillDTO true "Skill to create"
// @Success      201 {object} SkillDTO
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Router       /api/skills [post]
// @Security     bearerAuth
func (h *Handler) CreateGlobalSkill(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var dto CreateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := validateSkillName(dto.Name); err != nil {
		return err
	}

	skill := &Skill{
		Name:        dto.Name,
		Description: dto.Description,
		Content:     dto.Content,
		Metadata:    dto.Metadata,
		ProjectID:   nil, // global
	}

	embedding := h.generateEmbedding(c, dto.Description)

	if err := h.repo.Create(c.Request().Context(), skill, embedding); err != nil {
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id, err := parseSkillID(c)
	if err != nil {
		return err
	}

	var dto UpdateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	var embedding []float32
	descriptionChanged := dto.Description != nil
	if descriptionChanged {
		embedding = h.generateEmbedding(c, *dto.Description)
	}

	skill, err := h.repo.Update(c.Request().Context(), id, &dto, embedding, descriptionChanged)
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id, err := parseSkillID(c)
	if err != nil {
		return err
	}

	if err := h.repo.Delete(c.Request().Context(), id); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// --- Project-scoped skill endpoints ---

// ListProjectSkills handles GET /api/projects/:projectId/skills
// @Summary      List project skills
// @Description  List all skills available to agents in the project (global + project-scoped, merged)
// @Tags         skills
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} ListSkillsResponse
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/projects/{projectId}/skills [get]
// @Security     bearerAuth
func (h *Handler) ListProjectSkills(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	skills, err := h.repo.FindForAgent(c.Request().Context(), projectID)
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var dto CreateSkillDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if err := validateSkillName(dto.Name); err != nil {
		return err
	}

	skill := &Skill{
		Name:        dto.Name,
		Description: dto.Description,
		Content:     dto.Content,
		Metadata:    dto.Metadata,
		ProjectID:   &projectID,
	}

	embedding := h.generateEmbedding(c, dto.Description)

	if err := h.repo.Create(c.Request().Context(), skill, embedding); err != nil {
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
	// Delegate — project auth check is via route middleware
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
	// Delegate — project auth check is via route middleware
	return h.DeleteSkill(c)
}

// --- Helpers ---

// validateSkillName checks that the name matches the slug pattern and length constraints.
func validateSkillName(name string) error {
	if name == "" {
		return apperror.ErrBadRequest.WithMessage("name is required")
	}
	if len(name) > 64 {
		return apperror.ErrBadRequest.WithMessage("name must be 64 characters or fewer")
	}
	if !nameRegex.MatchString(name) {
		return apperror.ErrBadRequest.WithMessage("name must be a lowercase alphanumeric slug (e.g. my-skill)")
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

// generateEmbedding attempts to generate an embedding for the given text.
// On failure it logs a warning and returns nil (non-fatal).
func (h *Handler) generateEmbedding(c echo.Context, text string) []float32 {
	if text == "" {
		return nil
	}
	vec, err := h.embeddingsSvc.EmbedQuery(c.Request().Context(), text)
	if err != nil {
		h.log.Warn("skills: failed to generate description embedding (skill will have no embedding)",
			logger.Error(err),
		)
		return nil
	}
	return vec
}
