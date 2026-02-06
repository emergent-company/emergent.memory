package search

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for unified search
type Handler struct {
	svc *Service
}

// NewHandler creates a new search handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Search handles POST /search/unified
// @Summary Unified search
// @Description Search across graph objects and document chunks with configurable fusion strategies
// @Tags search
// @Accept json
// @Produce json
// @Param body body UnifiedSearchRequest true "Search request"
// @Param debug query bool false "If true, includes timing and statistics in response (requires search:debug scope)"
// @Success 200 {object} UnifiedSearchResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /search/unified [post]
func (h *Handler) Search(c echo.Context) error {
	// Get authenticated user
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Get project ID
	projectIDStr, err := auth.GetProjectID(c)
	if err != nil {
		return err
	}

	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	// Get org ID from user context
	var orgID uuid.UUID
	if user.OrgID != "" {
		orgID, err = uuid.Parse(user.OrgID)
		if err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid org ID")
		}
	}

	// Parse request body
	var req UnifiedSearchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate request
	if req.Query == "" {
		return apperror.ErrBadRequest.WithMessage("query is required")
	}
	if len(req.Query) > 800 {
		return apperror.ErrBadRequest.WithMessage("query must be 800 characters or less")
	}

	// Get user scopes
	scopes := user.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	// Determine if debug mode is requested (via body field or query param)
	wantsDebug := req.IncludeDebug || c.QueryParam("debug") == "true"

	// Check debug scope if debug mode requested
	if wantsDebug && !hasScope(scopes, "search:debug") {
		return apperror.ErrForbidden.WithMessage("debug scope required for debug mode")
	}

	// Update request to reflect actual debug mode (handles query param case)
	req.IncludeDebug = wantsDebug

	// Build search context
	searchCtx := &SearchContext{
		OrgID:     orgID,
		ProjectID: projectID,
		Scopes:    scopes,
	}

	// Execute unified search
	response, err := h.svc.Search(c.Request().Context(), projectID, &req, searchCtx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, response)
}

// hasScope checks if the given scope exists in the list
func hasScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}
