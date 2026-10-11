package sources

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for data-source integrations.
type Handler struct {
	svc *Service
}

// NewHandler creates a new sources handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListProviders handles GET /api/data-source-integrations/providers.
// @Summary      List data-source providers
// @Description  Returns the available data-source providers (e.g. Google Drive)
// @Tags         data-source-integrations
// @Produce      json
// @Success      200 {array} Provider "Available providers"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/providers [get]
// @Security     bearerAuth
func (h *Handler) ListProviders(c echo.Context) error {
	return c.JSON(http.StatusOK, h.svc.ListProviders(c.Request().Context()))
}

// GetProviderSchema handles GET /api/data-source-integrations/providers/:providerType/schema.
// @Summary      Get provider config schema
// @Description  Returns the JSON config schema for a provider
// @Tags         data-source-integrations
// @Produce      json
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Success      200 {object} ProviderSchema "Provider config schema"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Unknown provider"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/providers/{providerType}/schema [get]
// @Security     bearerAuth
func (h *Handler) GetProviderSchema(c echo.Context) error {
	schema, err := h.svc.GetProviderSchema(c.Request().Context(), c.Param("providerType"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, schema)
}

// ListSourceTypes handles GET /api/data-source-integrations/source-types.
// @Summary      List source types
// @Description  Returns document counts grouped by source type for the project
// @Tags         data-source-integrations
// @Produce      json
// @Success      200 {array} SourceType "Source types with counts"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/source-types [get]
// @Security     bearerAuth
func (h *Handler) ListSourceTypes(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	sourceTypes, err := h.svc.ListSourceTypes(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, sourceTypes)
}

// TestConfig handles POST /api/data-source-integrations/test-config.
// @Summary      Test an unsaved provider config
// @Description  Validates credentials in an unsaved config against the provider
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        request body TestConfigRequest true "Provider config to test"
// @Success      200 {object} TestConnectionResponse "Test result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/test-config [post]
// @Security     bearerAuth
func (h *Handler) TestConfig(c echo.Context) error {
	var req TestConfigRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	resp, err := h.svc.TestConfig(c.Request().Context(), &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// TestConnection handles POST /api/data-source-integrations/:id/test-connection.
// @Summary      Test a stored source connection
// @Description  Decrypts the stored credentials and performs a provider call
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {object} TestConnectionResponse "Test result"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/test-connection [post]
// @Security     bearerAuth
func (h *Handler) TestConnection(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.TestConnection(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// ListResources handles GET /api/data-source-integrations/:id/resources.
// @Summary      List a source's selectable resources
// @Description  Lists the sync targets (Drive folders / GitHub repositories) for a source
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {array} Resource "Selectable resources"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/resources [get]
// @Security     bearerAuth
func (h *Handler) ListResources(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	resources, err := h.svc.ListResources(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resources)
}

// List handles GET /api/data-source-integrations.
// @Summary      List data-source integrations
// @Description  Returns the project's data-source integrations
// @Tags         data-source-integrations
// @Produce      json
// @Success      200 {array} Integration "Integrations"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	items, err := h.svc.List(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, items)
}

// Create handles POST /api/data-source-integrations.
// @Summary      Create a data-source integration
// @Description  Creates a project-scoped source with encrypted credentials
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        request body CreateIntegrationRequest true "Integration to create"
// @Success      201 {object} Integration "Created integration"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	var req CreateIntegrationRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	created, err := h.svc.Create(c.Request().Context(), projectID, &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, created)
}

// Get handles GET /api/data-source-integrations/:id.
// @Summary      Get a data-source integration
// @Description  Returns one project-scoped integration (credentials redacted)
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {object} Integration "Integration"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id} [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	item, err := h.svc.Get(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, item)
}

// Update handles PATCH /api/data-source-integrations/:id.
// @Summary      Update a data-source integration
// @Description  Updates a project-scoped integration; credentials are re-encrypted when supplied
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        request body UpdateIntegrationRequest true "Fields to update"
// @Success      200 {object} Integration "Updated integration"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id} [patch]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	var req UpdateIntegrationRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	updated, err := h.svc.Update(c.Request().Context(), projectID, id, &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, updated)
}

// Delete handles DELETE /api/data-source-integrations/:id.
// @Summary      Delete a data-source integration
// @Description  Deletes a project-scoped integration; its documents are retained
// @Tags         data-source-integrations
// @Param        id path string true "Source ID (UUID)"
// @Success      204 "Deleted"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.Request().Context(), projectID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// projectID resolves the addressed project from the authenticated user
// (X-Project-ID header or project-bound token), mirroring the header-scoped
// domains.
func (h *Handler) projectID(c echo.Context) (uuid.UUID, error) {
	projectIDStr, err := auth.GetProjectID(c)
	if err != nil {
		return uuid.UUID{}, err
	}
	id, err := uuid.Parse(projectIDStr)
	if err != nil {
		return uuid.UUID{}, apperror.ErrBadRequest
	}
	return id, nil
}

// pathID parses the :id path parameter as a UUID.
func (h *Handler) pathID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.UUID{}, apperror.ErrBadRequest
	}
	return id, nil
}

// pathJobID parses the :jobId path parameter as a UUID.
func (h *Handler) pathJobID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("jobId"))
	if err != nil {
		return uuid.UUID{}, apperror.ErrBadRequest
	}
	return id, nil
}

// TriggerSync handles POST /api/data-source-integrations/:id/sync.
// @Summary      Trigger a sync
// @Description  Admits a manual sync job for a source; returns the active job when one is already running
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {object} TriggerSyncResponse "Sync admitted or existing job"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/sync [post]
// @Security     bearerAuth
func (h *Handler) TriggerSync(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.TriggerSync(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// ListSyncJobs handles GET /api/data-source-integrations/:id/sync-jobs.
// @Summary      List sync jobs
// @Description  Returns a source's sync jobs, newest first (optional ?status= filter)
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        status query string false "Filter by status"
// @Success      200 {object} ListSyncJobsResponse "Sync jobs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/sync-jobs [get]
// @Security     bearerAuth
func (h *Handler) ListSyncJobs(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	var status *string
	if s := c.QueryParam("status"); s != "" {
		status = &s
	}
	resp, err := h.svc.ListSyncJobs(c.Request().Context(), projectID, id, status)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// GetLatestSyncJob handles GET /api/data-source-integrations/:id/sync-jobs/latest.
// @Summary      Get latest sync job
// @Description  Returns the most recent sync job for a source (null when none)
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Success      200 {object} SyncJobDTO "Latest sync job (or null)"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Source not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/sync-jobs/latest [get]
// @Security     bearerAuth
func (h *Handler) GetLatestSyncJob(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.GetLatestSyncJob(c.Request().Context(), projectID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// GetSyncJob handles GET /api/data-source-integrations/:id/sync-jobs/:jobId.
// @Summary      Get a sync job
// @Description  Returns one sync job scoped to the source
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        jobId path string true "Job ID (UUID)"
// @Success      200 {object} SyncJobDTO "Sync job"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/sync-jobs/{jobId} [get]
// @Security     bearerAuth
func (h *Handler) GetSyncJob(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	jobID, err := h.pathJobID(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.GetSyncJob(c.Request().Context(), projectID, id, jobID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// CancelSyncJob handles POST /api/data-source-integrations/:id/sync-jobs/:jobId/cancel.
// @Summary      Cancel a sync job
// @Description  Cancels an active (pending/processing) sync job
// @Tags         data-source-integrations
// @Produce      json
// @Param        id path string true "Source ID (UUID)"
// @Param        jobId path string true "Job ID (UUID)"
// @Success      200 {object} CancelSyncJobResponse "Cancelled"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Job not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/{id}/sync-jobs/{jobId}/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelSyncJob(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	id, err := h.pathID(c)
	if err != nil {
		return err
	}
	jobID, err := h.pathJobID(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.CancelSyncJob(c.Request().Context(), projectID, id, jobID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
