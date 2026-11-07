package skills

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ListSkillProposals handles GET /api/projects/:projectId/skills/proposals.
// @Summary      List staged skill proposals
// @Description  List the agent skill-capture proposal inbox for a project. Use status=pending (default) for review, status=all for history.
// @Tags         skills
// @Produce      json
// @Param        projectId path  string true  "Project ID (UUID)"
// @Param        status    query string false "pending|applied|rejected|failed|all"
// @Param        limit     query int    false "Maximum rows (default 50, max 200)"
// @Success      200 {object} map[string]any
// @Router       /api/projects/{projectId}/skills/proposals [get]
// @Security     bearerAuth
func (h *Handler) ListSkillProposals(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid projectId")
	}
	status := c.QueryParam("status")
	if status == "" {
		status = ProposalStatusPending
	}
	if status == "all" {
		status = ""
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	rows, err := h.proposals.List(c.Request().Context(), pid, status, limit)
	if err != nil {
		return err
	}
	dtos := make([]*ProposalDTO, 0, len(rows))
	for _, p := range rows {
		dtos = append(dtos, p.ToDTO())
	}
	return c.JSON(http.StatusOK, map[string]any{"proposals": dtos, "count": len(dtos)})
}

// GetSkillProposal handles GET /api/projects/:projectId/skills/proposals/:proposalId.
// @Summary      Get a staged skill proposal
// @Tags         skills
// @Produce      json
// @Param        projectId  path string true "Project ID (UUID)"
// @Param        proposalId path string true "Proposal ID (UUID)"
// @Success      200 {object} ProposalDTO
// @Router       /api/projects/{projectId}/skills/proposals/{proposalId} [get]
// @Security     bearerAuth
func (h *Handler) GetSkillProposal(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid projectId")
	}
	id, err := uuid.Parse(c.Param("proposalId"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid proposalId")
	}
	p, err := h.proposals.Get(c.Request().Context(), pid, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p.ToDTO())
}

// ApproveSkillProposal handles POST /api/projects/:projectId/skills/proposals/:proposalId/approve.
// @Summary      Approve and apply a staged skill proposal
// @Description  Applies the proposal through the normal skills store. The security scan always runs and can still block the write.
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        projectId  path string             true "Project ID (UUID)"
// @Param        proposalId path string             true "Proposal ID (UUID)"
// @Param        body       body ApproveProposalDTO false "Optional review note"
// @Success      200 {object} map[string]any
// @Router       /api/projects/{projectId}/skills/proposals/{proposalId}/approve [post]
// @Security     bearerAuth
func (h *Handler) ApproveSkillProposal(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid projectId")
	}
	id, err := uuid.Parse(c.Param("proposalId"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid proposalId")
	}
	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return apperror.ErrUnauthorized
	}
	actorID, err := uuid.Parse(user.ID)
	if err != nil {
		return apperror.ErrUnauthorized
	}
	var body ApproveProposalDTO
	_ = c.Bind(&body)

	applied, err := h.proposals.Approve(c.Request().Context(), pid, id, actorID, body.Note)
	if err != nil {
		return err
	}
	resp := map[string]any{"success": true}
	if applied != nil {
		resp["skill"] = applied.ToDTO()
	}
	return c.JSON(http.StatusOK, resp)
}

// RejectSkillProposal handles POST /api/projects/:projectId/skills/proposals/:proposalId/reject.
// @Summary      Reject a staged skill proposal
// @Tags         skills
// @Accept       json
// @Produce      json
// @Param        projectId  path string             true "Project ID (UUID)"
// @Param        proposalId path string             true "Proposal ID (UUID)"
// @Param        body       body RejectProposalDTO  false "Optional review note"
// @Success      200 {object} map[string]any
// @Router       /api/projects/{projectId}/skills/proposals/{proposalId}/reject [post]
// @Security     bearerAuth
func (h *Handler) RejectSkillProposal(c echo.Context) error {
	projectID := c.Param("projectId")
	if _, err := h.requireProjectMember(c, projectID); err != nil {
		return err
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid projectId")
	}
	id, err := uuid.Parse(c.Param("proposalId"))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid proposalId")
	}
	user, err := auth.RequireUser(c.Request().Context())
	if err != nil {
		return apperror.ErrUnauthorized
	}
	actorID, err := uuid.Parse(user.ID)
	if err != nil {
		return apperror.ErrUnauthorized
	}
	var body RejectProposalDTO
	_ = c.Bind(&body)
	if err := h.proposals.Reject(c.Request().Context(), pid, id, actorID, body.Note); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true})
}
