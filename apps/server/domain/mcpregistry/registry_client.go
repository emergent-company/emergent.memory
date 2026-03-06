package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// DefaultRegistryBaseURL is the official MCP registry API endpoint.
	DefaultRegistryBaseURL = "https://registry.modelcontextprotocol.io"

	// registryAPIVersion is the API version prefix for all requests.
	registryAPIVersion = "v0.1"

	// defaultRegistryTimeout is the HTTP client timeout for registry requests.
	defaultRegistryTimeout = 15 * time.Second

	// defaultSearchLimit is the default number of results per search query.
	defaultSearchLimit = 20
)

// RegistryClient is an HTTP client for the official MCP registry API.
type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRegistryClient creates a new RegistryClient with the default base URL.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		baseURL: DefaultRegistryBaseURL,
		httpClient: &http.Client{
			Timeout: defaultRegistryTimeout,
		},
	}
}

// NewRegistryClientWithBaseURL creates a new RegistryClient with a custom base URL
// (useful for testing).
func NewRegistryClientWithBaseURL(baseURL string) *RegistryClient {
	return &RegistryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultRegistryTimeout,
		},
	}
}

// --- Registry API Response Types ---

// RegistrySearchResponse is the top-level response for server search/list.
type RegistrySearchResponse struct {
	Servers  []RegistryServerEntry `json:"servers"`
	Metadata RegistryMetadata      `json:"metadata"`
}

// RegistryServerEntry is a single server entry in search results.
type RegistryServerEntry struct {
	Server RegistryServer `json:"server"`
	Meta   RegistryMeta   `json:"_meta,omitempty"`
}

// RegistryServerResponse is the response for a single server version lookup.
type RegistryServerResponse struct {
	Server RegistryServer `json:"server"`
	Meta   RegistryMeta   `json:"_meta,omitempty"`
}

// RegistryServer is the core server object from the registry.
type RegistryServer struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Title       string             `json:"title,omitempty"`
	Version     string             `json:"version"`
	Repository  *RegistryRepo      `json:"repository,omitempty"`
	Packages    []RegistryPackage  `json:"packages,omitempty"`
	Remotes     []RegistryRemote   `json:"remotes,omitempty"`
	Tools       []RegistryToolInfo `json:"tools,omitempty"`
}

// RegistryRepo contains repository metadata.
type RegistryRepo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryPackage describes how to run the server locally.
type RegistryPackage struct {
	RegistryType         string             `json:"registryType"` // npm, pypi, oci, nuget, mcpb
	Name                 string             `json:"name"`
	Identifier           string             `json:"identifier,omitempty"` // e.g. "@modelcontextprotocol/server-github"
	Version              string             `json:"version,omitempty"`
	Transport            RegistryTransport  `json:"transport"`
	EnvironmentVariables []RegistryEnvVar   `json:"environmentVariables,omitempty"`
	RuntimeArguments     []RegistryArgument `json:"runtimeArguments,omitempty"`
	PackageArguments     []RegistryArgument `json:"packageArguments,omitempty"`
}

// RegistryTransport describes the transport type for a package.
type RegistryTransport struct {
	Type string `json:"type"` // stdio, sse, streamable-http
}

// RegistryRemote describes a remote server endpoint.
type RegistryRemote struct {
	Type                 string           `json:"type"` // streamable-http, sse
	URL                  string           `json:"url"`
	Headers              []RegistryHeader `json:"headers,omitempty"`
	EnvironmentVariables []RegistryEnvVar `json:"environmentVariables,omitempty"`
}

// RegistryEnvVar describes an environment variable needed by the server.
type RegistryEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// RegistryHeader describes an HTTP header needed for remote connections.
type RegistryHeader struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
	Value       string `json:"value,omitempty"` // May contain placeholders like "{api_key}"
}

// RegistryArgument describes a runtime or package argument.
type RegistryArgument struct {
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	IsRequired   bool     `json:"isRequired"`
	Value        string   `json:"value,omitempty"`
	Default      string   `json:"default,omitempty"`
	Enum         []string `json:"enum,omitempty"`
	IsRepeatable bool     `json:"isRepeatable"`
}

// RegistryToolInfo describes a tool provided by the registry server.
type RegistryToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// RegistryMeta contains metadata about a server entry.
type RegistryMeta struct {
	Official *RegistryOfficialMeta `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// RegistryOfficialMeta contains official registry metadata.
type RegistryOfficialMeta struct {
	Status      string `json:"status,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	IsLatest    bool   `json:"isLatest,omitempty"`
}

// RegistryMetadata contains pagination info for list responses.
type RegistryMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count,omitempty"`
}

// --- Client Methods ---

// SearchServers searches the official MCP registry for servers matching the query.
// The query parameter does case-insensitive substring search on server names.
// Returns paginated results; use cursor from RegistryMetadata.NextCursor for next page.
func (c *RegistryClient) SearchServers(ctx context.Context, query string, limit int, cursor string) (*RegistrySearchResponse, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{}
	if query != "" {
		params.Set("search", query)
	}
	params.Set("version", "latest")
	params.Set("limit", fmt.Sprintf("%d", limit))
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	reqURL := fmt.Sprintf("%s/%s/servers?%s", c.baseURL, registryAPIVersion, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}

	var result RegistrySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// GetServer fetches the latest version of a server from the official MCP registry.
// The name should be in the registry format (e.g. "io.github.github/github-mcp-server").
func (c *RegistryClient) GetServer(ctx context.Context, name string) (*RegistryServerResponse, error) {
	return c.GetServerVersion(ctx, name, "latest")
}

// GetServerVersion fetches a specific version of a server from the official MCP registry.
// Use "latest" as the version to get the most recent version.
func (c *RegistryClient) GetServerVersion(ctx context.Context, name string, version string) (*RegistryServerResponse, error) {
	if name == "" {
		return nil, fmt.Errorf("server name is required")
	}
	if version == "" {
		version = "latest"
	}

	// Server names contain "/" which must be URL-encoded in the path
	encodedName := url.PathEscape(name)
	reqURL := fmt.Sprintf("%s/%s/servers/%s/versions/%s", c.baseURL, registryAPIVersion, encodedName, url.PathEscape(version))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching registry server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("server %q not found in registry", name)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}

	var result RegistryServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
