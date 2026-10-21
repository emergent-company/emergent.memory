package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- data-source integrations (JSON surface) ---

// dsProviderTypeGoogleDrive names the Google Drive provider. The UI is
// provider-driven; this constant is the catalog/test default.
const dsProviderTypeGoogleDrive = "google_drive"

// dsDriveFileStrategy is the Google Drive drive.file scope strategy name. It is
// the one strategy that cannot list the drive without the (unimplemented) Google
// Picker, so an empty resource listing under it gets an actionable notice.
const dsDriveFileStrategy = "drive.file"

// dsCredentialKeys are the config keys that can carry write-only credential
// material (OAuth2 client/refresh tokens and personal access tokens). They are
// write-only: the server strips them before persistence and never returns them,
// so the UI renders them blank and only sends them when supplied. The provider
// schema marks its own credential fields with writeOnly and the gateway keys off
// that marker first; this list is the provider-neutral fallback for schema
// responses that predate it.
var dsCredentialKeys = []string{"client_id", "client_secret", "refresh_token", "access_token"}

// dsError maps a memory backend error onto a gateway response. Client errors
// (bad request, not found, conflict, validation) pass through with their real
// status and the backend's own message — a missing field must not look like a
// backend outage. Everything else (5xx, transport failure) is a 502.
func dsError(c echo.Context, err error) error {
	switch status := memoryStatus(err); status {
	case http.StatusBadRequest, http.StatusNotFound,
		http.StatusConflict, http.StatusUnprocessableEntity:
		return c.JSON(status, map[string]string{"error": dsFormError(err).Error()})
	default:
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
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

// listDSCredentials implements GET /api/data-source-integrations/credentials.
// It returns the OAuth apps available to the active project across scopes, with
// no secret material.
func (s *Server) listDSCredentials(c echo.Context) error {
	items, err := s.memory.ListDSCredentials(c.Request().Context())
	if err != nil {
		return dsError(c, err)
	}
	if items == nil {
		items = []DSOAuthCredentialDescription{}
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items})
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

// listProjectDSOAuthClients implements GET
// /api/projects/:projectId/data-source-integrations/oauth-clients.
func (s *Server) listProjectDSOAuthClients(c echo.Context) error {
	items, err := s.memory.ListProjectDSOAuthClients(c.Request().Context(), c.Param("projectId"))
	if err != nil {
		return dsError(c, err)
	}
	if items == nil {
		items = []DSOAuthClientConfig{}
	}
	return c.JSON(http.StatusOK, items)
}

// getProjectDSOAuthClient implements GET
// /api/projects/:projectId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) getProjectDSOAuthClient(c echo.Context) error {
	item, err := s.memory.GetProjectDSOAuthClient(c.Request().Context(), c.Param("projectId"), c.Param("providerType"))
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// upsertProjectDSOAuthClient implements PUT
// /api/projects/:projectId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) upsertProjectDSOAuthClient(c echo.Context) error {
	var req DSSetOAuthClientRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "clientId is required"})
	}
	item, err := s.memory.UpsertProjectDSOAuthClient(c.Request().Context(), c.Param("projectId"), c.Param("providerType"), &req)
	if err != nil {
		return dsError(c, err)
	}
	return c.JSON(http.StatusOK, item)
}

// deleteProjectDSOAuthClient implements DELETE
// /api/projects/:projectId/data-source-integrations/oauth-clients/:providerType.
func (s *Server) deleteProjectDSOAuthClient(c echo.Context) error {
	if err := s.memory.DeleteProjectDSOAuthClient(c.Request().Context(), c.Param("projectId"), c.Param("providerType")); err != nil {
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
