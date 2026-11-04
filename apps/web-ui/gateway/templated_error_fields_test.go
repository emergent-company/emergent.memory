package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestTemplatedErrorFieldsRedactRawBackendErrors drives the real handlers and
// loaders that assign the templated (non-pageError) error fields listed in
// issue #1689, injecting raw backend failures, and asserts each field is
// sanitized to memoryUnavailableMessage with none of pageErrorLeakTokens
// present. A field reverted to rendering err.Error() directly fails this test.
func TestTemplatedErrorFieldsRedactRawBackendErrors(t *testing.T) {
	cases := []struct {
		name  string
		build func(f *fakeMemory, raw error) string
	}{
		{
			name: "ChatsErr (agent dashboard)",
			build: func(f *fakeMemory, raw error) string {
				f.defs = map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}
				f.convErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.GET("/agents/:id", s.uiAgent)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1", nil))
				return rec.Body.String()
			},
		},
		{
			name: "ChatsErr (agent sessions)",
			build: func(f *fakeMemory, raw error) string {
				f.defs = map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}
				f.convErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.GET("/agents/:id/sessions", s.uiAgentSessions)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/sessions", nil))
				return rec.Body.String()
			},
		},
		{
			name: "StatusErr (embeddings)",
			build: func(f *fakeMemory, raw error) string {
				f.embeddingProgress = &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}}
				f.embeddingStatErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.GET("/embeddings", s.uiEmbeddings)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
				return rec.Body.String()
			},
		},
		{
			name: "CoverageErr (embeddings)",
			build: func(f *fakeMemory, raw error) string {
				f.embeddingProgress = &EmbeddingProgress{Objects: EmbeddingQueueStats{Pending: 3}}
				f.embeddingCoverageErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.GET("/embeddings", s.uiEmbeddings)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/embeddings", nil))
				return rec.Body.String()
			},
		},
		{
			name: "ToolsErr (connected devices)",
			build: func(f *fakeMemory, raw error) string {
				f.relaySessions = []RelaySession{{InstanceID: "node-1", Version: "1.0.0", ToolCount: 1, ConnectedAt: time.Now().Add(-time.Minute)}}
				f.relayToolsErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.GET("/settings/devices", s.uiConnectedDevices)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/devices?node=node-1", nil))
				return rec.Body.String()
			},
		},
		{
			name: "d.Err (providers panel)",
			build: func(f *fakeMemory, raw error) string {
				f.providerErr = raw
				s := &Server{cfg: Config{}, memory: f}
				return s.loadProviderPanel(context.Background()).Err.Error()
			},
		},
		{
			name: "GeneralErr (MCP server create)",
			build: func(f *fakeMemory, raw error) string {
				f.mcpServerCreateErr = raw
				s := &Server{cfg: Config{}, memory: f}
				e := echo.New()
				e.POST("/settings/mcp-servers/new", s.uiMCPCreate)
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/settings/mcp-servers/new", strings.NewReader("name=server-z&type=stdio&command=ignored"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				e.ServeHTTP(rec, req)
				return rec.Body.String()
			},
		},
		{
			name: "MCPActionErr (endpoint create)",
			build: func(f *fakeMemory, raw error) string {
				f.defs = map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}
				f.mcpEndpointCreateErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := agentMCPTestServer(s)
				rec := agentMCPPost(e, "/agents/a1/mcp-endpoint", "")
				return rec.Body.String()
			},
		},
		{
			name: "PendingErr (members)",
			build: func(f *fakeMemory, raw error) string {
				f.sentInvitesErr = raw
				s := &Server{cfg: Config{}, memory: f}
				e := echo.New()
				req := httptest.NewRequest(http.MethodGet, "/members", nil)
				rec := httptest.NewRecorder()
				var data membersPageData
				s.loadMembersData(e.NewContext(req, rec), &data)
				return data.PendingErr.Error()
			},
		},
		{
			name: "SaveErr (object type editor)",
			build: func(f *fakeMemory, raw error) string {
				f.compiled = &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}
				f.schemaPackErr = raw
				s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
				e := echo.New()
				e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)
				form := url.Values{}
				form.Set("_ui", "1")
				form.Set("description", "edited description")
				form.Add("property_name", "first")
				form.Add("property_type", "string")
				return schemaFormPost(e, "/schema/object-types/person", form).Body.String()
			},
		},
	}

	for _, tc := range cases {
		for _, fail := range rawBackendFailures() {
			t.Run(tc.name+"/"+fail.name, func(t *testing.T) {
				body := tc.build(&fakeMemory{}, fail.err)
				if !strings.Contains(body, memoryUnavailableMessage) {
					t.Errorf("missing sanitized message %q, got %q", memoryUnavailableMessage, body)
				}
				for _, leak := range pageErrorLeakTokens {
					if strings.Contains(body, leak) {
						t.Errorf("leaked %q", leak)
					}
				}
			})
		}
	}
}

// TestRedactBackendError covers the SaveErr helper directly: a real backend
// 5xx and a transport *url.Error are sanitized to memoryUnavailableMessage,
// while a locally-authored validation error (and nil) pass through unchanged.
func TestRedactBackendError(t *testing.T) {
	for _, fail := range rawBackendFailures() {
		got := redactBackendError(fail.err)
		if got == nil || got.Error() != memoryUnavailableMessage {
			t.Errorf("redactBackendError(%s) = %v, want %q", fail.name, got, memoryUnavailableMessage)
		}
	}

	local := errors.New(`property "x" needs a type`)
	if got := redactBackendError(local); got != local {
		t.Errorf("redactBackendError(local validation) = %v, want the same error unchanged", got)
	}
	if got := redactBackendError(nil); got != nil {
		t.Errorf("redactBackendError(nil) = %v, want nil", got)
	}
}
