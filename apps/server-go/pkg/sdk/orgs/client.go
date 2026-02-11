// Package orgs provides the Organizations service client for the Emergent API SDK.
package orgs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/emergent/emergent-core/pkg/sdk/auth"
	sdkerrors "github.com/emergent/emergent-core/pkg/sdk/errors"
)

// Client provides access to the Organizations API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// NewClient creates a new organizations client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.orgID = orgID
	c.projectID = projectID
}

// Organization represents an organization entity
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateOrganizationRequest represents an organization creation request
type CreateOrganizationRequest struct {
	Name string `json:"name"`
}

// List returns all organizations the authenticated user is a member of.
func (c *Client) List(ctx context.Context) ([]Organization, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/orgs", nil)
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
	var orgs []Organization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return orgs, nil
}

// Get retrieves a single organization by ID.
func (c *Client) Get(ctx context.Context, id string) (*Organization, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/orgs/"+id, nil)
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
	var org Organization
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &org, nil
}

// Create creates a new organization.
func (c *Client) Create(ctx context.Context, req *CreateOrganizationRequest) (*Organization, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/orgs", bytes.NewReader(body))
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
	var org Organization
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &org, nil
}

// Delete deletes an organization by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/orgs/"+id, nil)
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
