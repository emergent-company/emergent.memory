package datasource

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/encryption"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Handler handles HTTP requests for data source integrations
type Handler struct {
	repo       *Repository
	jobsSvc    *JobsService
	registry   *ProviderRegistry
	encryption *encryption.Service
	log        *slog.Logger
}

// NewHandler creates a new data source integrations handler
func NewHandler(
	repo *Repository,
	jobsSvc *JobsService,
	registry *ProviderRegistry,
	encryption *encryption.Service,
	log *slog.Logger,
) *Handler {
	return &Handler{
		repo:       repo,
		jobsSvc:    jobsSvc,
		registry:   registry,
		encryption: encryption,
		log:        log.With(logger.Scope("datasource.handler")),
	}
}

// ------------------------------------------------------------------
// Provider Endpoints
// ------------------------------------------------------------------

// ListProviders handles GET /api/data-source-integrations/providers
// @Summary      List data source providers
// @Description  Returns all available data source providers (ClickUp, IMAP, Gmail, Google Drive)
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Success      200 {array} ProviderDTO "List of available providers"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/data-source-integrations/providers [get]
// @Security     bearerAuth
func (h *Handler) ListProviders(c echo.Context) error {
	providers := []ProviderDTO{
		{
			Type:        "clickup",
			Name:        "ClickUp",
			Description: "Sync documents from ClickUp docs",
			SourceType:  "clickup-document",
			Available:   true,
		},
		{
			Type:        "imap",
			Name:        "IMAP Email",
			Description: "Import emails via IMAP",
			SourceType:  "email",
			Available:   true,
		},
		{
			Type:        "gmail_oauth",
			Name:        "Gmail",
			Description: "Import emails from Gmail using OAuth",
			SourceType:  "email",
			Available:   true,
		},
		{
			Type:        "google_drive",
			Name:        "Google Drive",
			Description: "Import documents from Google Drive",
			SourceType:  "drive",
			Available:   true,
		},
	}

	return c.JSON(http.StatusOK, providers)
}

// GetProviderSchema handles GET /api/data-source-integrations/providers/:providerType/schema
// @Summary      Get provider configuration schema
// @Description  Returns the JSON schema for configuring a specific provider type
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        providerType path string true "Provider type" Enums(clickup,imap,gmail_oauth,google_drive)
// @Success      200 {object} ProviderSchemaDTO "Provider configuration schema"
// @Failure      400 {object} apperror.Error "Invalid provider type"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Provider not found"
// @Router       /api/data-source-integrations/providers/{providerType}/schema [get]
// @Security     bearerAuth
func (h *Handler) GetProviderSchema(c echo.Context) error {
	providerType := c.Param("providerType")
	if providerType == "" {
		return apperror.NewBadRequest("provider type is required")
	}

	// Return provider-specific schemas
	var schema ProviderSchemaDTO
	switch providerType {
	case "clickup":
		schema = ProviderSchemaDTO{
			Type: "object",
			Properties: map[string]interface{}{
				"apiKey": map[string]interface{}{
					"type":        "string",
					"title":       "API Key",
					"description": "Your ClickUp personal API token",
				},
				"workspaceId": map[string]interface{}{
					"type":        "string",
					"title":       "Workspace ID",
					"description": "The ClickUp workspace ID to sync from",
				},
			},
			Required: []string{"apiKey"},
		}
	case "imap":
		schema = ProviderSchemaDTO{
			Type: "object",
			Properties: map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"title":       "IMAP Server",
					"description": "IMAP server hostname",
				},
				"port": map[string]interface{}{
					"type":        "integer",
					"title":       "Port",
					"description": "IMAP server port (usually 993 for SSL)",
					"default":     993,
				},
				"username": map[string]interface{}{
					"type":        "string",
					"title":       "Username",
					"description": "Email account username",
				},
				"password": map[string]interface{}{
					"type":        "string",
					"title":       "Password",
					"description": "Email account password",
				},
				"ssl": map[string]interface{}{
					"type":        "boolean",
					"title":       "Use SSL",
					"description": "Use SSL/TLS connection",
					"default":     true,
				},
			},
			Required: []string{"host", "username", "password"},
		}
	case "gmail_oauth":
		schema = ProviderSchemaDTO{
			Type: "object",
			Properties: map[string]interface{}{
				"accessToken": map[string]interface{}{
					"type":        "string",
					"title":       "Access Token",
					"description": "OAuth access token (obtained via OAuth flow)",
				},
				"refreshToken": map[string]interface{}{
					"type":        "string",
					"title":       "Refresh Token",
					"description": "OAuth refresh token (obtained via OAuth flow)",
				},
			},
			Required: []string{"accessToken", "refreshToken"},
		}
	case "google_drive":
		schema = ProviderSchemaDTO{
			Type: "object",
			Properties: map[string]interface{}{
				"accessToken": map[string]interface{}{
					"type":        "string",
					"title":       "Access Token",
					"description": "OAuth access token (obtained via OAuth flow)",
				},
				"refreshToken": map[string]interface{}{
					"type":        "string",
					"title":       "Refresh Token",
					"description": "OAuth refresh token (obtained via OAuth flow)",
				},
				"folderId": map[string]interface{}{
					"type":        "string",
					"title":       "Folder ID",
					"description": "Google Drive folder ID to sync (optional, syncs root if empty)",
				},
			},
			Required: []string{"accessToken", "refreshToken"},
		}
	default:
		return apperror.NewNotFound("Provider", providerType)
	}

	return c.JSON(http.StatusOK, schema)
}

// TestConfig handles POST /api/data-source-integrations/test-config
// @Summary      Test provider configuration
// @Description  Tests a provider configuration without creating an integration
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        request body TestConfigDTO true "Provider type and config to test"
// @Success      200 {object} TestConnectionResponseDTO "Test result"
// @Failure      400 {object} apperror.Error "Invalid request or unknown provider"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/data-source-integrations/test-config [post]
// @Security     bearerAuth
func (h *Handler) TestConfig(c echo.Context) error {
	var dto TestConfigDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.ProviderType == "" {
		return apperror.NewBadRequest("provider type is required")
	}

	if dto.Config == nil {
		return apperror.NewBadRequest("config is required")
	}

	provider, ok := h.registry.Get(dto.ProviderType)
	if !ok {
		return apperror.NewBadRequest("unknown provider type: " + dto.ProviderType)
	}

	// Test connection
	ctx := c.Request().Context()
	err := provider.TestConnection(ctx, ProviderConfig{
		Config: dto.Config,
	})
	if err != nil {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "Connection failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, TestConnectionResponseDTO{
		Success: true,
		Message: "Connection successful",
	})
}

// ------------------------------------------------------------------
// Integration CRUD Endpoints
// ------------------------------------------------------------------

// List handles GET /api/data-source-integrations
// @Summary      List data source integrations
// @Description  Returns all integrations for the current project with optional filtering
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Param        providerType query string false "Filter by provider type"
// @Param        sourceType query string false "Filter by source type"
// @Param        status query string false "Filter by status"
// @Success      200 {array} DataSourceIntegrationDTO "List of integrations"
// @Failure      400 {object} apperror.Error "X-Project-ID header required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations [get]
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
	if providerType := c.QueryParam("providerType"); providerType != "" {
		params.ProviderType = &providerType
	}
	if sourceType := c.QueryParam("sourceType"); sourceType != "" {
		params.SourceType = &sourceType
	}
	if status := c.QueryParam("status"); status != "" {
		params.Status = &status
	}

	integrations, err := h.repo.List(c.Request().Context(), user.ProjectID, &params)
	if err != nil {
		return apperror.NewInternal("failed to list integrations", err)
	}

	dtos := make([]DataSourceIntegrationDTO, len(integrations))
	for i, integration := range integrations {
		dtos[i] = integration.ToDTO()
	}

	return c.JSON(http.StatusOK, dtos)
}

// GetSourceTypes handles GET /api/data-source-integrations/source-types
// @Summary      Get source type document counts
// @Description  Returns document counts grouped by source type for the current project
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {array} SourceTypeDTO "Source types with counts"
// @Failure      400 {object} apperror.Error "X-Project-ID header required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/source-types [get]
// @Security     bearerAuth
func (h *Handler) GetSourceTypes(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	counts, err := h.repo.GetSourceTypeCounts(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to get source type counts", err)
	}

	return c.JSON(http.StatusOK, counts)
}

// Get handles GET /api/data-source-integrations/:id
// @Summary      Get integration by ID
// @Description  Returns a specific data source integration by ID
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} DataSourceIntegrationDTO "Integration details"
// @Failure      400 {object} apperror.Error "Invalid integration ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id} [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	integration, err := h.repo.GetByID(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	return c.JSON(http.StatusOK, integration.ToDTO())
}

// Create handles POST /api/data-source-integrations
// @Summary      Create data source integration
// @Description  Creates a new data source integration with encrypted configuration
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        request body CreateDataSourceIntegrationDTO true "Integration data"
// @Param        X-Project-ID header string true "Project ID"
// @Success      201 {object} DataSourceIntegrationDTO "Created integration"
// @Failure      400 {object} apperror.Error "Invalid request or validation error"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var dto CreateDataSourceIntegrationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.Name == "" {
		return apperror.NewBadRequest("name is required")
	}
	if dto.ProviderType == "" {
		return apperror.NewBadRequest("provider type is required")
	}
	if dto.SourceType == "" {
		return apperror.NewBadRequest("source type is required")
	}

	// Check if provider exists
	if _, ok := h.registry.Get(dto.ProviderType); !ok {
		return apperror.NewBadRequest("unknown provider type: " + dto.ProviderType)
	}

	// Check for duplicate name
	ctx := c.Request().Context()
	exists, err := h.repo.ExistsByName(ctx, user.ProjectID, dto.Name)
	if err != nil {
		return apperror.NewInternal("failed to check integration name", err)
	}
	if exists {
		return apperror.NewBadRequest("integration with this name already exists")
	}

	// Create integration
	integration := &DataSourceIntegration{
		OrganizationID: user.OrgID,
		ProjectID:      user.ProjectID,
		Name:           dto.Name,
		Description:    dto.Description,
		ProviderType:   dto.ProviderType,
		SourceType:     dto.SourceType,
		SyncMode:       SyncModeManual,
		Status:         IntegrationStatusActive,
		Metadata:       make(JSON),
	}

	if dto.SyncMode != nil {
		integration.SyncMode = SyncMode(*dto.SyncMode)
	}
	if dto.SyncIntervalMinutes != nil {
		integration.SyncIntervalMinutes = dto.SyncIntervalMinutes
	}
	if user.ID != "" {
		integration.CreatedBy = &user.ID
	}

	// Encrypt config if provided
	if dto.Config != nil && len(dto.Config) > 0 {
		encrypted, err := h.encryption.Encrypt(ctx, dto.Config)
		if err != nil {
			return apperror.NewInternal("failed to encrypt config", err)
		}
		integration.ConfigEncrypted = &encrypted
	}

	if err := h.repo.Create(ctx, integration); err != nil {
		return apperror.NewInternal("failed to create integration", err)
	}

	return c.JSON(http.StatusCreated, integration.ToDTO())
}

// Update handles PATCH /api/data-source-integrations/:id
// @Summary      Update data source integration
// @Description  Updates an existing integration (partial update with encrypted config)
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        request body UpdateDataSourceIntegrationDTO true "Integration update data"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} DataSourceIntegrationDTO "Updated integration"
// @Failure      400 {object} apperror.Error "Invalid request or validation error"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id} [patch]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	var dto UpdateDataSourceIntegrationDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	ctx := c.Request().Context()
	integration, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	// Apply updates
	if dto.Name != nil {
		// Check for duplicate name (excluding current)
		exists, err := h.repo.ExistsByName(ctx, user.ProjectID, *dto.Name)
		if err != nil {
			return apperror.NewInternal("failed to check integration name", err)
		}
		if exists && *dto.Name != integration.Name {
			return apperror.NewBadRequest("integration with this name already exists")
		}
		integration.Name = *dto.Name
	}
	if dto.Description != nil {
		integration.Description = dto.Description
	}
	if dto.SyncMode != nil {
		integration.SyncMode = SyncMode(*dto.SyncMode)
	}
	if dto.SyncIntervalMinutes != nil {
		integration.SyncIntervalMinutes = dto.SyncIntervalMinutes
	}
	if dto.Enabled != nil {
		if *dto.Enabled {
			integration.Status = IntegrationStatusActive
		} else {
			integration.Status = IntegrationStatusDisabled
		}
	}

	// Encrypt new config if provided
	if dto.Config != nil && len(dto.Config) > 0 {
		encrypted, err := h.encryption.Encrypt(ctx, dto.Config)
		if err != nil {
			return apperror.NewInternal("failed to encrypt config", err)
		}
		integration.ConfigEncrypted = &encrypted
	}

	integration.UpdatedAt = time.Now()

	if err := h.repo.Update(ctx, integration); err != nil {
		return apperror.NewInternal("failed to update integration", err)
	}

	return c.JSON(http.StatusOK, integration.ToDTO())
}

// Delete handles DELETE /api/data-source-integrations/:id
// @Summary      Delete data source integration
// @Description  Deletes a data source integration by ID
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      204 "Integration deleted successfully"
// @Failure      400 {object} apperror.Error "Invalid integration ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	if err := h.repo.Delete(c.Request().Context(), user.ProjectID, id); err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to delete integration", err)
	}

	return c.NoContent(http.StatusNoContent)
}

// ------------------------------------------------------------------
// Integration Operations
// ------------------------------------------------------------------

// TestConnection handles POST /api/data-source-integrations/:id/test-connection
// @Summary      Test integration connection
// @Description  Tests the connection for an existing integration using stored config
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} TestConnectionResponseDTO "Connection test result"
// @Failure      400 {object} apperror.Error "Invalid integration ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/test-connection [post]
// @Security     bearerAuth
func (h *Handler) TestConnection(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	ctx := c.Request().Context()
	integration, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	if integration.ConfigEncrypted == nil || *integration.ConfigEncrypted == "" {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "No configuration found for this integration",
		})
	}

	// Get provider
	provider, ok := h.registry.Get(integration.ProviderType)
	if !ok {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "Provider not available: " + integration.ProviderType,
		})
	}

	// Decrypt config
	config, err := h.encryption.Decrypt(ctx, *integration.ConfigEncrypted)
	if err != nil {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "Failed to decrypt configuration",
		})
	}

	// Test connection
	err = provider.TestConnection(ctx, ProviderConfig{
		IntegrationID: integration.ID,
		ProjectID:     integration.ProjectID,
		Config:        config,
		Metadata:      integration.Metadata,
	})
	if err != nil {
		return c.JSON(http.StatusOK, TestConnectionResponseDTO{
			Success: false,
			Message: "Connection failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, TestConnectionResponseDTO{
		Success: true,
		Message: "Connection successful",
	})
}

// ------------------------------------------------------------------
// Sync Job Endpoints
// ------------------------------------------------------------------

// TriggerSync handles POST /api/data-source-integrations/:id/sync
// @Summary      Trigger data source sync
// @Description  Triggers a sync job for an integration (manual or full sync)
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        request body TriggerSyncDTO false "Sync options (configurationId, fullSync, options)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} TriggerSyncResponseDTO "Sync job created or already running"
// @Failure      400 {object} apperror.Error "Invalid request or integration disabled"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/sync [post]
// @Security     bearerAuth
func (h *Handler) TriggerSync(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	var dto TriggerSyncDTO
	if err := c.Bind(&dto); err != nil {
		// Binding failure is OK - dto is optional
		dto = TriggerSyncDTO{}
	}

	ctx := c.Request().Context()
	integration, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	if integration.Status == IntegrationStatusDisabled {
		return apperror.NewBadRequest("integration is disabled")
	}

	// Check for existing active job
	activeJob, err := h.jobsSvc.GetActiveJobForIntegration(ctx, integration.ID)
	if err != nil {
		return apperror.NewInternal("failed to check active jobs", err)
	}
	if activeJob != nil {
		return c.JSON(http.StatusOK, TriggerSyncResponseDTO{
			Success: false,
			Message: "A sync is already in progress",
			JobID:   &activeJob.ID,
		})
	}

	// Create sync job
	jobID := uuid.New().String()
	job := &DataSourceSyncJob{
		ID:            jobID,
		IntegrationID: integration.ID,
		ProjectID:     integration.ProjectID,
		Status:        JobStatusPending,
		TriggerType:   TriggerTypeManual,
		MaxRetries:    3,
		SyncOptions:   make(JSON),
	}

	if dto.ConfigurationID != nil {
		job.ConfigurationID = dto.ConfigurationID
	}
	if user.ID != "" {
		job.TriggeredBy = &user.ID
	}
	if dto.Options != nil {
		job.SyncOptions = dto.Options
	}
	if dto.FullSync {
		job.SyncOptions["fullSync"] = true
	}

	if err := h.jobsSvc.Create(ctx, job); err != nil {
		return apperror.NewInternal("failed to create sync job", err)
	}

	return c.JSON(http.StatusOK, TriggerSyncResponseDTO{
		Success: true,
		Message: "Sync job created",
		JobID:   &jobID,
	})
}

// ListSyncJobs handles GET /api/data-source-integrations/:id/sync-jobs
// @Summary      List sync jobs for integration
// @Description  Returns all sync jobs for a specific integration with optional status filter
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        status query string false "Filter by job status"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} object{items=[]SyncJobDTO,total=int} "Sync jobs list with total count"
// @Failure      400 {object} apperror.Error "Invalid integration ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/sync-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListSyncJobs(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	ctx := c.Request().Context()

	// Verify integration exists and belongs to project
	_, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	// Parse query params
	var params ListSyncJobsParams
	if status := c.QueryParam("status"); status != "" {
		params.Status = &status
	}

	jobs, total, err := h.repo.ListSyncJobs(ctx, id, &params)
	if err != nil {
		return apperror.NewInternal("failed to list sync jobs", err)
	}

	dtos := make([]SyncJobDTO, len(jobs))
	for i, job := range jobs {
		dtos[i] = job.ToDTO()
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"items": dtos,
		"total": total,
	})
}

// GetLatestSyncJob handles GET /api/data-source-integrations/:id/sync-jobs/latest
// @Summary      Get latest sync job
// @Description  Returns the most recent sync job for an integration
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} SyncJobDTO "Latest sync job (null if no jobs exist)"
// @Failure      400 {object} apperror.Error "Invalid integration ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/sync-jobs/latest [get]
// @Security     bearerAuth
func (h *Handler) GetLatestSyncJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("integration ID is required")
	}

	ctx := c.Request().Context()

	// Verify integration exists and belongs to project
	_, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	job, err := h.repo.GetLatestSyncJob(ctx, id)
	if err != nil {
		return apperror.NewInternal("failed to get latest sync job", err)
	}

	if job == nil {
		return c.JSON(http.StatusOK, nil)
	}

	return c.JSON(http.StatusOK, job.ToDTO())
}

// GetSyncJob handles GET /api/data-source-integrations/:id/sync-jobs/:jobId
// @Summary      Get sync job by ID
// @Description  Returns a specific sync job by ID
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        jobId path string true "Sync job ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} SyncJobDTO "Sync job details"
// @Failure      400 {object} apperror.Error "Invalid integration ID or job ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration or sync job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/sync-jobs/{jobId} [get]
// @Security     bearerAuth
func (h *Handler) GetSyncJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	jobID := c.Param("jobId")
	if id == "" || jobID == "" {
		return apperror.NewBadRequest("integration ID and job ID are required")
	}

	ctx := c.Request().Context()

	// Verify integration exists and belongs to project
	_, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	job, err := h.repo.GetSyncJob(ctx, id, jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return apperror.NewNotFound("SyncJob", jobID)
		}
		return apperror.NewInternal("failed to get sync job", err)
	}

	return c.JSON(http.StatusOK, job.ToDTO())
}

// CancelSyncJob handles POST /api/data-source-integrations/:id/sync-jobs/:jobId/cancel
// @Summary      Cancel sync job
// @Description  Cancels a running or pending sync job
// @Tags         datasource
// @Accept       json
// @Produce      json
// @Param        id path string true "Integration ID (UUID)"
// @Param        jobId path string true "Sync job ID (UUID)"
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} object{success=bool,message=string} "Job cancelled successfully"
// @Failure      400 {object} apperror.Error "Invalid ID or job cannot be cancelled"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Integration or sync job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/data-source-integrations/{id}/sync-jobs/{jobId}/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelSyncJob(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	jobID := c.Param("jobId")
	if id == "" || jobID == "" {
		return apperror.NewBadRequest("integration ID and job ID are required")
	}

	ctx := c.Request().Context()

	// Verify integration exists and belongs to project
	_, err := h.repo.GetByID(ctx, user.ProjectID, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return apperror.NewNotFound("Integration", id)
		}
		return apperror.NewInternal("failed to get integration", err)
	}

	// Verify job exists
	job, err := h.repo.GetSyncJob(ctx, id, jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return apperror.NewNotFound("SyncJob", jobID)
		}
		return apperror.NewInternal("failed to get sync job", err)
	}

	// Check if job can be cancelled
	if job.Status != JobStatusPending && job.Status != JobStatusRunning {
		return apperror.NewBadRequest("job cannot be cancelled in current status: " + string(job.Status))
	}

	if err := h.jobsSvc.MarkCancelled(ctx, jobID); err != nil {
		return apperror.NewInternal("failed to cancel job", err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Job cancelled",
	})
}
