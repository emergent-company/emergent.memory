package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mcpImportTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/mcp-servers/import", s.uiMCPImportPage)
	e.POST("/settings/mcp-servers/import", s.uiMCPImport)
	return e
}

func mcpImportPost(e *echo.Echo, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/mcp-servers/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

func newMCPImportTestServer(t *testing.T) (*mcpRegistryTestBackend, *Server, *echo.Echo) {
	t.Helper()
	backend := newMCPRegistryTestBackend()
	srv := &Server{memory: backend}
	return backend, srv, mcpImportTestServer(srv)
}

func TestMCPImportPageRenders(t *testing.T) {
	_, _, e := newMCPImportTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/mcp-servers/import", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `name="config"`)
	assert.Contains(t, body, `name="target"`)
	assert.Contains(t, body, `name="device"`)
	assert.Contains(t, body, `name="overwrite"`)
	assert.Contains(t, body, `name="dryRun"`)
	assert.Contains(t, body, `data-mcp-import`)
}

func TestMCPImportRejectsEmptyConfig(t *testing.T) {
	backend, _, e := newMCPImportTestServer(t)
	rec := mcpImportPost(e, "config=&target=auto")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Paste a Claude MCP config document.")
	assert.Zero(t, backend.importCalls)
}

func TestMCPImportRejectsInvalidJSON(t *testing.T) {
	backend, _, e := newMCPImportTestServer(t)
	rec := mcpImportPost(e, "config=not-json&target=auto")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "not valid JSON")
	assert.Zero(t, backend.importCalls)
}

func TestMCPImportRejectsUnknownTarget(t *testing.T) {
	backend, _, e := newMCPImportTestServer(t)
	rec := mcpImportPost(e, "config=%7B%22mcpServers%22%3A%7B%7D%7D&target=carrier-pigeon")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Choose auto, server, or device.")
	assert.Zero(t, backend.importCalls)
}

func TestMCPImportCallsBackendAndRendersResult(t *testing.T) {
	backend, _, e := newMCPImportTestServer(t)
	backend.importResult = &MCPImportResult{
		Created: []string{"github"},
		Updated: []string{"files"},
		Skipped: []MCPImportSkip{{Name: "old", Reason: "already exists"}},
		Failed:  []MCPImportFailure{{Name: "bad", Error: "no transport"}},
	}
	form := "config=" + urlEncode(`{"mcpServers":{"github":{"command":"npx"}}}`) +
		"&target=device&device=dev-1&overwrite=1&dryRun=1"
	rec := mcpImportPost(e, form)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, backend.importCalls)
	assert.JSONEq(t, `{"mcpServers":{"github":{"command":"npx"}}}`, string(backend.lastImportConfig))
	assert.Equal(t, "device", backend.lastImportTarget)
	assert.Equal(t, "dev-1", backend.lastImportDevice)
	assert.True(t, backend.lastImportOver)
	assert.True(t, backend.lastImportDryRun)

	body := rec.Body.String()
	assert.Contains(t, body, "1 created")
	assert.Contains(t, body, "github")
	assert.Contains(t, body, "files")
	assert.Contains(t, body, "already exists")
	assert.Contains(t, body, "no transport")
	assert.Contains(t, body, `data-testid="mcp-import-result"`)
}

func TestMCPImportBackendErrorRenders(t *testing.T) {
	backend, _, e := newMCPImportTestServer(t)
	backend.importErr = fmt.Errorf("memory rejected the import")
	rec := mcpImportPost(e, "config="+urlEncode(`{"mcpServers":{"x":{"command":"run"}}}`)+"&target=auto")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Something went wrong.")
}

func urlEncode(s string) string {
	// Minimal escaping for the two characters that matter in these tests.
	r := strings.NewReplacer("{", "%7B", "}", "%7D", `"`, "%22", ":", "%3A", ",", "%2C")
	return r.Replace(s)
}
