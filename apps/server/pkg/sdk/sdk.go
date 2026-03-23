// Package sdk provides a Go client library for the Emergent API.
//
// The SDK supports both standalone (API key) and full deployment (OAuth) authentication modes.
//
// Example usage with API key:
//
//	client, err := sdk.New(sdk.Config{
//		ServerURL: "http://localhost:9090",
//		Auth: sdk.AuthConfig{
//			Mode:   "apikey",
//			APIKey: "emt_abc123...",
//		},
//	})
//
// Example usage with OAuth:
//
//	client, err := sdk.NewWithDeviceFlow(sdk.Config{
//		ServerURL: "https://api.emergent-company.ai",
//		Auth: sdk.AuthConfig{
//			Mode:      "oauth",
//			ClientID:  "emergent-sdk",
//			CredsPath: "~/.emergent/credentials.json",
//		},
//	})
package sdk

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apidocs"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/branches"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chunking"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chunks"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/datasources"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/discoveryjobs"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/documents"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/embeddingpolicies"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/health"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/integrations"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/invitations"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcp"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpregistry"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/monitoring"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/notifications"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/orgs"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemaregistry"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/search"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/superadmin"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/tasks"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/useractivity"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/users"
)

// Client is the main SDK client for the Emergent API.
type Client struct {
	auth      auth.Provider
	base      string
	mu        sync.RWMutex
	orgID     string
	projectID string
	http      *http.Client

	// Service clients — context-scoped (use org/project headers)
	Documents        *documents.Client
	Chunks           *chunks.Client
	Search           *search.Client
	Graph            *graph.Client
	Chat             *chat.Client
	Projects         *projects.Client
	Orgs             *orgs.Client
	Users            *users.Client
	APITokens        *apitokens.Client
	MCP              *mcp.Client
	MCPRegistry      *mcpregistry.Client
	Branches         *branches.Client
	UserActivity     *useractivity.Client
	SchemaRegistry   *schemaregistry.Client
	Notifications    *notifications.Client
	Tasks            *tasks.Client
	Monitoring       *monitoring.Client
	Agents           *agents.Client
	AgentDefinitions *agentdefinitions.Client
	DataSources      *datasources.Client
	DiscoveryJobs    *discoveryjobs.Client
	EmbeddingPolicy  *embeddingpolicies.Client
	Integrations     *integrations.Client
	Schemas          *schemas.Client
	Skills           *skills.Client
	Chunking         *chunking.Client
	Invitations      *invitations.Client

	// Service clients — non-context (no org/project needed)
	Health     *health.Client
	Superadmin *superadmin.Client
	APIDocs    *apidocs.Client
	Provider   *provider.Client
}

// Config holds configuration for the SDK client.
type Config struct {
	ServerURL  string
	Auth       AuthConfig
	OrgID      string       // Optional: default organization ID
	ProjectID  string       // Optional: default project ID
	HTTPClient *http.Client // Optional: custom HTTP client (defaults to 30s timeout)
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Mode      string // "apikey", "apitoken", or "oauth"
	APIKey    string // For API key mode (standalone X-API-Key) or API token mode (emt_* Bearer token)
	CredsPath string // For OAuth credential storage
	ClientID  string // For OAuth mode
}

// New creates a new Emergent API client.
func New(cfg Config) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("ServerURL is required")
	}

	// Create auth provider based on mode
	var authProvider auth.Provider
	switch cfg.Auth.Mode {
	case "apikey":
		if cfg.Auth.APIKey == "" {
			return nil, fmt.Errorf("APIKey is required for apikey mode")
		}
		// Auto-detect project API tokens (emt_ prefix) and use Bearer auth
		if auth.IsAPIToken(cfg.Auth.APIKey) {
			authProvider = auth.NewAPITokenProvider(cfg.Auth.APIKey)
		} else {
			authProvider = auth.NewAPIKeyProvider(cfg.Auth.APIKey)
		}
	case "apitoken":
		if cfg.Auth.APIKey == "" {
			return nil, fmt.Errorf("APIKey is required for apitoken mode")
		}
		authProvider = auth.NewAPITokenProvider(cfg.Auth.APIKey)
	case "oauth":
		return nil, fmt.Errorf("OAuth mode not yet implemented - use NewWithDeviceFlow()")
	default:
		return nil, fmt.Errorf("invalid auth mode: %s (must be 'apikey', 'apitoken', or 'oauth')", cfg.Auth.Mode)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	client := &Client{
		auth:      authProvider,
		base:      cfg.ServerURL,
		orgID:     cfg.OrgID,
		projectID: cfg.ProjectID,
		http:      httpClient,
	}

	// Initialize service clients
	initClients(client)

	return client, nil
}

// NewFromEnv creates a new Emergent API client by auto-discovering configuration.
//
// Resolution order (highest priority wins):
//  1. ~/.memory/config.yaml — CLI config file
//  2. .env — dotenv file (walked up from current directory)
//  3. .env.local — local overrides (walked up from current directory)
//  4. MEMORY_* environment variables
//
// Recognised variables / YAML keys:
//   - MEMORY_SERVER_URL / server_url — server URL (falls back to http://localhost:3002)
//   - MEMORY_API_KEY / api_key — API key or emt_* token (required)
//   - MEMORY_PROJECT_TOKEN / project_token — project-scoped emt_* token (overrides api_key)
//   - MEMORY_ORG_ID / org_id — default organisation ID
//   - MEMORY_PROJECT_ID / project_id — default project ID
//
// MEMORY_API_URL is accepted as an alias for MEMORY_SERVER_URL.
func NewFromEnv() (*Client, error) {
	discovered := loadEnvConfig()

	// project_token takes precedence over api_key as the credential
	apiKey := discovered.ProjectToken
	if apiKey == "" {
		apiKey = discovered.APIKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key found: set MEMORY_API_KEY, add api_key to ~/.memory/config.yaml, or create a .env.local file")
	}

	serverURL := discovered.ServerURL
	if serverURL == "" {
		serverURL = "http://localhost:3002"
	}

	return New(Config{
		ServerURL: serverURL,
		Auth: AuthConfig{
			Mode:   "apikey",
			APIKey: apiKey,
		},
		OrgID:     discovered.OrgID,
		ProjectID: discovered.ProjectID,
	})
}

// NewWithDeviceFlow creates a new client using OAuth device flow.
// This will initiate the device flow and wait for the user to complete authorization.
func NewWithDeviceFlow(cfg Config) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("ServerURL is required")
	}
	if cfg.Auth.ClientID == "" {
		return nil, fmt.Errorf("ClientID is required for OAuth mode")
	}
	if cfg.Auth.CredsPath == "" {
		return nil, fmt.Errorf("CredsPath is required for OAuth mode")
	}

	// Discover OIDC configuration
	oidcConfig, err := auth.DiscoverOIDC(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}

	// Create OAuth provider
	authProvider := auth.NewOAuthProvider(oidcConfig, cfg.Auth.ClientID, cfg.Auth.CredsPath)

	// Initiate device flow
	deviceResp, err := authProvider.InitiateDeviceFlow(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to initiate device flow: %w", err)
	}

	// Display instructions to user
	fmt.Println("\n=== OAuth Device Flow Authentication ===")
	fmt.Printf("\nPlease visit the following URL and enter the code:\n")
	fmt.Printf("  URL:  %s\n", deviceResp.VerificationURI)
	fmt.Printf("  Code: %s\n\n", deviceResp.UserCode)

	if deviceResp.VerificationURIComplete != "" {
		fmt.Printf("Or visit this URL with the code pre-filled:\n")
		fmt.Printf("  %s\n\n", deviceResp.VerificationURIComplete)
	}

	fmt.Println("Waiting for authorization...")

	// Poll for token
	if err := authProvider.PollForToken(context.Background(), deviceResp.DeviceCode, deviceResp.Interval, deviceResp.ExpiresIn); err != nil {
		return nil, fmt.Errorf("device flow failed: %w", err)
	}

	fmt.Println("Authentication successful!")

	// Create HTTP client
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	client := &Client{
		auth:      authProvider,
		base:      cfg.ServerURL,
		orgID:     cfg.OrgID,
		projectID: cfg.ProjectID,
		http:      httpClient,
	}

	// Initialize service clients
	initClients(client)

	return client, nil
}

// initClients initializes all service sub-clients on the given Client.
func initClients(c *Client) {
	// Context-scoped clients (org/project aware)
	c.Documents = documents.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Chunks = chunks.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Search = search.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Graph = graph.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Chat = chat.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Projects = projects.NewClient(c.http, c.base, c.auth)
	c.Orgs = orgs.NewClient(c.http, c.base, c.auth)
	c.Users = users.NewClient(c.http, c.base, c.auth)
	c.APITokens = apitokens.NewClient(c.http, c.base, c.auth)
	c.MCP = mcp.NewClient(c.http, c.base, c.auth)
	c.MCPRegistry = mcpregistry.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Branches = branches.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.UserActivity = useractivity.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.SchemaRegistry = schemaregistry.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Notifications = notifications.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Tasks = tasks.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Monitoring = monitoring.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Agents = agents.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.AgentDefinitions = agentdefinitions.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.DataSources = datasources.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.DiscoveryJobs = discoveryjobs.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.EmbeddingPolicy = embeddingpolicies.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Integrations = integrations.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Schemas = schemas.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Skills = skills.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Chunking = chunking.NewClient(c.http, c.base, c.auth, c.orgID, c.projectID)
	c.Invitations = invitations.NewClient(c.http, c.base, c.auth)

	// Non-context clients
	c.Health = health.NewClient(c.http, c.base)
	c.Superadmin = superadmin.NewClient(c.http, c.base, c.auth)
	c.APIDocs = apidocs.NewClient(c.http, c.base, c.auth)
	c.Provider = provider.NewClient(c.http, c.base, c.auth)
}

// SetContext sets the default organization and project context for API calls.
// It is safe to call concurrently with API methods. The lock is held for the
// entire update to ensure all sub-clients see a consistent context atomically.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.orgID = orgID
	c.projectID = projectID

	// Update all context-scoped service clients while holding the lock
	// to prevent concurrent API calls from seeing partially-updated state.
	c.Documents.SetContext(orgID, projectID)
	c.Chunks.SetContext(orgID, projectID)
	c.Search.SetContext(orgID, projectID)
	c.Graph.SetContext(orgID, projectID)
	c.Chat.SetContext(orgID, projectID)
	c.MCP.SetContext(projectID)
	c.MCPRegistry.SetContext(orgID, projectID)
	c.Branches.SetContext(orgID, projectID)
	c.UserActivity.SetContext(orgID, projectID)
	c.SchemaRegistry.SetContext(orgID, projectID)
	c.Notifications.SetContext(orgID, projectID)
	c.Tasks.SetContext(orgID, projectID)
	c.Monitoring.SetContext(orgID, projectID)
	c.Agents.SetContext(orgID, projectID)
	c.AgentDefinitions.SetContext(orgID, projectID)
	c.DataSources.SetContext(orgID, projectID)
	c.DiscoveryJobs.SetContext(orgID, projectID)
	c.EmbeddingPolicy.SetContext(orgID, projectID)
	c.Integrations.SetContext(orgID, projectID)
	c.Schemas.SetContext(orgID, projectID)
	c.Skills.SetContext(orgID, projectID)
	c.Chunking.SetContext(orgID, projectID)
	// Note: Health, Superadmin, APIDocs are non-context clients — no SetContext needed
	// Note: Projects, Orgs, Users, APITokens don't use org/project context in requests
}

// AuthenticateRequest applies authentication headers to the given request
// without executing it. This is useful when the caller needs to manage the
// HTTP transport itself (e.g. for streaming responses with a custom timeout).
func (c *Client) AuthenticateRequest(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// Do executes an HTTP request with authentication.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Add context headers if set
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()

	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}

	// Execute request
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// Close releases resources held by the client, including idle HTTP connections.
// After calling Close, the client should not be used.
func (c *Client) Close() {
	if t, ok := c.http.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}
