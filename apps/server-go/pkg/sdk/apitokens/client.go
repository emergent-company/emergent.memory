// Package apitokens provides the API Tokens service client for the Emergent API SDK.
package apitokens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the API Tokens API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new API tokens client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// APIToken represents an API token (includes full token value if retrieved by ID)
type APIToken struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Token     string   `json:"token,omitempty"` // Full token value - available when retrieved by ID
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"createdAt"`
	RevokedAt *string  `json:"revokedAt,omitempty"`
}

// CreateTokenResponse represents the response when creating a token (includes full token value)
type CreateTokenResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Token     string   `json:"token"` // Full token value - only returned at creation
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"createdAt"`
}

// CreateTokenRequest represents an API token creation request
type CreateTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// ListResponse represents the response from listing tokens
type ListResponse struct {
	Tokens []APIToken `json:"tokens"`
}

// Create creates a new API token for a project.
func (c *Client) Create(ctx context.Context, projectID string, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects/"+url.PathEscape(projectID)+"/tokens", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Add authentication
	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Execute request
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var token CreateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &token, nil
}

// List returns all API tokens for a project.
func (c *Client) List(ctx context.Context, projectID string) (*ListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(projectID)+"/tokens", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Execute request
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var result ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Get retrieves a single API token by ID.
func (c *Client) Get(ctx context.Context, projectID, tokenID string) (*APIToken, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(projectID)+"/tokens/"+url.PathEscape(tokenID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Execute request
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse response
	var token APIToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &token, nil
}

// Revoke revokes an API token, making it permanently unusable.
func (c *Client) Revoke(ctx context.Context, projectID, tokenID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/projects/"+url.PathEscape(projectID)+"/tokens/"+url.PathEscape(tokenID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Execute request
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	// Drain response body
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}
