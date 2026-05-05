package journal

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for the project journal.
type Handler struct {
	svc         *Service
	branchStore *branches.Store
}

// NewHandler creates a new journal Handler.
func NewHandler(svc *Service, branchStore *branches.Store) *Handler {
	return &Handler{svc: svc, branchStore: branchStore}
}

// ListJournal handles GET /api/graph/journal
// @Summary      List journal entries
// @Description  Returns journal entries and standalone notes for the current project
// @Tags         journal
// @Produce      json
// @Param        since   query string false "ISO-8601 timestamp or relative duration (e.g. 7d, 24h)"
// @Param        limit   query int    false "Max results (default 100)"
// @Param        page    query int    false "Page number (default 1)"
// @Param        branch           query string false "Branch name or UUID (omit for main branch)"
// @Param        include_branches query bool   false "Include merged branches in feed (default false)"
// @Success      200    {object} JournalResponse
// @Failure      400    {object} apperror.Error "Bad request"
// @Failure      401    {object} apperror.Error "Unauthorized"
// @Failure      500    {object} apperror.Error "Internal server error"
// @Router       /api/graph/journal [get]
// @Security     BearerAuth
func (h *Handler) ListJournal(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	params := ListParams{
		ProjectID: projectID,
		Limit:     100,
		Page:      1,
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			params.Limit = l
		}
	}

	if pageStr := c.QueryParam("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			params.Page = p
		}
	}

	if sinceStr := c.QueryParam("since"); sinceStr != "" {
		t, err := parseSince(sinceStr)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid since parameter: use ISO-8601 or relative duration (e.g. 7d, 24h, 30m)")
		}
		params.Since = &t
	}

	if branchStr := c.QueryParam("branch"); branchStr != "" {
		branchID, err := h.resolveBranchID(c, projectID.String(), branchStr)
		if err != nil {
			return err
		}
		params.BranchID = branchID
	}

	if c.QueryParam("include_branches") == "true" {
		params.IncludeBranches = true
	}

	resp, err := h.svc.List(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, resp)
}

// AddNote handles POST /api/graph/journal/notes
// @Summary      Add a journal note
// @Description  Adds a standalone or entry-attached markdown note to the project journal
// @Tags         journal
// @Accept       json
// @Produce      json
// @Param        request body AddNoteRequest true "Note details"
// @Success      201     {object} JournalNote
// @Failure      400     {object} apperror.Error "Bad request"
// @Failure      401     {object} apperror.Error "Unauthorized"
// @Failure      500     {object} apperror.Error "Internal server error"
// @Router       /api/graph/journal/notes [post]
// @Security     BearerAuth
func (h *Handler) AddNote(c echo.Context) error {
	projectID, err := getProjectID(c)
	if err != nil {
		return err
	}

	var req AddNoteRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.Body == "" {
		return apperror.ErrBadRequest.WithMessage("body is required")
	}

	// Set actor from auth context if not specified.
	if req.ActorType == "" {
		user := auth.GetUser(c)
		if user != nil {
			req.ActorType = ActorUser
			id, parseErr := uuid.Parse(user.ID)
			if parseErr == nil {
				req.ActorID = &id
			}
		} else {
			req.ActorType = ActorSystem
		}
	}

	note, err := h.svc.AddNote(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, note)
}

// resolveBranchID resolves a branch name or UUID string to a *uuid.UUID.
// Returns a 400 error if the branch is not found.
func (h *Handler) resolveBranchID(c echo.Context, projectID string, branchStr string) (*uuid.UUID, error) {
	// Try UUID first.
	if id, err := uuid.Parse(branchStr); err == nil {
		return &id, nil
	}
	// Look up by name.
	b, err := h.branchStore.GetByNameAndProject(c.Request().Context(), branchStr, &projectID)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, apperror.ErrBadRequest.WithMessage("branch not found: " + branchStr)
	}
	id, err := uuid.Parse(b.ID)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// getProjectID extracts the project UUID from the auth context.
// Deprecated: Use auth.GetProjectUUID(c) directly.
func getProjectID(c echo.Context) (uuid.UUID, error) {
	return auth.GetProjectUUID(c)
}

// parseSince parses a since string which may be an ISO-8601 timestamp or a
// relative duration like "7d", "24h", "30m".
func parseSince(s string) (time.Time, error) {
	if len(s) >= 2 {
		unit := s[len(s)-1]
		if unit == 'd' || unit == 'h' || unit == 'm' || unit == 's' {
			n, err := strconv.Atoi(s[:len(s)-1])
			if err == nil {
				switch unit {
				case 'd':
					return time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour), nil
				case 'h':
					return time.Now().UTC().Add(-time.Duration(n) * time.Hour), nil
				case 'm':
					return time.Now().UTC().Add(-time.Duration(n) * time.Minute), nil
				case 's':
					return time.Now().UTC().Add(-time.Duration(n) * time.Second), nil
				}
			}
		}
	}
	// Fall back to RFC3339 / ISO-8601
	return time.Parse(time.RFC3339, s)
}
