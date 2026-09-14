// Package memoryapi is a thin wrapper around the Memory Go SDK so the rest of
// the connector does not depend on SDK types directly. It exposes only the
// project and API-token operations the connector needs, returning small
// connector-owned structs.
package memoryapi

import (
	"context"
	"fmt"
	"net/http"

	sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/apitokens"
)

// Project is the connector-owned view of a Memory project.
type Project struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"org_id,omitempty"`
}

// Token is the connector-owned view of an API token (never carries the secret
// value except at creation time).
type Token struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
	RevokedAt *string  `json:"revoked_at,omitempty"`
}

// CreatedToken is returned by CreateToken and carries the plaintext token,
// which the server only returns once.
type CreatedToken struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Token     string   `json:"token"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
}

// Client wraps an SDK client configured for a single project-scoped token.
type Client struct {
	sdk *sdk.Client
}

// Option configures a Client.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient injects a custom HTTP client, used by tests to point the SDK
// at an httptest server.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(o *clientOptions) { o.httpClient = httpClient }
}

// NewProjectClient builds a client that authenticates with a project-scoped
// emt_* token using the SDK's apitoken auth mode.
func NewProjectClient(serverURL, projectToken string) (*Client, error) {
	return newClient(serverURL, projectToken, nil)
}

// NewProjectClientWithHTTPClient is NewProjectClient with a caller-supplied
// *http.Client, used by tests to point the SDK at an httptest server.
func NewProjectClientWithHTTPClient(serverURL, projectToken string, httpClient *http.Client) (*Client, error) {
	return newClient(serverURL, projectToken, httpClient)
}

// NewBearerClient builds a client authenticated with an OAuth bearer access
// token (also SDK apitoken mode; the provider sends Authorization: Bearer).
// Use this for calls made on behalf of a signed-in account.
func NewBearerClient(serverURL, bearerToken string, opts ...Option) (*Client, error) {
	o := clientOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return newClient(serverURL, bearerToken, o.httpClient)
}

func newClient(serverURL, token string, httpClient *http.Client) (*Client, error) {
	client, err := sdk.New(sdk.Config{
		ServerURL:  serverURL,
		Auth:       sdk.AuthConfig{Mode: "apitoken", APIKey: token},
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("memoryapi: new client: %w", err)
	}
	return &Client{sdk: client}, nil
}

// ListProjects returns the projects visible to the configured token.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	projects, err := c.sdk.Projects.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("memoryapi: list projects: %w", err)
	}
	out := make([]Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, Project{ID: p.ID, Name: p.Name, OrgID: p.OrgID})
	}
	return out, nil
}

// CreateToken creates a project API token with the given name and scopes.
func (c *Client) CreateToken(ctx context.Context, projectID, name string, scopes []string) (CreatedToken, error) {
	t, err := c.sdk.APITokens.Create(ctx, projectID, &apitokens.CreateTokenRequest{
		Name:   name,
		Scopes: scopes,
	})
	if err != nil {
		return CreatedToken{}, fmt.Errorf("memoryapi: create token: %w", err)
	}
	return CreatedToken{
		ID:        t.ID,
		Name:      t.Name,
		Token:     t.Token,
		Prefix:    t.Prefix,
		Scopes:    t.Scopes,
		CreatedAt: t.CreatedAt,
	}, nil
}

// ListTokens returns the API tokens registered for a project.
func (c *Client) ListTokens(ctx context.Context, projectID string) ([]Token, error) {
	resp, err := c.sdk.APITokens.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("memoryapi: list tokens: %w", err)
	}
	out := make([]Token, 0, len(resp.Tokens))
	for _, t := range resp.Tokens {
		out = append(out, Token{
			ID:        t.ID,
			Name:      t.Name,
			Prefix:    t.Prefix,
			Scopes:    t.Scopes,
			CreatedAt: t.CreatedAt,
			RevokedAt: t.RevokedAt,
		})
	}
	return out, nil
}

// RevokeToken revokes a project API token by ID.
func (c *Client) RevokeToken(ctx context.Context, projectID, tokenID string) error {
	if err := c.sdk.APITokens.Revoke(ctx, projectID, tokenID); err != nil {
		return fmt.Errorf("memoryapi: revoke token: %w", err)
	}
	return nil
}
