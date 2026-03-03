package provider

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
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

// --- Request / Response DTOs ---

// SaveGoogleAICredentialRequest is the request body for saving a Google AI API key.
type SaveGoogleAICredentialRequest struct {
	APIKey string `json:"apiKey" validate:"required"`
}

// SaveVertexAICredentialRequest is the request body for saving Vertex AI credentials.
type SaveVertexAICredentialRequest struct {
	ServiceAccountJSON string `json:"serviceAccountJson" validate:"required"`
	GCPProject         string `json:"gcpProject" validate:"required"`
	Location           string `json:"location" validate:"required"`
}

// SetOrgModelSelectionRequest sets the default models for an org + provider.
type SetOrgModelSelectionRequest struct {
	EmbeddingModel  string `json:"embeddingModel"`
	GenerativeModel string `json:"generativeModel"`
}

// SetProjectPolicyRequest sets a project's provider policy.
type SetProjectPolicyRequest struct {
	// Policy must be one of: none, organization, project.
	Policy string `json:"policy" validate:"required"`

	// The following fields are only required when policy == "project".
	APIKey             string `json:"apiKey"`
	ServiceAccountJSON string `json:"serviceAccountJson"`
	GCPProject         string `json:"gcpProject"`
	Location           string `json:"location"`
	EmbeddingModel     string `json:"embeddingModel"`
	GenerativeModel    string `json:"generativeModel"`
}

// OrgCredentialResponse is the public-safe representation of a stored credential.
type OrgCredentialResponse struct {
	ID         string       `json:"id"`
	OrgID      string       `json:"orgId"`
	Provider   ProviderType `json:"provider"`
	GCPProject string       `json:"gcpProject,omitempty"`
	Location   string       `json:"location,omitempty"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
}

// UsageSummaryResponse wraps usage rows with a note that costs are estimates.
type UsageSummaryResponse struct {
	Note string            `json:"note"`
	Data []UsageSummaryRow `json:"data"`
}

// --- Organization Credential Endpoints ---

// SaveGoogleAICredential stores a Google AI API key for an organization.
// @Summary      Save Google AI credential
// @Description  Encrypts and stores a Google AI API key for the organization. Syncs the model catalog on success.
// @Tags         provider
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Param        request body SaveGoogleAICredentialRequest true "Google AI credential"
// @Success      200 {object} map[string]string "Credential saved"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/providers/google-ai/credentials [post]
// @Security     bearerAuth
func (h *Handler) SaveGoogleAICredential(c echo.Context) error {
	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("orgId is required")
	}

	var req SaveGoogleAICredentialRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.APIKey == "" {
		return apperror.ErrBadRequest.WithMessage("apiKey is required")
	}

	ctx := c.Request().Context()
	if err := h.creds.SaveOrgCredential(ctx, orgID, ProviderGoogleAI, []byte(req.APIKey), "", ""); err != nil {
		return err
	}

	// Sync model catalog in the background; don't block the response.
	cred := &ResolvedCredential{Provider: ProviderGoogleAI, APIKey: req.APIKey}
	go func() {
		if err := h.catalog.SyncModels(context.Background(), ProviderGoogleAI, cred); err != nil {
			h.creds.log.Warn("model catalog sync failed after credential save", slog.String("provider", string(ProviderGoogleAI)))
		}
	}()

	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

// SaveVertexAICredential stores Vertex AI credentials for an organization.
// @Summary      Save Vertex AI credential
// @Description  Encrypts and stores Vertex AI service account credentials for the organization.
// @Tags         provider
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Param        request body SaveVertexAICredentialRequest true "Vertex AI credential"
// @Success      200 {object} map[string]string "Credential saved"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/providers/vertex-ai/credentials [post]
// @Security     bearerAuth
func (h *Handler) SaveVertexAICredential(c echo.Context) error {
	orgID := c.Param("orgId")
	if orgID == "" {
		return apperror.ErrBadRequest.WithMessage("orgId is required")
	}

	var req SaveVertexAICredentialRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.ServiceAccountJSON == "" {
		return apperror.ErrBadRequest.WithMessage("serviceAccountJson is required")
	}
	if req.GCPProject == "" {
		return apperror.ErrBadRequest.WithMessage("gcpProject is required")
	}
	if req.Location == "" {
		return apperror.ErrBadRequest.WithMessage("location is required")
	}

	ctx := c.Request().Context()
	if err := h.creds.SaveOrgCredential(ctx, orgID, ProviderVertexAI, []byte(req.ServiceAccountJSON), req.GCPProject, req.Location); err != nil {
		return err
	}

	// Sync model catalog in the background.
	cred := &ResolvedCredential{
		Provider:           ProviderVertexAI,
		GCPProject:         req.GCPProject,
		Location:           req.Location,
		ServiceAccountJSON: req.ServiceAccountJSON,
	}
	go func() {
		if err := h.catalog.SyncModels(context.Background(), ProviderVertexAI, cred); err != nil {
			h.creds.log.Warn("model catalog sync failed after credential save", slog.String("provider", string(ProviderVertexAI)))
		}
	}()

	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

// DeleteOrgCredential removes a provider credential for an organization.
// @Summary      Delete organization credential
// @Description  Removes the stored credential for the given provider from the organization.
// @Tags         provider
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Param        provider path string true "Provider (google-ai or vertex-ai)"
// @Success      200 {object} map[string]string "Deleted"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/providers/{provider}/credentials [delete]
// @Security     bearerAuth
func (h *Handler) DeleteOrgCredential(c echo.Context) error {
	orgID := c.Param("orgId")
	providerParam := ProviderType(c.Param("provider"))

	if err := h.creds.DeleteOrgCredential(c.Request().Context(), orgID, providerParam); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// ListOrgCredentials returns the stored credentials (metadata only) for an organization.
// @Summary      List organization credentials
// @Description  Returns metadata about stored credentials. Secrets are never returned.
// @Tags         provider
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Success      200 {array} OrgCredentialResponse "Credentials"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/providers/credentials [get]
// @Security     bearerAuth
func (h *Handler) ListOrgCredentials(c echo.Context) error {
	orgID := c.Param("orgId")

	if err := assertCallerOwnsOrg(c.Request().Context(), orgID); err != nil {
		return err
	}

	creds, err := h.repo.ListOrgCredentials(c.Request().Context(), orgID)
	if err != nil {
		return err
	}

	resp := make([]OrgCredentialResponse, len(creds))
	for i, cr := range creds {
		resp[i] = OrgCredentialResponse{
			ID:         cr.ID,
			OrgID:      cr.OrgID,
			Provider:   cr.Provider,
			GCPProject: cr.GCPProject,
			Location:   cr.Location,
			CreatedAt:  cr.CreatedAt,
			UpdatedAt:  cr.UpdatedAt,
		}
	}
	return c.JSON(http.StatusOK, resp)
}

// SetOrgModelSelection sets the default generative and embedding models for an org + provider.
// @Summary      Set organization model selection
// @Description  Sets the default embedding and generative models to use for the given provider.
// @Tags         provider
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Param        provider path string true "Provider (google-ai or vertex-ai)"
// @Param        request body SetOrgModelSelectionRequest true "Model selection"
// @Success      200 {object} map[string]string "Saved"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/providers/{provider}/models [put]
// @Security     bearerAuth
func (h *Handler) SetOrgModelSelection(c echo.Context) error {
	orgID := c.Param("orgId")
	providerParam := ProviderType(c.Param("provider"))

	if err := assertCallerOwnsOrg(c.Request().Context(), orgID); err != nil {
		return err
	}

	var req SetOrgModelSelectionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	sel := &OrganizationProviderModelSelection{
		OrgID:           orgID,
		Provider:        providerParam,
		EmbeddingModel:  req.EmbeddingModel,
		GenerativeModel: req.GenerativeModel,
	}
	if err := h.repo.UpsertOrgModelSelection(c.Request().Context(), sel); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

// ListModels returns the cached model catalog for a provider.
// @Summary      List provider models
// @Description  Returns the cached list of supported models for the given provider.
// @Tags         provider
// @Produce      json
// @Param        provider path string true "Provider (google-ai or vertex-ai)"
// @Param        type query string false "Model type filter (embedding or generative)"
// @Success      200 {array} ProviderSupportedModel "Models"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/providers/{provider}/models [get]
// @Security     bearerAuth
func (h *Handler) ListModels(c echo.Context) error {
	providerParam := ProviderType(c.Param("provider"))

	var modelType *ModelType
	if mt := c.QueryParam("type"); mt != "" {
		t := ModelType(mt)
		modelType = &t
	}

	models, err := h.catalog.ListModels(c.Request().Context(), providerParam, modelType)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, models)
}

// --- Project Policy Endpoints ---

// SetProjectPolicy sets or updates the provider policy for a project.
// @Summary      Set project provider policy
// @Description  Sets the provider policy for a project (none, organization, or project) and optionally stores project-level credentials.
// @Tags         provider
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        provider path string true "Provider (google-ai or vertex-ai)"
// @Param        request body SetProjectPolicyRequest true "Policy request"
// @Success      200 {object} map[string]string "Policy set"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/providers/{provider}/policy [put]
// @Security     bearerAuth
func (h *Handler) SetProjectPolicy(c echo.Context) error {
	projectID := c.Param("projectId")
	providerParam := ProviderType(c.Param("provider"))

	var req SetProjectPolicyRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	policy := ProviderPolicy(req.Policy)
	if policy != PolicyNone && policy != PolicyOrganization && policy != PolicyProject {
		return apperror.ErrBadRequest.WithMessage("policy must be one of: none, organization, project")
	}

	// Build the plaintext credential bytes for project-level creds.
	var plaintext []byte
	if policy == PolicyProject {
		switch providerParam {
		case ProviderGoogleAI:
			if req.APIKey == "" {
				return apperror.ErrBadRequest.WithMessage("apiKey is required when policy is 'project' for google-ai")
			}
			plaintext = []byte(req.APIKey)
		case ProviderVertexAI:
			if req.ServiceAccountJSON == "" {
				return apperror.ErrBadRequest.WithMessage("serviceAccountJson is required when policy is 'project' for vertex-ai")
			}
			if req.GCPProject == "" {
				return apperror.ErrBadRequest.WithMessage("gcpProject is required when policy is 'project' for vertex-ai")
			}
			if req.Location == "" {
				return apperror.ErrBadRequest.WithMessage("location is required when policy is 'project' for vertex-ai")
			}
			plaintext = []byte(req.ServiceAccountJSON)
		}
	}

	ctx := c.Request().Context()
	if err := h.creds.SaveProjectPolicy(ctx, projectID, providerParam, policy, plaintext,
		req.GCPProject, req.Location, req.EmbeddingModel, req.GenerativeModel); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

// GetProjectPolicy returns the current provider policy for a project.
// @Summary      Get project provider policy
// @Description  Returns the current provider policy for the given project + provider.
// @Tags         provider
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        provider path string true "Provider (google-ai or vertex-ai)"
// @Success      200 {object} ProjectProviderPolicy "Policy"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      404 {object} apperror.Error "Not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/providers/{provider}/policy [get]
// @Security     bearerAuth
func (h *Handler) GetProjectPolicy(c echo.Context) error {
	projectID := c.Param("projectId")
	providerParam := ProviderType(c.Param("provider"))

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	policy, err := h.repo.GetProjectPolicy(c.Request().Context(), projectID, providerParam)
	if err != nil {
		return err
	}
	if policy == nil {
		return apperror.ErrNotFound.WithMessage("no policy configured for this project and provider")
	}

	// Return safe fields (no encrypted blobs)
	resp := struct {
		ID              string         `json:"id"`
		ProjectID       string         `json:"projectId"`
		Provider        ProviderType   `json:"provider"`
		Policy          ProviderPolicy `json:"policy"`
		GCPProject      string         `json:"gcpProject,omitempty"`
		Location        string         `json:"location,omitempty"`
		EmbeddingModel  string         `json:"embeddingModel,omitempty"`
		GenerativeModel string         `json:"generativeModel,omitempty"`
		CreatedAt       time.Time      `json:"createdAt"`
		UpdatedAt       time.Time      `json:"updatedAt"`
	}{
		ID:              policy.ID,
		ProjectID:       policy.ProjectID,
		Provider:        policy.Provider,
		Policy:          policy.Policy,
		GCPProject:      policy.GCPProject,
		Location:        policy.Location,
		EmbeddingModel:  policy.EmbeddingModel,
		GenerativeModel: policy.GenerativeModel,
		CreatedAt:       policy.CreatedAt,
		UpdatedAt:       policy.UpdatedAt,
	}
	return c.JSON(http.StatusOK, resp)
}

// ListProjectPolicies returns all provider policies for a project.
// @Summary      List project provider policies
// @Description  Returns all provider policies configured for the given project.
// @Tags         provider
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Success      200 {array} ProjectProviderPolicy "Policies"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/providers/policies [get]
// @Security     bearerAuth
func (h *Handler) ListProjectPolicies(c echo.Context) error {
	projectID := c.Param("projectId")

	if err := h.creds.assertCallerOwnsProject(c.Request().Context(), projectID); err != nil {
		return err
	}

	policies, err := h.repo.ListProjectPolicies(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	// Strip encrypted fields before returning
	type safePolicy struct {
		ID              string         `json:"id"`
		ProjectID       string         `json:"projectId"`
		Provider        ProviderType   `json:"provider"`
		Policy          ProviderPolicy `json:"policy"`
		GCPProject      string         `json:"gcpProject,omitempty"`
		Location        string         `json:"location,omitempty"`
		EmbeddingModel  string         `json:"embeddingModel,omitempty"`
		GenerativeModel string         `json:"generativeModel,omitempty"`
		CreatedAt       time.Time      `json:"createdAt"`
		UpdatedAt       time.Time      `json:"updatedAt"`
	}
	resp := make([]safePolicy, len(policies))
	for i, p := range policies {
		resp[i] = safePolicy{
			ID:              p.ID,
			ProjectID:       p.ProjectID,
			Provider:        p.Provider,
			Policy:          p.Policy,
			GCPProject:      p.GCPProject,
			Location:        p.Location,
			EmbeddingModel:  p.EmbeddingModel,
			GenerativeModel: p.GenerativeModel,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		}
	}
	return c.JSON(http.StatusOK, resp)
}

// --- Usage & Cost Summary Endpoints ---

// GetProjectUsageSummary returns aggregated token usage and estimated costs for a project.
// @Summary      Get project usage summary
// @Description  Returns aggregated token usage and ESTIMATED costs for a project, grouped by provider and model.
// @Tags         provider
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        since query string false "Start time (RFC3339)"
// @Param        until query string false "End time (RFC3339)"
// @Success      200 {object} UsageSummaryResponse "Usage summary"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/projects/{projectId}/usage [get]
// @Security     bearerAuth
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

// GetOrgUsageSummary returns aggregated token usage and estimated costs across all projects in an org.
// @Summary      Get organization usage summary
// @Description  Returns aggregated token usage and ESTIMATED costs for all projects in the organization, grouped by provider and model.
// @Tags         provider
// @Produce      json
// @Param        orgId path string true "Organization ID"
// @Param        since query string false "Start time (RFC3339)"
// @Param        until query string false "End time (RFC3339)"
// @Success      200 {object} UsageSummaryResponse "Usage summary"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/organizations/{orgId}/usage [get]
// @Security     bearerAuth
func (h *Handler) GetOrgUsageSummary(c echo.Context) error {
	orgID := c.Param("orgId")

	if err := assertCallerOwnsOrg(c.Request().Context(), orgID); err != nil {
		return err
	}

	since, until := parseTimeRange(c)
	rows, err := h.repo.GetOrgUsageSummary(c.Request().Context(), orgID, since, until)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, UsageSummaryResponse{
		Note: "Costs shown are estimates based on retail pricing and may not reflect your actual provider invoice.",
		Data: rows,
	})
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
