package market

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// InstallService is the surface the HTTP handler calls. *Service implements it;
// handler tests substitute a fake.
type InstallService interface {
	Resolve(ctx context.Context, projectID, marketIssuer, code string) (*ResolveResult, error)
	Apply(ctx context.Context, projectID, installedBy, marketIssuer, code string, overrides map[string]string) (*ApplyResult, error)
}

// Handler serves the project-scoped market install endpoints.
type Handler struct {
	svc  InstallService
	auth *auth.Middleware
}

func NewHandler(svc InstallService, authMiddleware *auth.Middleware) *Handler {
	return &Handler{svc: svc, auth: authMiddleware}
}

// ResolveRequest is POST /api/market/installs/resolve.
type ResolveRequest struct {
	Market string `json:"market"`
	Code   string `json:"code"`
}

// ApplyRequest is POST /api/market/installs/apply.
type ApplyRequest struct {
	Market         string            `json:"market"`
	Code           string            `json:"code"`
	ProjectID      string            `json:"project_id"`
	ModelOverrides map[string]string `json:"model_overrides,omitempty"`
}

// Resolve handles POST /api/market/installs/resolve.
func (h *Handler) Resolve(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("project context required (X-Project-ID header or API token)")
	}
	var req ResolveRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(req.Market) == "" {
		return apperror.NewBadRequest("market is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return apperror.NewBadRequest("code is required")
	}
	result, err := h.svc.Resolve(c.Request().Context(), user.ProjectID, req.Market, req.Code)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, result)
}

// Apply handles POST /api/market/installs/apply.
func (h *Handler) Apply(c echo.Context) error {
	user := auth.MustGetUser(c)
	var req ApplyRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(req.Market) == "" {
		return apperror.NewBadRequest("market is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return apperror.NewBadRequest("code is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("project_id is required")
	}
	// The body carries the target project, so authorize it explicitly: a
	// project-bound token may only address its own project and a session caller
	// must belong to the owning org.
	if h.auth != nil {
		if err := h.auth.AuthorizeProject(c, projectID); err != nil {
			return err
		}
	}
	result, err := h.svc.Apply(c.Request().Context(), projectID, user.ID, req.Market, req.Code, req.ModelOverrides)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, result)
}
