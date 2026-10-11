package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- data-source integrations (JSON surface) ---

// dsProviderTypeGoogleDrive is the only provider this iteration supports; the
// connect form and the handler-side validation both key off it.
const dsProviderTypeGoogleDrive = "google_drive"

// dsCredentialKeys are the config keys that carry OAuth2 credential material.
// They are write-only: the server strips them before persistence and never
// returns them, so the UI renders them blank and only sends them when supplied.
var dsCredentialKeys = []string{"client_id", "client_secret", "refresh_token"}

// dsError maps a memory backend error onto a gateway response. Validation and
// not-found pass through with their real status; everything else is a 502.
func dsError(c echo.Context, err error) error {
	msg := err.Error()
	switch memoryStatus(err) {
	case http.StatusBadRequest:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	case http.StatusNotFound:
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	case http.StatusConflict:
		return c.JSON(http.StatusConflict, map[string]string{"error": msg})
	default:
		return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
	}
}

// validateDSConfig enforces the google_drive config contract before a request
// reaches memory: folder_id is required and a refresh token is required (without
// it a connection could never be authorized).
func validateDSConfig(cfg map[string]any) string {
	if cfg == nil {
		return "config.folder_id is required"
	}
	if folderID, _ := cfg["folder_id"].(string); strings.TrimSpace(folderID) == "" {
		return "config.folder_id is required"
	}
	if token, _ := cfg["refresh_token"].(string); strings.TrimSpace(token) == "" {
		return "config.refresh_token is required"
	}
	return ""
}

// dsHasCredentialKeys reports whether any OAuth2 credential key is present.
func dsHasCredentialKeys(cfg map[string]any) bool {
	for _, k := range dsCredentialKeys {
		if _, ok := cfg[k]; ok {
			return true
		}
	}
	return false
}

// listDSProviders implements GET /api/data-source-integrations/providers.
func (s *Server) listDSProviders(c echo.Context) error {
	providers, err := s.memory.ListDSProviders(c.Request().Context())
	if err != nil {
		return dsError(c, err)
	}
	if providers == nil {
		providers = []DSProvider{}
	}
	return c.JSON(http.StatusOK, providers)
}

// getDSProviderSchema implements GET /api/data-source-integrations/providers/:providerType/schema.
func (s *Server) getDSProviderSchema(c echo.Context) error {
	schema, err := s.memory.GetDSProviderSchema(c.Request().Context(), c.Param("providerType"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, schema)
}

// listDSSourceTypes implements GET /api/data-source-integrations/source-types.
func (s *Server) listDSSourceTypes(c echo.Context) error {
	types, err := s.memory.ListDSSourceTypes(c.Request().Context())
	if err != nil {
		return dsError(c, err)
	}
	if types == nil {
		types = []DSSourceType{}
	}
	return c.JSON(http.StatusOK, types)
}

// testDSConfig implements POST /api/data-source-integrations/test-config.
func (s *Server) testDSConfig(c echo.Context) error {
	var req DSTestConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.ProviderType != dsProviderTypeGoogleDrive {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "providerType must be google_drive"})
	}
	if msg := validateDSConfig(req.Config); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	resp, err := s.memory.TestDSConfig(c.Request().Context(), &req)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// testDSConnection implements POST /api/data-source-integrations/:id/test-connection.
func (s *Server) testDSConnection(c echo.Context) error {
	resp, err := s.memory.TestDSConnection(c.Request().Context(), c.Param("id"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// listDSIntegrations implements GET /api/data-source-integrations.
func (s *Server) listDSIntegrations(c echo.Context) error {
	items, err := s.memory.ListDSIntegrations(c.Request().Context())
	if err != nil {
		return dsError(c, err)
	}
	if items == nil {
		items = []DSIntegration{}
	}
	return c.JSON(http.StatusOK, items)
}

// getDSIntegration implements GET /api/data-source-integrations/:id.
func (s *Server) getDSIntegration(c echo.Context) error {
	item, err := s.memory.GetDSIntegration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// createDSIntegration implements POST /api/data-source-integrations.
func (s *Server) createDSIntegration(c echo.Context) error {
	var req DSCreateIntegrationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.ProviderType != dsProviderTypeGoogleDrive {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "providerType must be google_drive"})
	}
	if msg := validateDSConfig(req.Config); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	item, err := s.memory.CreateDSIntegration(c.Request().Context(), &req)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusCreated, item)
}

// updateDSIntegration implements PATCH /api/data-source-integrations/:id.
func (s *Server) updateDSIntegration(c echo.Context) error {
	var req DSUpdateIntegrationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Config != nil {
		if folderID, _ := req.Config["folder_id"].(string); strings.TrimSpace(folderID) == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "config.folder_id is required"})
		}
		if dsHasCredentialKeys(req.Config) {
			if token, _ := req.Config["refresh_token"].(string); strings.TrimSpace(token) == "" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "config.refresh_token is required"})
			}
		}
	}
	item, err := s.memory.UpdateDSIntegration(c.Request().Context(), c.Param("id"), &req)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// deleteDSIntegration implements DELETE /api/data-source-integrations/:id.
func (s *Server) deleteDSIntegration(c echo.Context) error {
	if err := s.memory.DeleteDSIntegration(c.Request().Context(), c.Param("id")); err != nil {
		return dsError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// triggerDSSync implements POST /api/data-source-integrations/:id/sync.
func (s *Server) triggerDSSync(c echo.Context) error {
	resp, err := s.memory.TriggerDSSync(c.Request().Context(), c.Param("id"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// listDSSyncJobs implements GET /api/data-source-integrations/:id/sync-jobs.
func (s *Server) listDSSyncJobs(c echo.Context) error {
	resp, err := s.memory.ListDSSyncJobs(c.Request().Context(), c.Param("id"), c.QueryParam("status"))
	if err != nil {
		return dsError(c, err)
	}
	if resp == nil {
		resp = &DSListSyncJobsResponse{Items: []DSSyncJob{}}
	}
	if resp.Items == nil {
		resp.Items = []DSSyncJob{}
	}
	return c.JSON(http.StatusOK, resp)
}

// getDSLatestSyncJob implements GET /api/data-source-integrations/:id/sync-jobs/latest.
// A source that has never synced renders JSON null, matching the SDK contract.
func (s *Server) getDSLatestSyncJob(c echo.Context) error {
	job, err := s.memory.GetLatestDSSyncJob(c.Request().Context(), c.Param("id"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, job)
}

// getDSSyncJob implements GET /api/data-source-integrations/:id/sync-jobs/:jobId.
func (s *Server) getDSSyncJob(c echo.Context) error {
	job, err := s.memory.GetDSSyncJob(c.Request().Context(), c.Param("id"), c.Param("jobId"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, job)
}

// cancelDSSyncJob implements POST /api/data-source-integrations/:id/sync-jobs/:jobId/cancel.
func (s *Server) cancelDSSyncJob(c echo.Context) error {
	resp, err := s.memory.CancelDSSyncJob(c.Request().Context(), c.Param("id"), c.Param("jobId"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// startDSOAuth implements POST
// /api/data-source-integrations/providers/:providerType/oauth/authorize. It
// returns the provider authorization URL for the browser to follow. The body is
// optional: {config, scopes}.
func (s *Server) startDSOAuth(c echo.Context) error {
	providerType := strings.TrimSpace(c.Param("providerType"))
	if providerType == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "providerType is required"})
	}
	var req struct {
		Config map[string]any `json:"config,omitempty"`
		Scopes []string       `json:"scopes,omitempty"`
	}
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}
	}
	resp, err := s.memory.StartDSOAuth(c.Request().Context(), providerType, req.Config, req.Scopes)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// listDSOAuthClients implements GET /api/orgs/:orgId/data-source-integrations/oauth-clients.
func (s *Server) listDSOAuthClients(c echo.Context) error {
	items, err := s.memory.ListDSOAuthClients(c.Request().Context(), c.Param("orgId"))
	if err != nil {
		return dsError(c, err)
	}
	if items == nil {
		items = []DSOAuthClientConfig{}
	}
	return c.JSON(http.StatusOK, items)
}

// getDSOAuthClient implements GET /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) getDSOAuthClient(c echo.Context) error {
	item, err := s.memory.GetDSOAuthClient(c.Request().Context(), c.Param("orgId"), c.Param("providerType"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// upsertDSOAuthClient implements PUT /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) upsertDSOAuthClient(c echo.Context) error {
	var req DSSetOAuthClientRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "clientId is required"})
	}
	item, err := s.memory.UpsertDSOAuthClient(c.Request().Context(), c.Param("orgId"), c.Param("providerType"), &req)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// deleteDSOAuthClient implements DELETE /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) deleteDSOAuthClient(c echo.Context) error {
	if err := s.memory.DeleteDSOAuthClient(c.Request().Context(), c.Param("orgId"), c.Param("providerType")); err != nil {
		return dsError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// listDocumentVersions implements GET /api/documents/:id/versions.
func (s *Server) listDocumentVersions(c echo.Context) error {
	versions, err := s.memory.ListDocumentVersions(c.Request().Context(), c.Param("id"))
	if err != nil {
		return documentError(c, err)
	}
	if versions == nil {
		versions = []DocumentVersion{}
	}
	return c.JSON(http.StatusOK, map[string]any{"versions": versions})
}
