package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/emergent-company/emergent/pkg/auth"
)

func TestMCPSessionWithAPIKey(t *testing.T) {
	e := echo.New()
	svc := &Service{}
	logger := slog.Default()
	h := NewHandler(svc, logger)

	testUser := &auth.AuthUser{
		ID:        "test-user-id",
		Email:     "test@example.com",
		ProjectID: "test-project-id",
	}

	apiKey := "test-api-key-12345"

	t.Run("initialize with X-API-Key creates session", func(t *testing.T) {
		initReq := Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
			Params: json.RawMessage(`{
				"protocolVersion": "2025-11-25",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0"}
			}`),
		}
		body, _ := json.Marshal(initReq)

		req := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		c.Set("user", testUser)

		resp := h.routeMethod(c, &initReq, testUser)

		assert.NotNil(t, resp)
		assert.Nil(t, resp.Error, "Initialize should succeed")
		assert.NotNil(t, resp.Result, "Initialize should return result")

		token := extractToken(c)
		assert.Equal(t, apiKey, token, "extractToken should return API key")

		session := h.getSession(token)
		assert.NotNil(t, session, "Session should exist for API key")
		assert.True(t, session.Initialized, "Session should be initialized")
	})

	t.Run("tools/list with X-API-Key uses existing session", func(t *testing.T) {
		toolsReq := Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "tools/list",
		}
		body, _ := json.Marshal(toolsReq)

		req := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		c.Set("user", testUser)

		resp := h.routeMethod(c, &toolsReq, testUser)

		assert.NotNil(t, resp)
		assert.Nil(t, resp.Error, "tools/list should succeed with existing session")
		assert.NotNil(t, resp.Result, "tools/list should return result")

		result, ok := resp.Result.(ToolsListResult)
		assert.True(t, ok, "Result should be ToolsListResult")
		assert.NotEmpty(t, result.Tools, "Should return tool definitions")
	})

	t.Run("Bearer token takes precedence over API key", func(t *testing.T) {
		bearerToken := "bearer-token-xyz"
		differentAPIKey := "different-api-key"

		initReq := Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`3`),
			Method:  "initialize",
			Params: json.RawMessage(`{
				"protocolVersion": "2025-11-25",
				"capabilities": {},
				"clientInfo": {"name": "test", "version": "1.0"}
			}`),
		}
		body, _ := json.Marshal(initReq)

		req := httptest.NewRequest(http.MethodPost, "/api/mcp/rpc", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		req.Header.Set("X-API-Key", differentAPIKey)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		c.Set("user", testUser)

		resp := h.routeMethod(c, &initReq, testUser)

		assert.NotNil(t, resp)
		assert.Nil(t, resp.Error)

		token := extractToken(c)
		assert.Equal(t, bearerToken, token, "Bearer token should take precedence")

		session := h.getSession(bearerToken)
		assert.NotNil(t, session, "Session should exist for Bearer token")

		sessionForAPIKey := h.getSession(differentAPIKey)
		assert.Nil(t, sessionForAPIKey, "Session should NOT exist for API key when Bearer is used")
	})
}
