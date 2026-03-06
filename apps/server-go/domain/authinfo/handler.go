package authinfo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles auth introspection HTTP requests
type Handler struct {
	db  bun.IDB
	cfg *config.Config
}

// NewHandler creates a new auth info handler
func NewHandler(db bun.IDB, cfg *config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}

// TokenInfoResponse is the response for GET /api/auth/me
type TokenInfoResponse struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Type        string   `json:"type"`
	ProjectID   string   `json:"project_id,omitempty"`
	ProjectName string   `json:"project_name,omitempty"`
	OrgID       string   `json:"org_id,omitempty"`
	TokenID     string   `json:"token_id,omitempty"`
	TokenName   string   `json:"token_name,omitempty"`
}

// IssuerResponse is the response for GET /api/auth/issuer
type IssuerResponse struct {
	Issuer     string `json:"issuer,omitempty"`
	Standalone bool   `json:"standalone"`
}

// Issuer handles GET /api/auth/issuer (public, no auth required)
// @Summary      OIDC issuer discovery
// @Description  Returns the OIDC issuer URL so CLI clients can discover the correct OAuth endpoints
// @Tags         auth
// @Produce      json
// @Success      200 {object} IssuerResponse "Issuer information"
// @Router       /api/auth/issuer [get]
func (h *Handler) Issuer(c echo.Context) error {
	if h.cfg != nil && h.cfg.Standalone.IsEnabled() {
		return c.JSON(http.StatusOK, IssuerResponse{Standalone: true})
	}

	issuer := ""
	if h.cfg != nil {
		issuer = h.cfg.Zitadel.GetIssuer()
	}

	return c.JSON(http.StatusOK, IssuerResponse{
		Issuer:     issuer,
		Standalone: false,
	})
}

// Me handles GET /api/auth/me
// @Summary      Token introspection
// @Description  Returns metadata about the current authentication token including project/org context
// @Tags         auth
// @Produce      json
// @Success      200 {object} TokenInfoResponse "Token information"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/auth/me [get]
// @Security     bearerAuth
func (h *Handler) Me(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	resp := TokenInfoResponse{
		UserID: user.ID,
		Email:  user.Email,
		Scopes: user.Scopes,
	}

	if user.APITokenID != "" {
		resp.Type = "api_token"
		resp.TokenID = user.APITokenID

		// Look up token name and project details
		if user.APITokenProjectID != "" {
			resp.ProjectID = user.APITokenProjectID

			// Look up token name
			var tokenName string
			err := h.db.NewSelect().
				TableExpr("core.api_tokens").
				Column("name").
				Where("id = ?", user.APITokenID).
				Scan(c.Request().Context(), &tokenName)
			if err == nil {
				resp.TokenName = tokenName
			}

			// Look up project name and org ID
			var projectInfo struct {
				Name           string `bun:"name"`
				OrganizationID string `bun:"organization_id"`
			}
			err = h.db.NewSelect().
				TableExpr("kb.projects").
				Column("name", "organization_id").
				Where("id = ?", user.APITokenProjectID).
				Scan(c.Request().Context(), &projectInfo)
			if err == nil {
				resp.ProjectName = projectInfo.Name
				resp.OrgID = projectInfo.OrganizationID
			}
		}
	} else {
		resp.Type = "session"
		// For session auth, include header-provided context
		if user.ProjectID != "" {
			resp.ProjectID = user.ProjectID
		}
		if user.OrgID != "" {
			resp.OrgID = user.OrgID
		}
	}

	return c.JSON(http.StatusOK, resp)
}
