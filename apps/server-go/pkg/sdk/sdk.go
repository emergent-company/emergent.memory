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
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chat"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chunks"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/health"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/mcp"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/orgs"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/users"
)

// Client is the main SDK client for the Emergent API.
type Client struct {
	auth      auth.Provider
	base      string
	orgID     string
	projectID string
	http      *http.Client

	// Service clients
	Documents *documents.Client
	Chunks    *chunks.Client
	Search    *search.Client
	Graph     *graph.Client
	Chat      *chat.Client
	Projects  *projects.Client
	Orgs      *orgs.Client
	Users     *users.Client
	APITokens *apitokens.Client
	Health    *health.Client
	MCP       *mcp.Client
}

// Config holds configuration for the SDK client.
type Config struct {
	ServerURL string
	Auth      AuthConfig
	OrgID     string // Optional: default organization ID
	ProjectID string // Optional: default project ID
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Mode      string // "apikey" or "oauth"
	APIKey    string // For API key mode
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
		authProvider = auth.NewAPIKeyProvider(cfg.Auth.APIKey)
	case "oauth":
		return nil, fmt.Errorf("OAuth mode not yet implemented - use NewWithDeviceFlow()")
	default:
		return nil, fmt.Errorf("invalid auth mode: %s (must be 'apikey' or 'oauth')", cfg.Auth.Mode)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	client := &Client{
		auth:      authProvider,
		base:      cfg.ServerURL,
		orgID:     cfg.OrgID,
		projectID: cfg.ProjectID,
		http:      httpClient,
	}

	// Initialize service clients
	client.Documents = documents.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Chunks = chunks.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Search = search.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Graph = graph.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Chat = chat.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Projects = projects.NewClient(client.http, client.base, client.auth)
	client.Orgs = orgs.NewClient(client.http, client.base, client.auth)
	client.Users = users.NewClient(client.http, client.base, client.auth)
	client.APITokens = apitokens.NewClient(client.http, client.base, client.auth)
	client.Health = health.NewClient(client.http, client.base)
	client.MCP = mcp.NewClient(client.http, client.base, client.auth)

	return client, nil
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

	fmt.Println("✓ Authentication successful!")

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	client := &Client{
		auth:      authProvider,
		base:      cfg.ServerURL,
		orgID:     cfg.OrgID,
		projectID: cfg.ProjectID,
		http:      httpClient,
	}

	// Initialize service clients
	client.Documents = documents.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Chunks = chunks.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Search = search.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Graph = graph.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Chat = chat.NewClient(client.http, client.base, client.auth, client.orgID, client.projectID)
	client.Projects = projects.NewClient(client.http, client.base, client.auth)
	client.Orgs = orgs.NewClient(client.http, client.base, client.auth)
	client.Users = users.NewClient(client.http, client.base, client.auth)
	client.APITokens = apitokens.NewClient(client.http, client.base, client.auth)
	client.Health = health.NewClient(client.http, client.base)
	client.MCP = mcp.NewClient(client.http, client.base, client.auth)

	return client, nil
}

// SetContext sets the default organization and project context for API calls.
func (c *Client) SetContext(orgID, projectID string) {
	c.orgID = orgID
	c.projectID = projectID

	// Update all service clients
	c.Documents.SetContext(orgID, projectID)
	c.Chunks.SetContext(orgID, projectID)
	c.Search.SetContext(orgID, projectID)
	c.Graph.SetContext(orgID, projectID)
	c.Chat.SetContext(orgID, projectID)
	c.Projects.SetContext(orgID, projectID)
	c.Orgs.SetContext(orgID, projectID)
	c.Users.SetContext(orgID, projectID)
	c.APITokens.SetContext(orgID, projectID)
	c.MCP.SetContext(orgID, projectID)
}

// Do executes an HTTP request with authentication.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Add context headers if set
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	// Execute request
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}
