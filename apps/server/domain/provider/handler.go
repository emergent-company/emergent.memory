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

// --- Project Provider Config Endpoints ---

// ListProjectProviders returns all provider configs for a specific project.
// @Summary List project-level provider configs
// @Param projectId path string true "Project ID"
// @Success 200 {array} ProjectProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/providers [get]
func (h *Handler) ListProjectProviders(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	resp, err := h.creds.ListProjectConfigs(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// ListProjectConfigs returns all project-level provider configs for projects in an org.
// @Summary List project-level provider config overrides for an org
// @Param orgId path string true "Organization ID"
// @Success 200 {array} ProjectProviderConfigResponse
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /organizations/{orgId}/project-providers [get]
func (h *Handler) ListProjectConfigs(c echo.Context) error {
	orgID := c.Param("orgId")

	resp, err := h.creds.ListProjectConfigsByOrg(c.Request().Context(), orgID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// SaveProjectConfig stores provider credentials and model selections for a project.
// @Summary Configure project-level provider
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name (google, google-vertex, openai, or deepseek)"
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

	resp, err := h.creds.UpsertProjectConfig(c.Request().Context(), projectID, provider, req)
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

	if err := h.creds.DeleteProjectConfig(c.Request().Context(), projectID, provider); err != nil {
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

// ListAllModels returns the cached model catalog across all providers.
// @Summary List all available LLM models
// @Tags providers
// @Produce json
// @Param type query string false "Filter by model type (embedding or generative)"
// @Success 200 {array} ProviderSupportedModel
// @Failure 401 {object} apperror.Error
// @Router /v1/models [get]
func (h *Handler) ListAllModels(c echo.Context) error {
	var modelType *ModelType
	if mt := c.QueryParam("type"); mt != "" {
		t := ModelType(mt)
		modelType = &t
	}

	models, err := h.catalog.ListAllModels(c.Request().Context(), modelType)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, models)
}

// ListPricing returns all global retail pricing rows (per provider and model,
// per-modality USD prices per 1M tokens). Read-only; pricing is managed by the
// internal sync.
// @Summary List global retail pricing
// @Success 200 {array} ProviderPricing
// @Failure 401 {object} apperror.Error
// @Router /pricing [get]
func (h *Handler) ListPricing(c echo.Context) error {
	rows, err := h.repo.ListPricing(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rows)
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

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetProjectUsageSummary(c.Request().Context(), projectID, since, until)
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
	if err := assertCallerOwnsOrg(ctx, h.repo, orgID); err != nil {
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

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	granularity := c.QueryParam("granularity")
	since, until := parseTimeRange(c)
	rows, err := h.repo.GetProjectUsageTimeSeries(c.Request().Context(), projectID, granularity, since, until)
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
	if err := assertCallerOwnsOrg(ctx, h.repo, orgID); err != nil {
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

// --- Project Pricing Overrides ---

// UpsertProjectPricingOverridesRequest is the request body for upserting a
// project pricing override. Prices are in USD per 1 million tokens.
type UpsertProjectPricingOverridesRequest struct {
	Provider        ProviderType `json:"provider"`
	Model           string       `json:"model"`
	TextInputPrice  float64      `json:"textInputPrice"`
	ImageInputPrice float64      `json:"imageInputPrice"`
	VideoInputPrice float64      `json:"videoInputPrice"`
	AudioInputPrice float64      `json:"audioInputPrice"`
	OutputPrice     float64      `json:"outputPrice"`
}

// ListProjectPricingOverrides returns all pricing overrides for a project.
// @Summary List project pricing overrides
// @Param projectId path string true "Project ID"
// @Success 200 {array} ProjectCustomPricing
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/pricing-overrides [get]
func (h *Handler) ListProjectPricingOverrides(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	overrides, err := h.repo.ListProjectCustomPricing(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, overrides)
}

// UpsertProjectPricingOverrides creates or updates a pricing override for a project.
// @Summary Upsert a project pricing override
// @Param projectId path string true "Project ID"
// @Param body body UpsertProjectPricingOverridesRequest true "Pricing override (USD per 1M tokens)"
// @Success 200 {object} ProjectCustomPricing
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/pricing-overrides [put]
func (h *Handler) UpsertProjectPricingOverrides(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req UpsertProjectPricingOverridesRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Provider == "" {
		return apperror.ErrBadRequest.WithMessage("provider is required")
	}
	if req.Model == "" {
		return apperror.ErrBadRequest.WithMessage("model is required")
	}

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	entry := &ProjectCustomPricing{
		ProjectID:       projectID,
		Provider:        req.Provider,
		ProviderSlug:    ProviderSlug(req.Provider),
		Model:           req.Model,
		TextInputPrice:  req.TextInputPrice,
		ImageInputPrice: req.ImageInputPrice,
		VideoInputPrice: req.VideoInputPrice,
		AudioInputPrice: req.AudioInputPrice,
		OutputPrice:     req.OutputPrice,
	}
	if err := h.repo.UpsertProjectCustomPricing(c.Request().Context(), entry); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, entry)
}

// DeleteProjectPricingOverride removes a pricing override from a project.
// @Summary Delete a project pricing override
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name (google, google-vertex, openai, or deepseek)"
// @Param model path string true "Model name"
// @Success 200 {object} map[string]string
// @Failure 401 {object} apperror.Error
// @Failure 403 {object} apperror.Error
// @Router /projects/{projectId}/pricing-overrides/{provider}/{model} [delete]
func (h *Handler) DeleteProjectPricingOverride(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}
	provider := ProviderType(c.Param("provider"))
	model := c.Param("model")
	if provider == "" || model == "" {
		return apperror.ErrBadRequest.WithMessage("provider and model are required")
	}

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	if err := h.repo.DeleteProjectCustomPricing(c.Request().Context(), projectID, string(provider), model); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
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
	if err := assertCallerOwnsOrg(ctx, h.repo, orgID); err != nil {
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

// TestProjectProviderResponse is the response body for the project provider test endpoint.
// Identical shape to TestProviderResponse but scoped to a project's credential config.
type TestProjectProviderResponse = TestProviderResponse

// testProjectProviderRequest is the OPTIONAL JSON body accepted by
// TestProjectProvider. When omitted (or when Model is empty), the endpoint runs
// the existing generate-then-embed test on the credential's configured models.
// When Model is set, the endpoint tests exactly that model; ModelType selects
// the test path ("generative" is the default, "embedding" runs the embed test).
type testProjectProviderRequest struct {
	Model     string `json:"model"`
	ModelType string `json:"modelType"`
}

// TestProjectProvider sends a live test call using a project's configured
// provider credentials. An optional JSON body ({model, modelType}) selects an
// explicit model to test instead of the credential's configured model.
// @Summary Test a project provider with a live generate call
// @Param projectId path string true "Project ID"
// @Param provider path string true "Provider name"
// @Param body body testProjectProviderRequest false "Optional model override"
// @Success 200 {object} TestProjectProviderResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Router /projects/{projectId}/providers/{provider}/test [post]
func (h *Handler) TestProjectProvider(c echo.Context) error {
	projectID := c.Param("projectId")
	providerParam := c.Param("provider")
	p := ProviderType(providerParam)

	// fail is this handler's single apperror Style A call site: the lint
	// ratchet counts `.WithMessage` chains, so every bad-request path funnels
	// through here instead of chaining inline.
	fail := func(msg string) error { return apperror.ErrBadRequest.WithMessage(msg) }

	// Optional body: lets callers test an explicit model (generative or
	// embedding) rather than the credential's configured model. An empty body
	// (or an empty model) leaves behaviour byte-for-byte unchanged.
	var req testProjectProviderRequest
	if err := c.Bind(&req); err != nil {
		return fail("invalid request body")
	}
	if req.ModelType != "" &&
		req.ModelType != string(ModelTypeGenerative) &&
		req.ModelType != string(ModelTypeEmbedding) {
		return fail("invalid modelType: must be \"generative\" or \"embedding\"")
	}

	// Enforce project ownership before resolving credentials: the caller must
	// own the project whose provider credentials drive this outbound test call.
	// Ownership is derived from real org membership (see assertCallerOwnsProject).
	ctx := c.Request().Context()
	if err := h.creds.assertCallerOwnsProject(ctx, projectID); err != nil {
		return err
	}
	ctx = auth.ContextWithProjectID(ctx, projectID)

	cred, err := h.creds.Resolve(ctx, p)
	if err != nil {
		return fail("failed to resolve credentials: " + err.Error())
	}
	if cred == nil {
		return fail("no credentials configured for provider " + providerParam + " on project " + projectID)
	}

	start := time.Now()

	// Explicit model override: run only the requested test path.
	if req.Model != "" && req.ModelType == string(ModelTypeEmbedding) {
		embModel, embErr := h.catalog.TestEmbedForModel(ctx, p, cred, req.Model)
		if embErr != nil {
			return fail("provider test failed: " + embErr.Error())
		}
		return c.JSON(http.StatusOK, TestProjectProviderResponse{
			Provider:       providerParam,
			Model:          req.Model,
			EmbeddingModel: embModel,
			EmbeddingOK:    embModel != "not supported",
			LatencyMs:      time.Since(start).Milliseconds(),
		})
	}

	if req.Model != "" {
		// Generative override (also the default when modelType is omitted).
		reply, genErr := h.catalog.TestGenerateForModel(ctx, p, cred, req.Model)
		if genErr != nil {
			return fail("provider test failed: " + genErr.Error())
		}
		return c.JSON(http.StatusOK, TestProjectProviderResponse{
			Provider:  providerParam,
			Model:     req.Model,
			Reply:     reply,
			LatencyMs: time.Since(start).Milliseconds(),
		})
	}

	// No body / empty model: existing behaviour unchanged.
	model, reply, err := h.catalog.TestGenerate(ctx, p, cred)
	if err != nil {
		return fail("provider test failed: " + err.Error())
	}

	embModel, embErr := h.catalog.TestEmbed(ctx, p, cred)
	resp := TestProjectProviderResponse{
		Provider:  providerParam,
		Model:     model,
		Reply:     reply,
		LatencyMs: time.Since(start).Milliseconds(),
	}
	if embErr != nil {
		resp.EmbeddingError = embErr.Error()
	} else {
		resp.EmbeddingModel = embModel
		resp.EmbeddingOK = embModel != "not supported"
	}

	return c.JSON(http.StatusOK, resp)
}

// TestProviderResponse is the response body for the provider test endpoint.
type TestProviderResponse struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Reply          string `json:"reply"`
	LatencyMs      int64  `json:"latencyMs"`
	EmbeddingModel string `json:"embeddingModel,omitempty"`
	EmbeddingOK    bool   `json:"embeddingOk"`
	EmbeddingError string `json:"embeddingError,omitempty"`
}

// TestProvider sends a live "hello" generate call to verify provider credentials work end-to-end.
// @Summary Test a provider with a live generate call
// @Param provider path string true "Provider name (google, google-vertex, openai, or deepseek)"
// @Param projectId query string false "Project ID for credential resolution"
// @Param orgId query string false "Org ID for credential resolution"
// @Success 200 {object} TestProviderResponse
// @Failure 400 {object} apperror.Error
// @Failure 401 {object} apperror.Error
// @Router /providers/{provider}/test [post]
func (h *Handler) TestProvider(c echo.Context) error {
	providerParam := c.Param("provider")
	if providerParam != string(ProviderGoogleAI) &&
		providerParam != string(ProviderVertexAI) &&
		providerParam != string(ProviderOpenAI) &&
		providerParam != string(ProviderDeepSeek) {
		return apperror.ErrBadRequest.WithMessage("provider must be google, google-vertex, openai, or deepseek")
	}
	p := ProviderType(providerParam)

	ctx := c.Request().Context()
	if projectID := c.QueryParam("projectId"); projectID != "" {
		// Enforce project ownership before resolving credentials (mirrors
		// TestProjectProvider). Ownership is derived from real org membership.
		if err := h.creds.assertCallerOwnsProject(ctx, projectID); err != nil {
			return err
		}
		ctx = auth.ContextWithProjectID(ctx, projectID)
	}
	// The ?orgId query param is intentionally not injected into the auth context:
	// credential resolution is project-scoped only (org-level config is
	// deprecated), so writing a request-controlled org into the context would be
	// an untrusted-org injection with no effect (issue #841 sibling audit).

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

	embModel, embErr := h.catalog.TestEmbed(ctx, p, cred)
	resp := TestProviderResponse{
		Provider:  providerParam,
		Model:     model,
		Reply:     reply,
		LatencyMs: time.Since(start).Milliseconds(),
	}
	if embErr != nil {
		resp.EmbeddingError = embErr.Error()
	} else {
		resp.EmbeddingModel = embModel
		resp.EmbeddingOK = embModel != "not supported"
	}

	return c.JSON(http.StatusOK, resp)
}

// GetCurrentUserUsageSummary returns aggregated LLM usage for the authenticated user.
// @Summary Get current user LLM usage summary
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Success 200 {object} UsageSummaryResponse
// @Failure 401 {object} apperror.Error
// @Router /users/me/usage [get]
func (h *Handler) GetCurrentUserUsageSummary(c echo.Context) error {
	user := auth.MustGetUser(c)

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetUserUsageSummary(c.Request().Context(), user.ID, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageSummaryResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}

// GetCurrentUserUsageTimeSeries returns time-bucketed LLM usage for the authenticated user.
// @Summary Get current user LLM usage time series
// @Param since query string false "Start time (RFC3339)"
// @Param until query string false "End time (RFC3339)"
// @Param granularity query string false "Bucket size: day (default), week, month"
// @Success 200 {object} UsageTimeSeriesResponse
// @Failure 401 {object} apperror.Error
// @Router /users/me/usage/timeseries [get]
func (h *Handler) GetCurrentUserUsageTimeSeries(c echo.Context) error {
	user := auth.MustGetUser(c)

	granularity := c.QueryParam("granularity")
	since, until := parseTimeRange(c)
	rows, err := h.repo.GetUserUsageTimeSeries(c.Request().Context(), user.ID, granularity, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageTimeSeriesResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
}
