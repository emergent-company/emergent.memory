package mcpregistry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/mcpregistry"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, mock *testutil.MockServer) *sdk.Client {
	t.Helper()
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		ProjectID: "proj_test123",
		OrgID:     "org_test123",
	})
	require.NoError(t, err)
	return client
}

func TestMCPRegistryList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-servers", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		resp := mcpregistry.APIResponse[[]mcpregistry.MCPServer]{
			Success: true,
			Data: []mcpregistry.MCPServer{
				{
					ID:        "srv-1",
					ProjectID: "proj_test123",
					Name:      "GitHub MCP",
					Enabled:   true,
					Type:      mcpregistry.ServerTypeSSE,
					ToolCount: 5,
				},
				{
					ID:        "srv-2",
					ProjectID: "proj_test123",
					Name:      "Local Stdio",
					Enabled:   false,
					Type:      mcpregistry.ServerTypeStdio,
					ToolCount: 3,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.List(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.Data, 2)
	assert.Equal(t, "GitHub MCP", result.Data[0].Name)
	assert.Equal(t, mcpregistry.ServerTypeSSE, result.Data[0].Type)
	assert.Equal(t, 5, result.Data[0].ToolCount)
	assert.False(t, result.Data[1].Enabled)
}

func TestMCPRegistryGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	serverURL := "http://localhost:8080/sse"
	mock.On("GET", "/api/admin/mcp-servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		desc := "Search GitHub repos"
		resp := mcpregistry.APIResponse[mcpregistry.MCPServerDetail]{
			Success: true,
			Data: mcpregistry.MCPServerDetail{
				MCPServer: mcpregistry.MCPServer{
					ID:        "srv-1",
					ProjectID: "proj_test123",
					Name:      "GitHub MCP",
					Enabled:   true,
					Type:      mcpregistry.ServerTypeSSE,
					URL:       &serverURL,
					ToolCount: 1,
				},
				Tools: []mcpregistry.MCPServerTool{
					{
						ID:          "tool-1",
						ServerID:    "srv-1",
						ToolName:    "search_repos",
						Description: &desc,
						Enabled:     true,
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.Get(context.Background(), "srv-1")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "GitHub MCP", result.Data.Name)
	assert.Equal(t, &serverURL, result.Data.URL)
	assert.Len(t, result.Data.Tools, 1)
	assert.Equal(t, "search_repos", result.Data.Tools[0].ToolName)
}

func TestMCPRegistryCreate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/admin/mcp-servers", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		var body mcpregistry.CreateMCPServerRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "New Server", body.Name)
		assert.Equal(t, mcpregistry.ServerTypeSSE, body.Type)

		resp := mcpregistry.APIResponse[mcpregistry.MCPServer]{
			Success: true,
			Data: mcpregistry.MCPServer{
				ID:      "srv-new",
				Name:    body.Name,
				Type:    body.Type,
				Enabled: true,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	serverURL := "http://localhost:9090/sse"
	result, err := client.MCPRegistry.Create(context.Background(), &mcpregistry.CreateMCPServerRequest{
		Name: "New Server",
		Type: mcpregistry.ServerTypeSSE,
		URL:  &serverURL,
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "srv-new", result.Data.ID)
	assert.Equal(t, "New Server", result.Data.Name)
}

func TestMCPRegistryDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/admin/mcp-servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		msg := "MCP server deleted"
		resp := mcpregistry.APIResponse[any]{
			Success: true,
			Message: &msg,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	err := client.MCPRegistry.Delete(context.Background(), "srv-1")
	assert.NoError(t, err)
}

func TestMCPRegistrySyncTools(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/admin/mcp-servers/srv-1/sync", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		msg := "synced 5 tools successfully"
		resp := mcpregistry.APIResponse[any]{
			Success: true,
			Message: &msg,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.SyncTools(context.Background(), "srv-1")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Message)
	assert.Equal(t, "synced 5 tools successfully", *result.Message)
}

func TestMCPRegistryInspect(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/admin/mcp-servers/srv-1/inspect", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		resp := mcpregistry.APIResponse[mcpregistry.MCPServerInspect]{
			Success: true,
			Data: mcpregistry.MCPServerInspect{
				ServerID:   "srv-1",
				ServerName: "GitHub MCP",
				ServerType: mcpregistry.ServerTypeSSE,
				Status:     "ok",
				LatencyMs:  42,
				ServerInfo: &mcpregistry.InspectServerInfo{
					Name:            "github-mcp",
					Version:         "1.0.0",
					ProtocolVersion: "2025-11-25",
				},
				Capabilities: &mcpregistry.InspectCapabilities{
					Tools:     true,
					Prompts:   false,
					Resources: true,
				},
				Tools: []mcpregistry.InspectTool{
					{Name: "search_repos", Description: "Search GitHub repos"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.Inspect(context.Background(), "srv-1")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "ok", result.Data.Status)
	assert.Equal(t, int64(42), result.Data.LatencyMs)
	assert.NotNil(t, result.Data.ServerInfo)
	assert.Equal(t, "github-mcp", result.Data.ServerInfo.Name)
	assert.True(t, result.Data.Capabilities.Tools)
	assert.False(t, result.Data.Capabilities.Prompts)
	assert.Len(t, result.Data.Tools, 1)
}

func TestMCPRegistryListTools(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-servers/srv-1/tools", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")

		desc := "Search GitHub repos"
		resp := mcpregistry.APIResponse[[]mcpregistry.MCPServerTool]{
			Success: true,
			Data: []mcpregistry.MCPServerTool{
				{
					ID:          "tool-1",
					ServerID:    "srv-1",
					ToolName:    "search_repos",
					Description: &desc,
					Enabled:     true,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.ListTools(context.Background(), "srv-1")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, "search_repos", result.Data[0].ToolName)
	assert.True(t, result.Data[0].Enabled)
}

func TestMCPRegistryToggleTool(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PATCH", "/api/admin/mcp-servers/srv-1/tools/tool-1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, false, body["enabled"])

		msg := "tool updated"
		resp := mcpregistry.APIResponse[any]{
			Success: true,
			Message: &msg,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	err := client.MCPRegistry.ToggleTool(context.Background(), "srv-1", "tool-1", false)
	assert.NoError(t, err)
}

func TestMCPRegistryUpdate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PATCH", "/api/admin/mcp-servers/srv-1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		var body mcpregistry.UpdateMCPServerRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.NotNil(t, body.Name)
		assert.Equal(t, "Updated Name", *body.Name)

		resp := mcpregistry.APIResponse[mcpregistry.MCPServer]{
			Success: true,
			Data: mcpregistry.MCPServer{
				ID:      "srv-1",
				Name:    *body.Name,
				Type:    mcpregistry.ServerTypeSSE,
				Enabled: true,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	name := "Updated Name"
	result, err := client.MCPRegistry.Update(context.Background(), "srv-1", &mcpregistry.UpdateMCPServerRequest{
		Name: &name,
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "Updated Name", result.Data.Name)
}

func TestMCPRegistrySetContext(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-servers", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "new_project")
		testutil.AssertHeader(t, r, "X-Org-ID", "new_org")

		resp := mcpregistry.APIResponse[[]mcpregistry.MCPServer]{
			Success: true,
			Data:    []mcpregistry.MCPServer{},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)
	client.SetContext("new_org", "new_project")

	result, err := client.MCPRegistry.List(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Success)
}

func TestMCPRegistryHTTPError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-servers/nonexistent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message":    "mcp_server not found: nonexistent",
			"statusCode": 404,
		})
	})

	client := newTestClient(t, mock)

	_, err := client.MCPRegistry.Get(context.Background(), "nonexistent")
	assert.Error(t, err)
}
