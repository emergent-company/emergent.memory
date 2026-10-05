package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// RegistryRepo mirrors memory's RegistryRepoDTO: the source repository of a
// registry entry.
type RegistryRepo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryEnvVar mirrors memory's RegistryEnvVarDTO: a declared environment
// variable the entry needs.
type RegistryEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// RegistryPackage mirrors memory's RegistryPackageDTO: how to run the entry
// locally.
type RegistryPackage struct {
	RegistryType     string   `json:"registryType"` // npm, pypi, oci, nuget, mcpb
	Name             string   `json:"name,omitempty"`
	Identifier       string   `json:"identifier,omitempty"` // e.g. "@modelcontextprotocol/server-github"
	Transport        string   `json:"transport"`            // stdio, sse, streamable-http
	RuntimeArguments []string `json:"runtimeArguments,omitempty"`
	PackageArguments []string `json:"packageArguments,omitempty"`
}

// RegistryRemote mirrors memory's RegistryRemoteDTO: a remote endpoint.
type RegistryRemote struct {
	Type string `json:"type"` // streamable-http, sse
	URL  string `json:"url"`
}

// RegistryHeader mirrors memory's RegistryHeaderDTO: a declared HTTP header.
type RegistryHeader struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
	Value       string `json:"value,omitempty"`
}

// RegistryServer mirrors memory's RegistryServerDTO: one registry entry.
type RegistryServer struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Repository  *RegistryRepo     `json:"repository,omitempty"`
	HasRemotes  bool              `json:"hasRemotes"`
	HasPackages bool              `json:"hasPackages"`
	Packages    []RegistryPackage `json:"packages,omitempty"`
	Remotes     []RegistryRemote  `json:"remotes,omitempty"`
	EnvVars     []RegistryEnvVar  `json:"envVars,omitempty"`
	Headers     []RegistryHeader  `json:"headers,omitempty"`
}

// RegistrySearchResult mirrors memory's RegistrySearchResultDTO.
type RegistrySearchResult struct {
	Servers    []RegistryServer `json:"servers"`
	NextCursor string           `json:"nextCursor,omitempty"`
	Count      int              `json:"count"`
}

// SearchRegistry searches the official MCP registry through Memory.
// GET /api/admin/mcp-registry/search?q=&limit=&cursor=
func (c *Client) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*RegistrySearchResult, error) {
	u := c.base + "/api/admin/mcp-registry/search"
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[RegistrySearchResult]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("registry search failed: %s", envelopeMessage(result.Error, result.Message))
	}
	return &result.Data, nil
}

// GetRegistryServer fetches one registry entry by name. The name may contain a
// slash (e.g. "io.github.ChromeDevTools/chrome-devtools-mcp") and is path-escaped.
// GET /api/admin/mcp-registry/servers/{name}
func (c *Client) GetRegistryServer(ctx context.Context, name string) (*RegistryServer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/admin/mcp-registry/servers/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[RegistryServer]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("registry lookup failed: %s", envelopeMessage(result.Error, result.Message))
	}
	return &result.Data, nil
}

// envelopeMessage renders the first non-empty envelope error/message field.
func envelopeMessage(e, m *string) string {
	if e != nil && *e != "" {
		return *e
	}
	if m != nil && *m != "" {
		return *m
	}
	return "unknown error"
}
