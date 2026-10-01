package agents

import (
	"net/http"
	"strconv"
	"time"

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

// WorkItemDTO is the board-card projection returned by the list endpoint: the
// work object's HEAD joined to its latest run's execution status, plus the
// derived unroutable flag.
type WorkItemDTO struct {
	ProjectID             string `json:"projectId"`
	CanonicalID           string `json:"canonicalId"`
	Type                  string `json:"type"`
	Key                   string `json:"key"`
	Status                string `json:"status"`
	Assignee              string `json:"assignee"`
	Version               int    `json:"version"`
	NeedsReview           bool   `json:"needsReview"`
	UpdatedAt             string `json:"updatedAt"`
	LatestRunStatus       string `json:"latestRunStatus,omitempty"`
	LatestRunFailureClass string `json:"latestRunFailureClass,omitempty"`
	LatestRunID           string `json:"latestRunId,omitempty"`
	RunCount              int    `json:"runCount"`
	Unroutable            bool   `json:"unroutable"`
}

func toWorkItemDTO(it *graph.WorkItem) *WorkItemDTO {
	if it == nil {
		return nil
	}
	return &WorkItemDTO{
		ProjectID:             it.ProjectID,
		CanonicalID:           it.CanonicalID,
		Type:                  it.Type,
		Key:                   it.Key,
		Status:                it.Status,
		Assignee:              it.Assignee,
		Version:               it.Version,
		NeedsReview:           it.NeedsReview,
		UpdatedAt:             it.UpdatedAt.Format(time.RFC3339),
		LatestRunStatus:       it.LatestRunStatus,
		LatestRunFailureClass: it.LatestRunFailureClass,
		LatestRunID:           it.LatestRunID,
		RunCount:              it.RunCount,
		Unroutable:            it.Unroutable,
	}
}

// WorkItemRunDTO is a compact run summary in the work-item detail response.
type WorkItemRunDTO struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  *time.Time     `json:"completedAt"`
	ErrorMessage *string        `json:"errorMessage,omitempty"`
	FailureClass *string        `json:"failureClass,omitempty"`
	Summary      map[string]any `json:"summary,omitempty"`
}

// WorkItemFeedbackDTO is a feedback round in the work-item detail response.
type WorkItemFeedbackDTO struct {
	Round     int    `json:"round"`
	Author    string `json:"author,omitempty"`
	Text      string `json:"text"`
	RunID     string `json:"runId,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// WorkItemDetailDTO is the card-drawer projection: the work item joined to its
// feedback history and recent runs.
type WorkItemDetailDTO struct {
	Item     *WorkItemDTO          `json:"item"`
	Feedback []WorkItemFeedbackDTO `json:"feedback"`
	Runs     []WorkItemRunDTO      `json:"runs"`
}

// GetWorkItem handles GET /api/projects/:projectId/work-items/:canonicalId
//
// @Summary      Get a work item
// @Description  Returns one work item joined to its latest run, its feedback history, and its recent runs
// @Tags         agents
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        canonicalId path string true "Work item canonical ID"
// @Success      200 {object} APIResponse[WorkItemDetailDTO] "Work item detail"
// @Failure      404 {object} apperror.Error "Not found"
// @Router       /api/projects/{projectId}/work-items/{canonicalId} [get]
// @Security     bearerAuth
func (h *Handler) GetWorkItem(c echo.Context) error {
	projectID, canonicalID, err := h.workActionParams(c)
	if err != nil {
		return err
	}
	detail, err := h.workActions.GetWorkItemDetail(c.Request().Context(), projectID, canonicalID)
	if err != nil {
		return err
	}
	dto := &WorkItemDetailDTO{
		Item:     toWorkItemDTO(detail.Item),
		Feedback: make([]WorkItemFeedbackDTO, 0, len(detail.Feedback)),
		Runs:     make([]WorkItemRunDTO, 0, len(detail.Runs)),
	}
	for _, fb := range detail.Feedback {
		if fb == nil {
			continue
		}
		fdto := WorkItemFeedbackDTO{Round: fb.Round, Text: fb.Text, CreatedAt: fb.CreatedAt.Format(time.RFC3339)}
		if fb.Author != nil {
			fdto.Author = *fb.Author
		}
		if fb.RunID != nil {
			fdto.RunID = *fb.RunID
		}
		dto.Feedback = append(dto.Feedback, fdto)
	}
	for _, run := range detail.Runs {
		if run == nil {
			continue
		}
		dto.Runs = append(dto.Runs, WorkItemRunDTO{
			ID:           run.ID,
			Status:       string(run.Status),
			StartedAt:    run.StartedAt,
			CompletedAt:  run.CompletedAt,
			ErrorMessage: run.ErrorMessage,
			FailureClass: run.FailureClass,
			Summary:      run.Summary,
		})
	}
	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// ListWorkItems handles GET /api/projects/:projectId/work-items
//
// @Summary      List work items
// @Description  Returns the Kanban projection of board-enabled work items (object HEAD joined to latest run), filterable by status and type
// @Tags         agents
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        status query string false "Filter by work status"
// @Param        type query string false "Filter by object type"
// @Param        limit query int false "Max items (default 200, max 1000)"
// @Success      200 {object} APIResponse[[]WorkItemDTO] "Work items"
// @Router       /api/projects/{projectId}/work-items [get]
// @Security     bearerAuth
func (h *Handler) ListWorkItems(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	status := c.QueryParam("status")
	typeName := c.QueryParam("type")
	limit := 200
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, 1000)
		}
	}
	items, err := h.workActions.ListWorkItems(c.Request().Context(), projectID, status, typeName, limit)
	if err != nil {
		return err
	}
	out := make([]*WorkItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, toWorkItemDTO(it))
	}
	return c.JSON(http.StatusOK, SuccessResponse(out))
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
