package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestSSEStreamCloseIsIdempotent pins #2338: session termination (DELETE) and
// the GET handler's deferred cleanup both reach a stream, so closing must be
// safe twice.
func TestSSEStreamCloseIsIdempotent(t *testing.T) {
	stream := &SSEStream{Done: make(chan struct{})}
	stream.Close()
	stream.Close()

	select {
	case <-stream.Done:
	default:
		t.Fatal("Close did not close Done")
	}
}

// TestSSEStreamCloseConcurrent covers the real race shape: DELETE and the SSE
// handler's defer closing the same stream from two goroutines.
func TestSSEStreamCloseConcurrent(t *testing.T) {
	stream := &SSEStream{Done: make(chan struct{})}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream.Close()
		}()
	}
	wg.Wait()

	select {
	case <-stream.Done:
	default:
		t.Fatal("Done was not closed")
	}
}

// ============================================================================
// resources/* on the Streamable HTTP transport (#1089)
// ============================================================================

const streamTestProjectID = "11111111-1111-1111-1111-111111111111"

// streamEndpointRequest drives one JSON-RPC request through the project
// Streamable HTTP endpoint and decodes the response.
func streamEndpointRequest(t *testing.T, h *StreamableHTTPHandler, user *auth.AuthUser, method string, params any, sessionID string) (*httptest.ResponseRecorder, Response) {
	t.Helper()

	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method}
	if params != nil {
		body, err := json.Marshal(params)
		require.NoError(t, err)
		req.Params = body
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	httpReq.Header.Set("Accept", "application/json")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httpReq, rec)
	c.Set(string(auth.UserContextKey), user)
	require.NoError(t, h.HandleUnifiedEndpoint(c))

	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return rec, resp
}

// TestStreamableEndpointServesAdvertisedResourcesCapability is the #1089
// regression: initialize advertises the resources capability, so resources/list
// on /api/mcp must answer with the resource catalog instead of -32601.
func TestStreamableEndpointServesAdvertisedResourcesCapability(t *testing.T) {
	h := NewStreamableHTTPHandler(&Service{}, slog.Default())
	user := &auth.AuthUser{ID: "user-1", ProjectID: streamTestProjectID, APITokenID: "tok-1", Scopes: []string{"data:read"}}

	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "cloudflare-portal", Version: "1.0"},
	}, "")
	require.Nil(t, initResp.Error)
	result, ok := initResp.Result.(map[string]any)
	require.True(t, ok, "initialize result should be an object")
	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok)
	_, advertisesResources := capabilities["resources"]
	require.True(t, advertisesResources, "initialize must advertise the resources capability")

	sessionID := initRec.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	_, listResp := streamEndpointRequest(t, h, user, "resources/list", nil, sessionID)
	require.Nil(t, listResp.Error, "resources/list must not return method-not-found: %+v", listResp.Error)

	resources, ok := listResp.Result.(map[string]any)["resources"].([]any)
	require.True(t, ok, "resources/list result should carry a resources array")
	assert.Len(t, resources, 6, "the server publishes six resources")
	uris := make([]string, 0, len(resources))
	for _, entry := range resources {
		resource, ok := entry.(map[string]any)
		require.True(t, ok)
		uris = append(uris, resource["uri"].(string))
	}
	assert.Contains(t, uris, "memory://schema/entity-types")
	assert.Contains(t, uris, "memory://project/{project_id}/metadata")
}

// TestStreamableResourcesListRequiresInitializedSession keeps resources/list
// behind the same session gate as the other session-scoped methods.
func TestStreamableResourcesListRequiresInitializedSession(t *testing.T) {
	h := NewStreamableHTTPHandler(&Service{}, slog.Default())
	user := &auth.AuthUser{ID: "user-1", ProjectID: streamTestProjectID, APITokenID: "tok-1", Scopes: []string{"data:read"}}

	_, resp := streamEndpointRequest(t, h, user, "resources/list", nil, "")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrCodeInvalidRequest, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "initialize")
}

// TestStreamableResourcesReadValidatesParams proves resources/read is dispatched
// and validates its input instead of falling through to method-not-found.
func TestStreamableResourcesReadValidatesParams(t *testing.T) {
	h := NewStreamableHTTPHandler(&Service{}, slog.Default())
	user := &auth.AuthUser{ID: "user-1", ProjectID: streamTestProjectID, APITokenID: "tok-1", Scopes: []string{"data:read"}}

	initRec, initResp := streamEndpointRequest(t, h, user, "initialize", InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	}, "")
	require.Nil(t, initResp.Error)
	sessionID := initRec.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	_, missingURI := streamEndpointRequest(t, h, user, "resources/read", map[string]any{}, sessionID)
	require.NotNil(t, missingURI.Error)
	assert.Equal(t, ErrCodeInvalidParams, missingURI.Error.Code)

	// An unserved URI is reported as not-found, which proves the call reached
	// ReadResource rather than the method-not-found default branch.
	_, unknownURI := streamEndpointRequest(t, h, user, "resources/read", map[string]any{"uri": "memory://does/not-exist"}, sessionID)
	require.NotNil(t, unknownURI.Error)
	assert.Equal(t, ErrCodeNotFound, unknownURI.Error.Code)
	assert.Contains(t, unknownURI.Error.Message, "memory://does/not-exist")
}

// TestStreamableResourcesDeniedForRestrictedInstance proves the streamable
// transport applies the same share-instance content gate to resources as it
// does to prompts.
func TestStreamableResourcesDeniedForRestrictedInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj-1", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	h := NewStreamableHTTPHandler(&Service{shareInstances: store, shareTokens: &fakeTokenSvc{}, agentDir: &fakeAgentDir{}}, slog.Default())
	session := &MCPSession{ID: "s1", ProjectID: "proj-1", Initialized: true}
	user := &auth.AuthUser{ID: "u", ProjectID: "proj-1", APITokenID: "tok-1", Scopes: []string{"data:read"}}

	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/mcp", nil), httptest.NewRecorder())

	listResp := h.handleResourcesList(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`)}, session, user)
	require.NotNil(t, listResp.Error)
	assert.Equal(t, ErrCodeForbidden, listResp.Error.Code)

	readResp := h.handleResourcesRead(c, &Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Params: json.RawMessage(`{"uri":"memory://schema/entity-types"}`)}, session, user)
	require.NotNil(t, readResp.Error)
	assert.Equal(t, ErrCodeForbidden, readResp.Error.Code)
}
