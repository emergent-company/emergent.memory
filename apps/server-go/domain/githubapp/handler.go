package githubapp

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles GitHub App HTTP requests.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates a new GitHub App handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("component", "githubapp-handler")}
}

// GetStatus handles GET /api/v1/settings/github
// Returns the current GitHub App connection status.
//
// @Summary      Get GitHub App connection status
// @Tags         github
// @Produce      json
// @Success      200
// @Router       /api/v1/settings/github [get]
func (h *Handler) GetStatus(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	status, err := h.svc.GetStatus(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to get GitHub status", err)
	}

	return c.JSON(http.StatusOK, status)
}

// Connect handles POST /api/v1/settings/github/connect
// Generates a GitHub App manifest and returns the redirect URL.
//
// @Summary      Start GitHub App connection flow
// @Tags         github
// @Accept       json
// @Produce      json
// @Success      200
// @Router       /api/v1/settings/github/connect [post]
func (h *Handler) Connect(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req ConnectRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		// Derive from request host
		scheme := "https"
		if c.Request().TLS == nil {
			scheme = c.Scheme()
		}
		redirectURL = fmt.Sprintf("%s://%s/api/v1/settings/github/callback", scheme, c.Request().Host)
	}

	manifestURL, err := h.svc.GenerateManifestURL(redirectURL)
	if err != nil {
		return apperror.NewInternal("failed to generate manifest URL", err)
	}

	return c.JSON(http.StatusOK, &ConnectResponse{
		ManifestURL: manifestURL,
	})
}

// Callback handles GET /api/v1/settings/github/callback
// Exchanges the temporary code for GitHub App credentials.
//
// @Summary      Handle GitHub App manifest callback
// @Tags         github
// @Produce      json
// @Param        code  query  string  true  "Temporary code from GitHub"
// @Success      200
// @Router       /api/v1/settings/github/callback [get]
func (h *Handler) Callback(c echo.Context) error {
	code := c.QueryParam("code")
	if code == "" {
		return apperror.NewBadRequest("code parameter is required")
	}

	ownerID := ""
	if user := auth.GetUser(c); user != nil {
		ownerID = user.ID
	}

	err := h.svc.HandleCallback(c.Request().Context(), code, ownerID)
	if err != nil {
		h.log.Error("GitHub callback failed", "error", err)
		return apperror.NewInternal("GitHub App setup failed", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "GitHub App connected successfully. Install the app on your organization to enable repository access.",
	})
}

// Disconnect handles DELETE /api/v1/settings/github
// Removes all GitHub App credentials.
//
// @Summary      Disconnect GitHub App
// @Tags         github
// @Produce      json
// @Success      200
// @Router       /api/v1/settings/github [delete]
func (h *Handler) Disconnect(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	err := h.svc.Disconnect(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to disconnect GitHub", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "GitHub App disconnected",
	})
}

// CLISetup handles POST /api/v1/settings/github/cli
// Accepts app_id, PEM, and installation_id from CLI setup.
//
// @Summary      Configure GitHub App via CLI
// @Tags         github
// @Accept       json
// @Produce      json
// @Success      200
// @Router       /api/v1/settings/github/cli [post]
func (h *Handler) CLISetup(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CLISetupRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	if req.AppID <= 0 {
		return apperror.NewBadRequest("app_id is required and must be positive")
	}
	if req.PrivateKeyPEM == "" {
		return apperror.NewBadRequest("private_key_pem is required")
	}
	if req.InstallationID <= 0 {
		return apperror.NewBadRequest("installation_id is required and must be positive")
	}

	ownerID := user.ID
	err := h.svc.CLISetup(c.Request().Context(), &req, ownerID)
	if err != nil {
		return apperror.NewInternal("CLI setup failed", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "GitHub App configured via CLI",
	})
}

// Webhook handles POST /api/v1/settings/github/webhook
// Processes GitHub webhook events (installation.created, etc.).
//
// @Summary      Handle GitHub webhook events
// @Tags         github
// @Accept       json
// @Produce      json
// @Success      200
// @Router       /api/v1/settings/github/webhook [post]
func (h *Handler) Webhook(c echo.Context) error {
	eventType := c.Request().Header.Get("X-GitHub-Event")
	if eventType == "" {
		return apperror.NewBadRequest("missing X-GitHub-Event header")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return apperror.NewBadRequest("failed to read request body")
	}

	// Verify webhook signature (X-Hub-Signature-256) against stored webhook secret.
	signature := c.Request().Header.Get("X-Hub-Signature-256")
	if signature == "" {
		h.log.Warn("webhook request missing X-Hub-Signature-256 header")
		return c.JSON(http.StatusForbidden, map[string]string{"error": "missing signature"})
	}

	if err := h.svc.VerifyWebhookSignature(c.Request().Context(), signature, body); err != nil {
		h.log.Warn("webhook signature verification failed", "error", err)
		return c.JSON(http.StatusForbidden, map[string]string{"error": "invalid signature"})
	}

	// Only handle installation events
	if eventType != "installation" {
		h.log.Debug("ignoring webhook event", "event", eventType)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored"})
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return apperror.NewBadRequest("invalid webhook payload")
	}

	if event.Action == "created" && event.Installation != nil {
		org := ""
		if event.Installation.Account != nil {
			org = event.Installation.Account.Login
		}

		err := h.svc.HandleInstallation(
			c.Request().Context(),
			event.Installation.AppID,
			event.Installation.ID,
			org,
		)
		if err != nil {
			h.log.Error("failed to handle installation webhook",
				"app_id", event.Installation.AppID,
				"installation_id", event.Installation.ID,
				"error", err,
			)
			// Still return 200 to GitHub (webhook must not fail)
		} else {
			h.log.Info("GitHub App installation recorded",
				"app_id", event.Installation.AppID,
				"installation_id", event.Installation.ID,
				"org", org,
			)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
