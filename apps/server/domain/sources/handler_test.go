package sources_test

import (
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SourcesHandlerSuite exercises the /api/data-source-integrations surface
// through the in-process server: authentication (401), provider catalog/schema,
// source-types, CRUD, validation, project scoping (404), and no-secret-leak.
type SourcesHandlerSuite struct {
	testutil.BaseSuite
}

func TestSourcesHandlerSuite(t *testing.T) {
	suite.Run(t, new(SourcesHandlerSuite))
}

func (s *SourcesHandlerSuite) SetupSuite() {
	// The Drive source encryption key must be configured for create/update
	// paths. Set it before the server/config is built so the sources service
	// derives the same config-sourced key as production.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	s.T().Setenv("LLM_ENCRYPTION_KEY", hex.EncodeToString(key))
	s.SetDBSuffix("sources_handler")
	s.BaseSuite.SetupSuite()
}

// TestUnauthenticatedIs401 verifies the group rejects missing credentials.
func (s *SourcesHandlerSuite) TestUnauthenticatedIs401() {
	resp := s.Client.GET("/api/data-source-integrations")
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestListProviders verifies the provider catalog includes google_drive (the
// registry ships multiple providers, so the assertion locates it by type).
func (s *SourcesHandlerSuite) TestListProviders() {
	resp := s.Client.GET("/api/data-source-integrations/providers",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var providers []map[string]any
	s.Require().NoError(resp.JSON(&providers))

	var drive map[string]any
	for _, p := range providers {
		if p["type"] == "google_drive" {
			drive = p
			break
		}
	}
	s.Require().NotNil(drive, "provider catalog missing google_drive entry: %v", providers)
	s.Require().Equal(true, drive["available"])
}

// TestProviderSchema verifies the schema shape and the unknown-provider 404.
func (s *SourcesHandlerSuite) TestProviderSchema() {
	resp := s.Client.GET("/api/data-source-integrations/providers/google_drive/schema",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var schema map[string]any
	s.Require().NoError(resp.JSON(&schema))
	s.Require().Equal("google_drive", schema["type"])
	req, ok := schema["required"].([]any)
	s.Require().True(ok, "schema.required missing")
	s.Require().Contains(req, "folder_id")

	unknown := s.Client.GET("/api/data-source-integrations/providers/imap/schema",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusNotFound, unknown.StatusCode, unknown.String())
}

// TestSourceTypesEmpty verifies the source-types endpoint returns an array.
func (s *SourcesHandlerSuite) TestSourceTypesEmpty() {
	resp := s.Client.GET("/api/data-source-integrations/source-types",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())
	var out []any
	s.Require().NoError(resp.JSON(&out))
}

// TestCreateValidation verifies missing folder_id and missing refresh_token are
// rejected before any persistence.
func (s *SourcesHandlerSuite) TestCreateValidation() {
	resp := s.Client.POST("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "No folder",
			"providerType": "google_drive",
			"config":       map[string]any{"refresh_token": "rt"},
		}))
	s.Require().Equal(http.StatusUnprocessableEntity, resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "No token",
			"providerType": "google_drive",
			"config":       map[string]any{"folder_id": "f"},
		}))
	s.Require().Equal(http.StatusUnprocessableEntity, resp.StatusCode, resp.String())
}

// TestCreateGetNoSecretLeak verifies a full create → read round trip returns
// 201/200 and never leaks credential material.
func (s *SourcesHandlerSuite) TestCreateGetNoSecretLeak() {
	const secret = "super-secret-refresh-token"
	create := s.Client.POST("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Drive",
			"providerType": "google_drive",
			"config": map[string]any{
				"folder_id":     "folder-1",
				"mime_types":    []string{"application/pdf"},
				"recursive":     true,
				"client_id":     "cid",
				"client_secret": "cs",
				"refresh_token": secret,
			},
		}))
	s.Require().Equal(http.StatusCreated, create.StatusCode, create.String())

	var created map[string]any
	s.Require().NoError(create.JSON(&created))
	id, _ := created["id"].(string)
	s.Require().NotEmpty(id)

	// Create response must not leak secrets.
	s.Require().NotContains(create.String(), secret)
	s.Require().NotContains(create.String(), "cs")

	// List must not leak secrets.
	list := s.Client.GET("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, list.StatusCode, list.String())
	s.Require().NotContains(list.String(), secret)

	// Get must not leak secrets.
	get := s.Client.GET("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, get.StatusCode, get.String())
	s.Require().NotContains(get.String(), secret)
	s.Require().NotContains(get.String(), "client_secret")

	// Patch (name-only) succeeds.
	patch := s.Client.PATCH("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"name": "Drive Renamed"}))
	s.Require().Equal(http.StatusOK, patch.StatusCode, patch.String())

	// Delete succeeds (204).
	del := s.Client.DELETE("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNoContent, del.StatusCode, del.String())

	// Get after delete → 404.
	gone := s.Client.GET("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, gone.StatusCode, gone.String())
}

// TestGetNonexistentIs404 verifies a well-formed but unknown id resolves to
// not-found, and a malformed id resolves to bad-request.
func (s *SourcesHandlerSuite) TestGetNonexistentIs404() {
	unknown := "11111111-2222-3333-4444-555555555555"
	resp := s.Client.GET("/api/data-source-integrations/"+unknown,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode, resp.String())

	malformed := s.Client.GET("/api/data-source-integrations/not-a-uuid",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusBadRequest, malformed.StatusCode, malformed.String())
}

// TestPatchEnabledTogglesStatus verifies UpdateIntegrationRequest.Enabled maps
// to the persisted source status — the operator kill switch that stops
// scheduled syncs (ListDueSources only admits active sources).
func (s *SourcesHandlerSuite) TestPatchEnabledTogglesStatus() {
	create := s.Client.POST("/api/data-source-integrations",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":         "Toggle",
			"providerType": "google_drive",
			"config":       map[string]any{"folder_id": "folder-1", "refresh_token": "rt"},
		}))
	s.Require().Equal(http.StatusCreated, create.StatusCode, create.String())

	var created map[string]any
	s.Require().NoError(create.JSON(&created))
	id, _ := created["id"].(string)
	s.Require().NotEmpty(id)

	statusOf := func() string {
		s.T().Helper()
		get := s.Client.GET("/api/data-source-integrations/"+id,
			testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
		s.Require().Equal(http.StatusOK, get.StatusCode, get.String())
		var body map[string]any
		s.Require().NoError(get.JSON(&body))
		st, _ := body["status"].(string)
		return st
	}

	s.Require().Equal("active", statusOf(), "new source must start active")

	disable := s.Client.PATCH("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))
	s.Require().Equal(http.StatusOK, disable.StatusCode, disable.String())
	s.Require().Equal("disabled", statusOf(), "enabled=false must disable the source")

	enable := s.Client.PATCH("/api/data-source-integrations/"+id,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": true}))
	s.Require().Equal(http.StatusOK, enable.StatusCode, enable.String())
	s.Require().Equal("active", statusOf(), "enabled=true must re-activate the source")
}

// TestOAuthAuthorizeRequiresAuth verifies the OAuth authorize endpoint is gated.
func (s *SourcesHandlerSuite) TestOAuthAuthorizeRequiresAuth() {
	resp := s.Client.GET("/api/data-source-integrations/providers/google_drive/oauth/authorize")
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode, resp.String())
}

// TestOAuthCallbackInvalidStateRedirects verifies an unknown/expired state lands
// on the error path (and that the callback is reachable unauthenticated).
func (s *SourcesHandlerSuite) TestOAuthCallbackInvalidStateRedirects() {
	resp := s.Client.GET("/api/data-source-integrations/oauth/google_drive/callback?state=unknown-state&code=x")
	s.Require().Equal(http.StatusSeeOther, resp.StatusCode, resp.String())
	s.Require().Contains(resp.Headers.Get("Location"), "oauth_error=",
		"invalid state must not create a source and must redirect to the error path")
}

// TestOAuthCallbackProviderErrorRedirects verifies a provider-denied consent
// (?error=access_denied) redirects without touching the pending-state store.
func (s *SourcesHandlerSuite) TestOAuthCallbackProviderErrorRedirects() {
	resp := s.Client.GET("/api/data-source-integrations/oauth/google_drive/callback?error=access_denied")
	s.Require().Equal(http.StatusSeeOther, resp.StatusCode, resp.String())
	s.Require().Contains(resp.Headers.Get("Location"), "access_denied")
}
