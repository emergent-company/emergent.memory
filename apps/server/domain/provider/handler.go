package provider

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles HTTP requests for the provider domain.
type Handler struct {
	creds   *CredentialService
	catalog *ModelCatalogService
	repo    *Repository
}

// NewHandler creates a new provider handler.
func NewHandler(creds *CredentialService, catalog *ModelCatalogService, repo *Repository) *Handler {
	return &Handler{creds: creds, catalog: catalog, repo: repo}
}

// --- Organization Provider Config Endpoints ---

// SaveOrgConfig stores provider credentials and model selections for an organization.
// @Summary Configure org-level provider
// @Param orgId path string true "Organization ID"
// @Param provider path string true "Provider name (google or google-vertex)"
// @Param body body UpsertProviderConfigRequest true "Provider config"
// @Success 200 {object} ProviderConfigResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/providers/{provider} [put]
func (h *Handler) SaveOrgConfig(c echo.Context) error {
	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("orgId is required")
	}
	provider := ProviderType(c.Param("provider"))

	var req UpsertProviderConfigRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Inject orgID into context when X-Org-ID header is absent.
	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	resp, err := h.creds.UpsertOrgConfig(ctx, orgID, provider, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// GetOrgConfig returns the stored config metadata (no secrets) for an org's provider.
// @Summary Get org-level provider config
// @Param orgId path string true "Organization ID"
// @Param provider path string true "Provider name"
// @Success 200 {object} ProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /organizations/{orgId}/providers/{provider} [get]
func (h *Handler) GetOrgConfig(c echo.Context) error {
	orgID := c.Param("orgId")
	provider := ProviderType(c.Param("provider"))

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	resp, err := h.creds.GetOrgConfig(ctx, orgID, provider)
	if err != nil {
		return err
	}
	if resp == nil {
		return apperror.ErrNotFound.WithMessage("no provider config found for this organization and provider")
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteOrgConfig removes a provider config for an organization.
// @Summary Delete org-level provider config
// @Param orgId path string true "Organization ID"
// @Param provider path string true "Provider name"
// @Success 200 {object} map[string]string
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/providers/{provider} [delete]
func (h *Handler) DeleteOrgConfig(c echo.Context) error {
	orgID := c.Param("orgId")
	provider := ProviderType(c.Param("provider"))

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	if err := h.creds.DeleteOrgConfig(ctx, orgID, provider); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// ListOrgConfigs returns all provider configs (metadata only) for an organization.
// @Summary List org-level provider configs
// @Param orgId path string true "Organization ID"
// @Success 200 {array} ProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/providers [get]
func (h *Handler) ListOrgConfigs(c echo.Context) error {
	orgID := c.Param("orgId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	resp, err := h.creds.ListOrgConfigs(ctx, orgID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// --- Project Provider Config Endpoints ---

// ListProjectConfigs returns all project-level provider configs for projects in an org.
// @Summary List project-level provider config overrides for an org
// @Param orgId path string true "Organization ID"
// @Success 200 {array} ProjectProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/project-providers [get]
func (h *Handler) ListProjectConfigs(c echo.Context) error {
	orgID := c.Param("orgId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	resp, err := h.creds.ListProjectConfigsByOrg(ctx, orgID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// SaveProjectConfig stores provider credentials and model selections for a project.
// @Summary Configure project-level provider
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name (google or google-vertex)"
// @Param body body UpsertProviderConfigRequest true "Provider config"
// @Success 200 {object} ProviderConfigResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/providers/{provider} [put]
func (h *Handler) SaveProjectConfig(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}
	provider := ProviderType(c.Param("provider"))

	var req UpsertProviderConfigRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Inject the project's org into context when the caller is using a project
	// token (which doesn't carry an OrgID). This mirrors the pattern used for
	// org-scoped endpoints and satisfies assertCallerOwnsProject.
	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		orgID, err := h.creds.repo.GetOrgIDForProject(ctx, projectID)
		if err == nil && orgID != "" {
			ctx = auth.ContextWithOrgID(ctx, orgID)
		}
	}

	resp, err := h.creds.UpsertProjectConfig(ctx, projectID, provider, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// GetProjectConfig returns the stored config metadata (no secrets) for a project's provider.
// @Summary Get project-level provider config
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name"
// @Success 200 {object} ProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Failure 404 {object} apperror.Error
// @Router /projects/{projectId}/providers/{provider} [get]
func (h *Handler) GetProjectConfig(c echo.Context) error {
	projectID := c.Param("projectId")
	provider := ProviderType(c.Param("provider"))

	resp, err := h.creds.GetProjectConfig(c.Request().Context(), projectID, provider)
	if err != nil {
		return err
	}
	if resp == nil {
		return apperror.ErrNotFound.WithMessage("no provider config found for this project and provider")
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteProjectConfig removes a provider config for a project.
// @Summary Delete project-level provider config
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name"
// @Success 200 {object} map[string]string
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/providers/{provider} [delete]
func (h *Handler) DeleteProjectConfig(c echo.Context) error {
	projectID := c.Param("projectId")
	provider := ProviderType(c.Param("provider"))

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		orgID, err := h.creds.repo.GetOrgIDForProject(ctx, projectID)
		if err == nil && orgID != "" {
			ctx = auth.ContextWithOrgID(ctx, orgID)
		}
	}

	if err := h.creds.DeleteProjectConfig(ctx, projectID, provider); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Model Catalog ---

// ListModels returns the cached model catalog for a provider.
// @Summary List available models for a provider
// @Param provider path string true "Provider name"
// @Param type query string false "Filter by model type (embedding or generative)"
// @Success 200 {array} ProviderSupportedModel
// @Failure 401 {object} apperror.Error
// @Router /providers/{provider}/models [get]
func (h *Handler) ListModels(c echo.Context) error {
	provider := ProviderType(c.Param("provider"))

	var modelType *ModelType
	if mt := c.QueryParam("type"); mt != "" {
		t := ModelType(mt)
		modelType = &t
	}

	models, err := h.catalog.ListModels(c.Request().Context(), provider, modelType)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, models)
}

// --- Usage & Cost Summary ---

// GetProjectUsageSummary returns aggregated token usage and estimated costs for a project.
// @Summary Get project LLM usage summary
// @Param projectId path string true "Project ID"
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} UsageSummaryResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/usage [get]
func (h *Handler) GetProjectUsageSummary(c echo.Context) error {
	projectID := c.Param("projectId")

	// Inject the project's org into context when the caller is using a project
	// token (which doesn't carry an OrgID). This satisfies assertCallerOwnsProject.
	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		orgID, err := h.creds.repo.GetOrgIDForProject(ctx, projectID)
		if err == nil && orgID != "" {
			ctx = auth.ContextWithOrgID(ctx, orgID)
		}
	}

	if err := h.creds.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetProjectUsageSummary(ctx, projectID, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageSummaryResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// GetOrgUsageSummary returns aggregated token usage and estimated costs for an organization.
// @Summary Get org LLM usage summary
// @Param orgId path string true "Organization ID"
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} UsageSummaryResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/usage [get]
func (h *Handler) GetOrgUsageSummary(c echo.Context) error {
	orgID := c.Param("orgId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetOrgUsageSummary(ctx, orgID, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageSummaryResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// GetProjectUsageTimeSeries returns time-bucketed usage for a project.
// @Summary Get project LLM usage time series
// @Param projectId path string true "Project ID"
// @Param granularity query string false "Bucket size: day (default), week, or month"
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} UsageTimeSeriesResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/usage/timeseries [get]
func (h *Handler) GetProjectUsageTimeSeries(c echo.Context) error {
	projectID := c.Param("projectId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		orgID, err := h.creds.repo.GetOrgIDForProject(ctx, projectID)
		if err == nil && orgID != "" {
			ctx = auth.ContextWithOrgID(ctx, orgID)
		}
	}

	if err := h.creds.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}

	granularity := c.QueryParam("granularity")
	since, until := parseTimeRange(c)
	rows, err := h.repo.GetProjectUsageTimeSeries(ctx, projectID, granularity, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageTimeSeriesResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// GetOrgUsageTimeSeries returns time-bucketed usage for an organization.
// @Summary Get org LLM usage time series
// @Param orgId path string true "Organization ID"
// @Param granularity query string false "Bucket size: day (default), week, or month"
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} UsageTimeSeriesResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/usage/timeseries [get]
func (h *Handler) GetOrgUsageTimeSeries(c echo.Context) error {
	orgID := c.Param("orgId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}

	granularity := c.QueryParam("granularity")
	since, until := parseTimeRange(c)
	rows, err := h.repo.GetOrgUsageTimeSeries(ctx, orgID, granularity, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageTimeSeriesResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// GetOrgUsageByProject returns aggregated usage for an org broken down by project.
// @Summary Get org LLM usage by project
// @Param orgId path string true "Organization ID"
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} OrgUsageByProjectResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/usage/by-project [get]
func (h *Handler) GetOrgUsageByProject(c echo.Context) error {
	orgID := c.Param("orgId")

	ctx := c.Request().Context()
	if auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	if err := assertCallerOwnsOrg(ctx, orgID); err != nil {
		return err
	}

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetOrgUsageByProject(ctx, orgID, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, OrgUsageByProjectResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// UsageSummaryResponse wraps usage rows with a note that costs are estimates.
type UsageSummaryResponse struct {
	Note string            `json:"note"`
	Data []UsageSummaryRow `json:"data"`
}

// UsageTimeSeriesResponse wraps time-series rows with a disclaimer note.
type UsageTimeSeriesResponse struct {
	Note string               `json:"note"`
	Data []UsageTimeSeriesRow `json:"data"`
}

// OrgUsageByProjectResponse wraps per-project rows with a disclaimer note.
type OrgUsageByProjectResponse struct {
	Note string                 `json:"note"`
	Data []OrgUsageByProjectRow `json:"data"`
}

// parseTimeRange extracts optional ?since= and ?until= query params.
func parseTimeRange(c echo.Context) (since, until *time.Time) {
	if s := c.QueryParam("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = &t
		}
	}
	if u := c.QueryParam("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = &t
		}
	}
	return since, until
}

// TestProviderResponse is the response body for the provider test endpoint.
type TestProviderResponse struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reply     string `json:"reply"`
	LatencyMs int64  `json:"latencyMs"`
}

// TestProvider sends a live "hello" generate call to verify provider credentials work end-to-end.
// @Summary Test a provider with a live generate call
// @Param provider path string true "Provider name (google or google-vertex)"
// @Param projectId query string false "Project ID for credential resolution"
// @Param orgId query string false "Org ID for credential resolution"
// @Success 200 {object} TestProviderResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Router /providers/{provider}/test [post]
func (h *Handler) TestProvider(c echo.Context) error {
	providerParam := c.Param("provider")
	if providerParam != string(ProviderGoogleAI) &&
		providerParam != string(ProviderVertexAI) {
		return apperror.ErrBadRequest.WithMessage("provider must be google or google-vertex")
	}
	p := ProviderType(providerParam)

	ctx := c.Request().Context()
	if projectID := c.QueryParam("projectId"); projectID != "" {
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}
	if orgID := c.QueryParam("orgId"); orgID != "" && auth.OrgIDFromContext(ctx) == "" {
		ctx = auth.ContextWithOrgID(ctx, orgID)
	}

	cred, err := h.creds.Resolve(ctx, p)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("failed to resolve credentials: " + err.Error())
	}
	if cred == nil {
		return apperror.ErrBadRequest.WithMessage("no credentials configured for provider " + providerParam)
	}

	start := time.Now()
	model, reply, err := h.catalog.TestGenerate(ctx, p, cred)
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("provider test failed: " + err.Error())
	}

	return c.JSON(http.StatusOK, TestProviderResponse{
		Provider:  providerParam,
		Model:     model,
		Reply:     reply,
		LatencyMs: time.Since(start).Milliseconds(),
	})
}
