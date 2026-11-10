package githubapp

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	maxWebhookBody = 5 << 20
	eventHeader    = "X-GitHub-Event"
)

// Handler exposes admin registration and the session-less webhook ingress.
type Handler struct {
	svc *Service
}

// NewHandler builds the HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Webhook handles POST /api/webhooks/github/app. The route is outside session
// auth: the HMAC signature is the trust anchor.
// @Summary      GitHub App webhook ingress
// @Description  Session-less GitHub App delivery endpoint, authenticated by X-Hub-Signature-256.
// @Tags         github-app
// @Accept       json
// @Param        X-GitHub-Event header string true "GitHub event name"
// @Param        X-GitHub-Delivery header string false "Delivery id"
// @Param        X-Hub-Signature-256 header string true "HMAC-SHA256 signature"
// @Success      202
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      503 {object} apperror.Error
// @Router       /api/webhooks/github/app [post]
func (h *Handler) Webhook(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBody))
	if err != nil {
		return c.String(http.StatusBadRequest, "cannot read request body")
	}
	err = h.svc.HandleWebhook(c.Request().Context(),
		c.Request().Header.Get(eventHeader),
		c.Request().Header.Get(DeliveryHeader),
		c.Request().Header.Get(SignatureHeader),
		body,
	)
	switch {
	case errors.Is(err, ErrInvalidSignature):
		return c.String(http.StatusUnauthorized, "invalid signature")
	case errors.Is(err, ErrUnknownInstallation):
		return c.String(http.StatusNotFound, "unknown installation")
	case errors.Is(err, ErrMissingCredentials), errors.Is(err, ErrWebhookNotConfigured):
		return c.String(http.StatusServiceUnavailable, "github app is not configured")
	case err != nil:
		return c.String(http.StatusBadRequest, "cannot process delivery")
	}
	return c.NoContent(http.StatusAccepted)
}

type registerAppDTO struct {
	AppID         int64  `json:"appId"`
	Slug          string `json:"slug"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
}

type createInstallationDTO struct {
	ProjectID      string   `json:"projectId"`
	InstallationID int64    `json:"installationId"`
	Repos          []string `json:"repos"`
	AgentID        string   `json:"agentId"`
	Queue          string   `json:"queue"`
}

type updateInstallationDTO struct {
	Repos   *[]string `json:"repos"`
	AgentID *string   `json:"agentId"`
	Queue   *string   `json:"queue"`
	Enabled *bool     `json:"enabled"`
}

type orgAppResponse struct {
	App           *GitHubApp      `json:"app,omitempty"`
	Installations []*Installation `json:"installations"`
}

func (h *Handler) orgAndUser(c echo.Context) (uuid.UUID, string, error) {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		return uuid.Nil, "", apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	return orgID, user.ID, nil
}

// GetOrgApp handles GET /api/orgs/:orgId/github-app.
// @Summary      Get the org GitHub App connection
// @Description  Returns the org App (no credentials) and its installations. Org admin only.
// @Tags         github-app
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {object} orgAppResponse
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app [get]
// @Security     bearerAuth
func (h *Handler) GetOrgApp(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	view, err := h.svc.ListOrgApp(c.Request().Context(), orgID, userID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, orgAppResponse{App: view.App, Installations: view.Installations})
}

// RegisterApp handles POST /api/orgs/:orgId/github-app.
// @Summary      Register a GitHub App
// @Description  Stores App id, slug, private key, and webhook secret (encrypted). Org admin only.
// @Tags         github-app
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        body body registerAppDTO true "App credentials"
// @Success      201 {object} GitHubApp
// @Failure      400 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app [post]
// @Security     bearerAuth
func (h *Handler) RegisterApp(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	var dto registerAppDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	app, err := h.svc.RegisterApp(c.Request().Context(), orgID, userID, RegisterAppRequest{
		AppID:         dto.AppID,
		Slug:          dto.Slug,
		PrivateKey:    dto.PrivateKey,
		WebhookSecret: dto.WebhookSecret,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, app)
}

// DeleteApp handles DELETE /api/orgs/:orgId/github-app/:appPK.
// @Summary      Delete a GitHub App connection
// @Description  Removes the App and its installations. Org admin only.
// @Tags         github-app
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appPK path string true "App row ID (UUID)"
// @Success      204
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app/{appPK} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteApp(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	appPK, err := uuid.Parse(c.Param("appPK"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	if err := h.svc.DeleteApp(c.Request().Context(), orgID, userID, appPK); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// CreateInstallation handles POST /api/orgs/:orgId/github-app/installations.
// @Summary      Bind a GitHub App installation
// @Description  Binds an installation to a project, repo allowlist, runtime agent, and queue. Org admin only.
// @Tags         github-app
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        body body createInstallationDTO true "Installation binding"
// @Success      201 {object} Installation
// @Failure      400 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app/installations [post]
// @Security     bearerAuth
func (h *Handler) CreateInstallation(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	var dto createInstallationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	projectID, err := uuid.Parse(dto.ProjectID)
	if err != nil {
		return apperror.ErrBadRequest
	}
	agentID, err := uuid.Parse(dto.AgentID)
	if err != nil {
		return apperror.ErrBadRequest
	}
	inst, err := h.svc.CreateInstallation(c.Request().Context(), orgID, userID, CreateInstallationRequest{
		ProjectID:      projectID,
		InstallationID: dto.InstallationID,
		Repos:          dto.Repos,
		AgentID:        agentID,
		Queue:          dto.Queue,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, inst)
}

// UpdateInstallation handles PATCH /api/orgs/:orgId/github-app/installations/:id.
// @Summary      Update an installation binding
// @Description  Patches repos, agent, queue, or enabled. Org admin only.
// @Tags         github-app
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        id path string true "Installation row ID (UUID)"
// @Param        body body updateInstallationDTO true "Patch"
// @Success      200 {object} Installation
// @Failure      400 {object} apperror.Error
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app/installations/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateInstallation(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	var dto updateInstallationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.ErrBadRequest
	}
	req := UpdateInstallationRequest{Repos: dto.Repos, Queue: dto.Queue, Enabled: dto.Enabled}
	if dto.AgentID != nil {
		agentID, err := uuid.Parse(*dto.AgentID)
		if err != nil {
			return apperror.ErrBadRequest
		}
		req.AgentID = &agentID
	}
	inst, err := h.svc.UpdateInstallation(c.Request().Context(), orgID, userID, id, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, inst)
}

// DeleteInstallation handles DELETE /api/orgs/:orgId/github-app/installations/:id.
// @Summary      Delete an installation binding
// @Description  Removes the installation row. Org admin only.
// @Tags         github-app
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        id path string true "Installation row ID (UUID)"
// @Success      204
// @Failure      403 {object} apperror.Error
// @Router       /api/orgs/{orgId}/github-app/installations/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteInstallation(c echo.Context) error {
	orgID, userID, err := h.orgAndUser(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperror.ErrBadRequest
	}
	if err := h.svc.DeleteInstallation(c.Request().Context(), orgID, userID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
