package agents

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// WorkItemHeadDTO is the response projection of a work item's HEAD.
type WorkItemHeadDTO struct {
	ProjectID   string `json:"projectId"`
	CanonicalID string `json:"canonicalId"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Status      string `json:"status"`
	Assignee    string `json:"assignee"`
	Version     int    `json:"version"`
}

// WorkItemActionDTO is the response for a human action on a work item.
type WorkItemActionDTO struct {
	Item      *WorkItemHeadDTO `json:"item"`
	Round     int              `json:"round,omitempty"`
	Escalated bool             `json:"escalated,omitempty"`
	RunID     string           `json:"runId,omitempty"`
}

// RequestChangesWorkItemDTO is the request body for request-changes.
type RequestChangesWorkItemDTO struct {
	// Feedback is required (non-empty) review feedback recorded as a new round.
	Feedback string `json:"feedback"`
}

// ReassignWorkItemDTO is the request body for reassign. An empty assignee
// clears the lane (any listener may claim).
type ReassignWorkItemDTO struct {
	Assignee string `json:"assignee"`
}

func toWorkItemHeadDTO(h *graph.WorkObjectHead) *WorkItemHeadDTO {
	if h == nil {
		return nil
	}
	return &WorkItemHeadDTO{
		ProjectID:   h.ProjectID,
		CanonicalID: h.CanonicalID,
		Type:        h.Type,
		Key:         h.Key,
		Status:      h.Status,
		Assignee:    h.Assignee,
		Version:     h.Version,
	}
}

func (h *Handler) workActionParams(c echo.Context) (projectID, canonicalID string, err error) {
	projectID = c.Param("projectId")
	canonicalID = c.Param("canonicalId")
	if projectID == "" || canonicalID == "" {
		return "", "", apperror.NewBadRequest("projectId and canonicalId are required")
	}
	return projectID, canonicalID, nil
}

// ApproveWorkItem handles POST /api/projects/:projectId/work-items/:canonicalId/approve
//
// @Summary      Approve a work item
// @Description  Finalizes a work item in review: sets reviewed_by/reviewed_at, clears needs_review, and moves it to done
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Success      200 {object} APIResponse[WorkItemActionDTO] "Approved"
// @Failure      404 {object} apperror.Error "Not found"
// @Failure      409 {object} apperror.Error "Not in review"
// @Router       /api/projects/{projectId}/work-items/{canonicalId}/approve [post]
// @Security     bearerAuth
func (h *Handler) ApproveWorkItem(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	res, err := h.workActions.Approve(c.Request().Context(), projectID, canonicalID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, SuccessResponse(&WorkItemActionDTO{Item: toWorkItemHeadDTO(res.Head)}))
}

// RequestChangesWorkItem handles POST /api/projects/:projectId/work-items/:canonicalId/request-changes
//
// @Summary      Request changes on a work item
// @Description  Returns a work item in review to revision, records feedback, and enqueues a rework run (or escalates at the revision cap)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Param        body body RequestChangesWorkItemDTO true "Feedback"
// @Success      200 {object} APIResponse[WorkItemActionDTO] "Changes requested"
// @Failure      400 {object} apperror.Error "Empty feedback"
// @Failure      409 {object} apperror.Error "Not in review"
// @Router       /api/projects/{projectId}/work-items/{canonicalId}/request-changes [post]
// @Security     bearerAuth
func (h *Handler) RequestChangesWorkItem(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	var dto RequestChangesWorkItemDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	res, err := h.workActions.RequestChanges(c.Request().Context(), projectID, canonicalID, user.ID, dto.Feedback)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, SuccessResponse(&WorkItemActionDTO{
		Item:      toWorkItemHeadDTO(res.Head),
		Round:     res.Round,
		Escalated: res.Escalated,
		RunID:     res.RunID,
	}))
}

// RetryWorkItem handles POST /api/projects/:projectId/work-items/:canonicalId/retry
//
// @Summary      Retry a work item
// @Description  Returns a blocked work item to ready and enqueues it
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Success      200 {object} APIResponse[WorkItemActionDTO] "Retried"
// @Failure      409 {object} apperror.Error "Not blocked"
// @Router       /api/projects/{projectId}/work-items/{canonicalId}/retry [post]
// @Security     bearerAuth
func (h *Handler) RetryWorkItem(c echo.Context) error {
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	res, err := h.workActions.Retry(c.Request().Context(), projectID, canonicalID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, SuccessResponse(&WorkItemActionDTO{
		Item:  toWorkItemHeadDTO(res.Head),
		RunID: res.RunID,
	}))
}

// ReassignWorkItem handles POST /api/projects/:projectId/work-items/:canonicalId/reassign
//
// @Summary      Reassign a work item
// @Description  Sets or clears the assignee of a work item (empty assignee clears it)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Param        body body ReassignWorkItemDTO true "Assignee"
// @Success      200 {object} APIResponse[WorkItemActionDTO] "Reassigned"
// @Failure      404 {object} apperror.Error "Not found"
// @Router       /api/projects/{projectId}/work-items/{canonicalId}/reassign [post]
// @Security     bearerAuth
func (h *Handler) ReassignWorkItem(c echo.Context) error {
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	var dto ReassignWorkItemDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	assignee := &dto.Assignee
	res, err := h.workActions.Reassign(c.Request().Context(), projectID, canonicalID, assignee)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, SuccessResponse(&WorkItemActionDTO{Item: toWorkItemHeadDTO(res.Head)}))
}

// CancelWorkItem handles POST /api/projects/:projectId/work-items/:canonicalId/cancel
//
// @Summary      Cancel a work item
// @Description  Closes a work item (blocked) and cancels any in-flight run
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Success      200 {object} APIResponse[WorkItemActionDTO] "Cancelled"
// @Failure      409 {object} apperror.Error "Already done"
// @Router       /api/projects/{projectId}/work-items/{canonicalId}/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelWorkItem(c echo.Context) error {
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	res, err := h.workActions.Cancel(c.Request().Context(), projectID, canonicalID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, SuccessResponse(&WorkItemActionDTO{Item: toWorkItemHeadDTO(res.Head)}))
}
