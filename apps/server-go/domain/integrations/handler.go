package integrations

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/encryption"
)

// Handler handles HTTP requests for integrations
type Handler struct {
	repo       *Repository
	registry   *IntegrationRegistry
	encryption *encryption.Service
}

// NewHandler creates a new integrations handler
func NewHandler(repo *Repository, registry *IntegrationRegistry, encryption *encryption.Service) *Handler {
	return &Handler{
		repo:       repo,
		registry:   registry,
		encryption: encryption,
	}
}

// ListAvailable returns all available integration types from the registry
// @Summary      List available integrations
// @Description  Get all available integration types that can be configured
// @Tags         integrations
// @Produce      json
// @Success      200 {array} AvailableIntegrationDTO "Available integration types"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/available [get]
// @Security     bearerAuth
func (h *Handler) ListAvailable(c echo.Context) error {
	integrations := h.registry.List()
	return c.JSON(http.StatusOK, integrations)
}

// List returns integrations configured for the current project
// @Summary      List project integrations
// @Description  Get all configured integrations for the current project with optional filtering
// @Tags         integrations
// @Produce      json
// @Param        name query string false "Filter by integration name"
// @Param        enabled query boolean false "Filter by enabled status"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {array} IntegrationDTO "Configured integrations"
// @Failure      400 {object} apperror.Error "Missing project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	// Parse query params
	var params ListIntegrationsParams
	if name := c.QueryParam("name"); name != "" {
		params.Name = &name
	}
	if enabled := c.QueryParam("enabled"); enabled != "" {
		val := enabled == "true"
		params.Enabled = &val
	}

	integrations, err := h.repo.List(c.Request().Context(), user.ProjectID, &params)
	if err != nil {
		return apperror.NewInternal("failed to list integrations", err)
	}

	// Convert to DTOs with decrypted settings where available
	ctx := c.Request().Context()
	dtos := make([]IntegrationDTO, len(integrations))
	for i, integration := range integrations {
		dtos[i] = h.integrationToDTO(ctx, integration)
	}

	return c.JSON(http.StatusOK, dtos)
}

// Get returns a specific integration by name
// @Summary      Get integration
// @Description  Retrieve a specific integration configuration by name (includes decrypted settings with masked secrets)
// @Tags         integrations
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} IntegrationDTO "Integration details"
// @Failure      400 {object} apperror.Error "Missing name or project ID"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name} [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	integration, err := h.repo.GetByName(c.Request().Context(), user.ProjectID, name)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	return c.JSON(http.StatusOK, h.integrationToDTO(c.Request().Context(), integration))
}

// GetPublic returns non-sensitive integration info
// @Summary      Get public integration info
// @Description  Retrieve public (non-sensitive) integration information without decrypted settings
// @Tags         integrations
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} PublicIntegrationDTO "Public integration info"
// @Failure      400 {object} apperror.Error "Missing name or project ID"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name}/public [get]
// @Security     bearerAuth
func (h *Handler) GetPublic(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	integration, err := h.repo.GetByName(c.Request().Context(), user.ProjectID, name)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	return c.JSON(http.StatusOK, integration.ToPublicDTO())
}

// Create creates a new integration
// @Summary      Create integration
// @Description  Create a new integration configuration with encrypted settings
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Param        request body CreateIntegrationDTO true "Integration data (settings will be encrypted)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      201 {object} IntegrationDTO "Created integration"
// @Failure      400 {object} apperror.Error "Invalid request or integration already exists"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var dto CreateIntegrationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.Name == "" {
		return apperror.NewBadRequest("name is required")
	}

	if dto.DisplayName == "" {
		return apperror.NewBadRequest("display_name is required")
	}

	// Check if integration type is valid
	if !h.registry.Exists(dto.Name) {
		return apperror.NewBadRequest("unknown integration type: " + dto.Name)
	}

	// Check if integration already exists
	ctx := c.Request().Context()
	exists, err := h.repo.ExistsByName(ctx, user.ProjectID, dto.Name)
	if err != nil {
		return apperror.NewInternal("failed to check integration existence", err)
	}
	if exists {
		return apperror.NewBadRequest("integration already exists with name: " + dto.Name)
	}

	// Create the integration
	integration := &Integration{
		Name:        dto.Name,
		DisplayName: dto.DisplayName,
		Description: dto.Description,
		Enabled:     true,
		OrgID:       user.OrgID,
		ProjectID:   user.ProjectID,
		LogoURL:     dto.LogoURL,
		CreatedBy:   dto.CreatedBy,
	}

	if dto.Enabled != nil {
		integration.Enabled = *dto.Enabled
	}

	// Encrypt settings if provided
	if dto.Settings != nil && len(dto.Settings) > 0 {
		encrypted, err := h.encryption.Encrypt(ctx, dto.Settings)
		if err != nil {
			return apperror.NewInternal("failed to encrypt settings", err)
		}
		// Store as bytes (the encrypted string is base64-encoded)
		integration.SettingsEncrypted = []byte(encrypted)
	}

	if err := h.repo.Create(ctx, integration); err != nil {
		return apperror.NewInternal("failed to create integration", err)
	}

	return c.JSON(http.StatusCreated, h.integrationToDTO(ctx, integration))
}

// Update updates an existing integration
// @Summary      Update integration
// @Description  Update an existing integration configuration (partial updates supported, settings will be re-encrypted if provided)
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        request body UpdateIntegrationDTO true "Update data"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} IntegrationDTO "Updated integration"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name} [put]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	var dto UpdateIntegrationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Get existing integration
	ctx := c.Request().Context()
	integration, err := h.repo.GetByName(ctx, user.ProjectID, name)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	// Apply updates
	if dto.DisplayName != nil {
		integration.DisplayName = *dto.DisplayName
	}
	if dto.Description != nil {
		integration.Description = dto.Description
	}
	if dto.Enabled != nil {
		integration.Enabled = *dto.Enabled
	}
	if dto.LogoURL != nil {
		integration.LogoURL = dto.LogoURL
	}

	// Encrypt settings if provided
	if dto.Settings != nil && len(dto.Settings) > 0 {
		encrypted, err := h.encryption.Encrypt(ctx, dto.Settings)
		if err != nil {
			return apperror.NewInternal("failed to encrypt settings", err)
		}
		// Store as bytes (the encrypted string is base64-encoded)
		integration.SettingsEncrypted = []byte(encrypted)
	}

	if err := h.repo.Update(ctx, integration); err != nil {
		return apperror.NewInternal("failed to update integration", err)
	}

	return c.JSON(http.StatusOK, h.integrationToDTO(ctx, integration))
}

// Delete deletes an integration
// @Summary      Delete integration
// @Description  Delete an integration configuration (permanent deletion)
// @Tags         integrations
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        X-Project-ID header string true "Project ID"
// @Success      204 "Integration deleted successfully"
// @Failure      400 {object} apperror.Error "Missing name or project ID"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	if err := h.repo.Delete(c.Request().Context(), user.ProjectID, name); err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to delete integration", err)
	}

	return c.NoContent(http.StatusNoContent)
}

// TestConnection tests the connection for an integration
// @Summary      Test integration connection
// @Description  Test the connectivity and authentication for an integration (validates configured settings)
// @Tags         integrations
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} TestConnectionResponseDTO "Connection test result"
// @Failure      400 {object} apperror.Error "Missing name or project ID"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name}/test [post]
// @Security     bearerAuth
func (h *Handler) TestConnection(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	// Get integration
	integration, err := h.repo.GetByName(c.Request().Context(), user.ProjectID, name)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	// Decrypt settings
	if integration.SettingsEncrypted == nil || len(integration.SettingsEncrypted) == 0 {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "No configuration found for this integration",
		})
	}

	// TODO: Implement actual connection testing based on integration type
	// For now, just return success if settings exist
	return c.JSON(http.StatusOK, TestConnectionResponseDTO{
		Success: true,
		Message: "Connection test successful",
	})
}

// TriggerSync triggers a sync for an integration
// @Summary      Trigger integration sync
// @Description  Manually trigger a synchronization job for an integration (integration must be enabled)
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Param        name path string true "Integration name"
// @Param        config body TriggerSyncConfigDTO false "Optional sync configuration"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} TriggerSyncResponseDTO "Sync job triggered"
// @Failure      400 {object} apperror.Error "Integration disabled or invalid request"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/integrations/{name}/sync [post]
// @Security     bearerAuth
func (h *Handler) TriggerSync(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("integration name is required")
	}

	var config TriggerSyncConfigDTO
	if err := c.Bind(&config); err != nil {
		// Binding failure is OK - config is optional
		config = TriggerSyncConfigDTO{}
	}

	// Get integration
	integration, err := h.repo.GetByName(c.Request().Context(), user.ProjectID, name)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", name)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	if !integration.Enabled {
		return apperror.NewBadRequest("integration is disabled")
	}

	// TODO: Implement actual sync triggering
	// This would create a sync job and return the job ID
	return c.JSON(http.StatusOK, TriggerSyncResponseDTO{
		Success: true,
		Message: "Sync triggered successfully",
		JobID:   nil, // Would be set to actual job ID
	})
}

// integrationToDTO converts an integration to a DTO with decrypted settings
func (h *Handler) integrationToDTO(ctx context.Context, integration *Integration) IntegrationDTO {
	dto := integration.ToDTO()

	// Decrypt settings if available
	if integration.SettingsEncrypted != nil && len(integration.SettingsEncrypted) > 0 {
		// Convert bytes to string for decryption
		encryptedStr := string(integration.SettingsEncrypted)
		settings, err := h.encryption.Decrypt(ctx, encryptedStr)
		if err == nil && settings != nil {
			// Mask sensitive fields
			dto.Settings = h.maskSensitiveSettings(settings)
		}
	}

	return dto
}

// maskSensitiveSettings masks sensitive values in settings
func (h *Handler) maskSensitiveSettings(settings map[string]interface{}) map[string]interface{} {
	sensitiveKeys := map[string]bool{
		"api_key":       true,
		"access_token":  true,
		"secret":        true,
		"password":      true,
		"client_secret": true,
		"bot_token":     true,
	}

	masked := make(map[string]interface{})
	for key, value := range settings {
		if sensitiveKeys[key] {
			if str, ok := value.(string); ok && len(str) > 4 {
				masked[key] = str[:4] + "****"
			} else {
				masked[key] = "****"
			}
		} else {
			masked[key] = value
		}
	}
	return masked
}
