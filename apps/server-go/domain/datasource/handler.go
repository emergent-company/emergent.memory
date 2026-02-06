package datasource

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/encryption"
	"github.com/emergent/emergent-core/pkg/logger"
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
// Returns all available data source providers
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
// Returns the configuration schema for a provider
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
// Tests a provider configuration without creating an integration
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
// Returns integrations for the current project
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
// Returns source types with document counts
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
// Returns a specific integration by ID
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
// Creates a new integration
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
// Updates an existing integration
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
// Deletes an integration
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
// Tests the connection for an existing integration
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
// Triggers a sync for an integration
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
// Returns sync jobs for an integration
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
// Returns the most recent sync job
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
// Returns a specific sync job
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
// Cancels a running sync job
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
