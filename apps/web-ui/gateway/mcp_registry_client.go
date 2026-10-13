package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// --- Official MCP registry browse/install proxies ---
//
// These methods proxy memory's official-registry admin routes
// (/api/admin/mcp-registry/search and /install). Project scoping rides on the
// session X-Project-ID header via sessionHeaders, same as the mcp-servers CRUD
// methods in extras.go.

// MCPRegistryServer mirrors memory's RegistryServerDTO: one registry entry in a
// search result. HasRemotes/HasPackages drive the page's install-vs-guidance
// split — an entry with a remote transport is installable into the project; a
// stdio-only entry (packages, no remotes) shows connector guidance instead.
type MCPRegistryServer struct {
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description"`
	Version     string               `json:"version"`
	Repository  *MCPRegistryRepo     `json:"repository,omitempty"`
	HasRemotes  bool                 `json:"hasRemotes"`
	HasPackages bool                 `json:"hasPackages"`
	Packages    []MCPRegistryPackage `json:"packages,omitempty"`
	Remotes     []MCPRegistryRemote  `json:"remotes,omitempty"`
	EnvVars     []MCPRegistryEnvVar  `json:"envVars,omitempty"`
}

// MCPRegistryRepo mirrors memory's RegistryRepoDTO.
type MCPRegistryRepo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// MCPRegistryPackage mirrors memory's RegistryPackageDTO.
type MCPRegistryPackage struct {
	RegistryType string `json:"registryType"`
	Name         string `json:"name,omitempty"`
	Identifier   string `json:"identifier,omitempty"`
	Transport    string `json:"transport"`
}

// MCPRegistryRemote mirrors memory's RegistryRemoteDTO.
type MCPRegistryRemote struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// MCPRegistryEnvVar mirrors memory's RegistryEnvVarDTO.
type MCPRegistryEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// MCPRegistrySearchResult mirrors memory's RegistrySearchResultDTO.
type MCPRegistrySearchResult struct {
	Servers    []MCPRegistryServer `json:"servers"`
	NextCursor string              `json:"nextCursor,omitempty"`
	Count      int                 `json:"count"`
}

// MCPRegistryInstallResult mirrors memory's InstallResultDTO. Server reuses the
// gateway's MCPServer wire shape (memory's MCPServerDetailDTO is a superset);
// RequiredEnvVars names the environment variables the admin must set before the
// server works.
type MCPRegistryInstallResult struct {
	Server          *MCPServer             `json:"server"`
	RequiredEnvVars []MCPRegistryEnvVar    `json:"requiredEnvVars"`
	RequiredConfig  []MCPRequiredConfigKey `json:"requiredConfig"`
	Message         string                 `json:"message,omitempty"`
}

// mcpRegistryInstallRequest is the install POST body, mirroring memory's
// InstallFromRegistryDTO. Version and name are optional (empty = latest /
// derived local name). Target/TargetInstanceID are set only by the device
// install path; an empty target installs an outbound server on the shared host.
type mcpRegistryInstallRequest struct {
	RegistryName     string `json:"registryName"`
	Version          string `json:"version,omitempty"`
	Name             string `json:"name,omitempty"`
	Target           string `json:"target,omitempty"`
	TargetInstanceID string `json:"targetInstanceId,omitempty"`
}

// SearchRegistry searches the official MCP registry through memory. An empty
// query/limit/cursor omits the corresponding query param (memory defaults the
// limit to 20).
func (m *MemoryClient) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*MCPRegistrySearchResult, error) {
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
	path := "/api/admin/mcp-registry/search"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var env successEnvelope[MCPRegistrySearchResult]
	// The official registry upstream is slow/hanging: when it times out, memory
	// answers 502 registry_unavailable. That 502 is not a transient blip worth
	// replaying — a retry just multiplies page latency 3× (memory's full
	// registry timeout × maxMemoryAttempts). Issue the browse call without the
	// generic 502/503/504 retry.
	if err := m.doNoRetry(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// InstallRegistryServer installs a registry entry into the current project via
// memory's remote-only install. Empty version installs latest; empty name
// derives the local name from the registry name.
func (m *MemoryClient) InstallRegistryServer(ctx context.Context, registryName, version, name string) (*MCPRegistryInstallResult, error) {
	req := mcpRegistryInstallRequest{RegistryName: registryName, Version: version, Name: name}
	var env successEnvelope[MCPRegistryInstallResult]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-registry/install", req, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// InstallRegistryServerToDevice installs a registry entry's local package on a
// connected connector device (memory's device install path translates the
// entry's npm/pypi package into a stdio command and pushes it over the relay).
// An empty instanceID broadcasts to every opted-in device and is accepted only
// for project admins; the caller validates the field before calling.
func (m *MemoryClient) InstallRegistryServerToDevice(ctx context.Context, registryName, version, name, instanceID string) (*MCPRegistryInstallResult, error) {
	req := mcpRegistryInstallRequest{
		RegistryName:     registryName,
		Version:          version,
		Name:             name,
		Target:           "device",
		TargetInstanceID: instanceID,
	}
	var env successEnvelope[MCPRegistryInstallResult]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-registry/install", req, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
