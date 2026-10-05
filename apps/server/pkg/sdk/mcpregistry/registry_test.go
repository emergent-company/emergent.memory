package mcpregistry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpregistry"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchRegistry(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-registry/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")
		assert.Equal(t, "chrome", r.URL.Query().Get("q"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		assert.Equal(t, "abc", r.URL.Query().Get("cursor"))

		resp := mcpregistry.APIResponse[mcpregistry.RegistrySearchResult]{
			Success: true,
			Data: mcpregistry.RegistrySearchResult{
				Servers: []mcpregistry.RegistryServer{
					{Name: "io.github.x/y", Version: "1.0.0", Title: "X/Y", HasPackages: true},
				},
				NextCursor: "abc123",
				Count:      1,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.SearchRegistry(context.Background(), "chrome", 10, "abc")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Count)
	assert.Equal(t, "abc123", result.NextCursor)
	require.Len(t, result.Servers, 1)
	assert.Equal(t, "io.github.x/y", result.Servers[0].Name)
	assert.Equal(t, "1.0.0", result.Servers[0].Version)
}

func TestSearchRegistryNoParams(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-registry/search", func(w http.ResponseWriter, r *http.Request) {
		// No query string forwarded when all params are empty/zero.
		assert.Equal(t, "", r.URL.RawQuery)

		resp := mcpregistry.APIResponse[mcpregistry.RegistrySearchResult]{
			Success: true,
			Data:    mcpregistry.RegistrySearchResult{Servers: []mcpregistry.RegistryServer{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.SearchRegistry(context.Background(), "", 0, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Count)
}

func TestGetRegistryServerEscapesName(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// r.URL.Path is the decoded path (the escaped %2F decodes to a real slash),
	// which is the key testutil.MockServer matches on.
	mock.On("GET", "/api/admin/mcp-registry/servers/io.github.x/y", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test123")
		// The raw escaped path must preserve the %2F so the server's router can
		// treat the name as a single segment.
		assert.True(t, strings.Contains(r.URL.EscapedPath(), "io.github.x%2Fy"),
			"expected escaped path %q to contain io.github.x%%2Fy", r.URL.EscapedPath())

		resp := mcpregistry.APIResponse[mcpregistry.RegistryServer]{
			Success: true,
			Data: mcpregistry.RegistryServer{
				Name:        "io.github.x/y",
				Title:       "X/Y",
				Description: "desc",
				Version:     "2.3.4",
				HasPackages: true,
				Packages: []mcpregistry.RegistryPackage{
					{RegistryType: "npm", Name: "x-y", Identifier: "x-y", Transport: "stdio"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.GetRegistryServer(context.Background(), "io.github.x/y")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "io.github.x/y", result.Name)
	assert.Equal(t, "2.3.4", result.Version)
	require.Len(t, result.Packages, 1)
	assert.Equal(t, "npm", result.Packages[0].RegistryType)
	assert.Equal(t, "stdio", result.Packages[0].Transport)
}

func TestGetRegistryServerDecodesFullDetail(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-registry/servers/gh", func(w http.ResponseWriter, r *http.Request) {
		resp := mcpregistry.APIResponse[mcpregistry.RegistryServer]{
			Success: true,
			Data: mcpregistry.RegistryServer{
				Name:        "io.github.github/github-mcp-server",
				Title:       "GitHub MCP",
				Description: "Search GitHub",
				Version:     "1.2.0",
				Repository:  &mcpregistry.RegistryRepo{URL: "https://github.com/x", Source: "github"},
				HasRemotes:  true,
				HasPackages: true,
				Packages: []mcpregistry.RegistryPackage{
					{
						RegistryType:     "npm",
						Name:             "github",
						Identifier:       "@modelcontextprotocol/server-github",
						Version:          "2.3.4",
						Transport:        "stdio",
						RuntimeArguments: []string{"--log-level", "debug"},
						PackageArguments: []string{"serve"},
					},
				},
				Remotes: []mcpregistry.RegistryRemote{
					{Type: "streamable-http", URL: "https://api.github.com/mcp"},
				},
				EnvVars: []mcpregistry.RegistryEnvVar{
					{Name: "GITHUB_TOKEN", Description: "PAT", IsRequired: true, IsSecret: true},
				},
				Headers: []mcpregistry.RegistryHeader{
					{Name: "Authorization", Description: "Bearer", IsRequired: true, IsSecret: true, Value: "Bearer {api_key}"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := newTestClient(t, mock)

	result, err := client.MCPRegistry.GetRegistryServer(context.Background(), "gh")
	require.NoError(t, err)
	assert.Equal(t, "GitHub MCP", result.Title)
	assert.NotNil(t, result.Repository)
	assert.Equal(t, "github", result.Repository.Source)
	assert.True(t, result.HasRemotes)
	assert.True(t, result.HasPackages)
	require.Len(t, result.Remotes, 1)
	assert.Equal(t, "streamable-http", result.Remotes[0].Type)
	require.Len(t, result.EnvVars, 1)
	assert.True(t, result.EnvVars[0].IsRequired)
	assert.True(t, result.EnvVars[0].IsSecret)
	require.Len(t, result.Packages, 1)
	assert.Equal(t, []string{"--log-level", "debug"}, result.Packages[0].RuntimeArguments)
	assert.Equal(t, []string{"serve"}, result.Packages[0].PackageArguments)
	assert.Equal(t, "2.3.4", result.Packages[0].Version)
	require.Len(t, result.Headers, 1)
	assert.Equal(t, "Authorization", result.Headers[0].Name)
	assert.Equal(t, "Bearer {api_key}", result.Headers[0].Value)
	assert.True(t, result.Headers[0].IsSecret)
}

func TestGetRegistryServerUpstreamError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/admin/mcp-registry/servers/nope", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "bad_request", "message": "failed to get registry server: server not found"},
		})
	})

	client := newTestClient(t, mock)

	_, err := client.MCPRegistry.GetRegistryServer(context.Background(), "nope")
	require.Error(t, err)
}
