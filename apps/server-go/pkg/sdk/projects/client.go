// Package projects provides the Projects service client for the Emergent API SDK.
package projects

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

// Client provides access to the Projects API.
type Client struct {
	http *http.Client
	base string
	auth auth.Provider
}

// NewClient creates a new projects client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// TemplatePack represents an installed template pack for a project
type TemplatePack struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	ObjectTypes       []string `json:"objectTypes"`
	RelationshipTypes []string `json:"relationshipTypes"`
}

// ProjectStats represents aggregated statistics for a project
type ProjectStats struct {
	DocumentCount     int            `json:"documentCount"`
	ObjectCount       int            `json:"objectCount"`
	RelationshipCount int            `json:"relationshipCount"`
	TotalJobs         int            `json:"totalJobs"`
	RunningJobs       int            `json:"runningJobs"`
	QueuedJobs        int            `json:"queuedJobs"`
	TemplatePacks     []TemplatePack `json:"templatePacks"`
}

// Project represents a project entity
type Project struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	OrgID              string                 `json:"orgId"`
	KBPurpose          *string                `json:"kb_purpose,omitempty"`
	ChatPromptTemplate *string                `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects *bool                  `json:"auto_extract_objects,omitempty"`
	AutoExtractConfig  map[string]interface{} `json:"auto_extract_config,omitempty"`
	Stats              *ProjectStats          `json:"stats,omitempty"`
}

// ProjectMember represents a project member
type ProjectMember struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName *string `json:"displayName,omitempty"`
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	Role        string  `json:"role"`
	JoinedAt    string  `json:"joinedAt"`
}

// CreateProjectRequest represents a project creation request
type CreateProjectRequest struct {
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
}

// UpdateProjectRequest represents a project update request
type UpdateProjectRequest struct {
	Name               *string                `json:"name,omitempty"`
	KBPurpose          *string                `json:"kb_purpose,omitempty"`
	ChatPromptTemplate *string                `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects *bool                  `json:"auto_extract_objects,omitempty"`
	AutoExtractConfig  map[string]interface{} `json:"auto_extract_config,omitempty"`
}

// ListOptions holds options for listing projects.
type ListOptions struct {
	Limit        int
	OrgID        string
	IncludeStats bool
}

// List returns all projects the authenticated user is a member of.
func (c *Client) List(ctx context.Context, opts *ListOptions) ([]Project, error) {
	u, err := url.Parse(c.base + "/api/projects")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.OrgID != "" {
			q.Set("orgId", opts.OrgID)
		}
		if opts.IncludeStats {
			q.Set("include_stats", "true")
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
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
	var projects []Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return projects, nil
}

// GetOptions holds options for getting a project.
type GetOptions struct {
	IncludeStats bool
}

// Get retrieves a single project by ID.
func (c *Client) Get(ctx context.Context, id string, opts *GetOptions) (*Project, error) {
	u, err := url.Parse(c.base + "/api/projects/" + url.PathEscape(id))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil && opts.IncludeStats {
		q.Set("include_stats", "true")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
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
	var project Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &project, nil
}

// Create creates a new project.
func (c *Client) Create(ctx context.Context, req *CreateProjectRequest) (*Project, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects", bytes.NewReader(body))
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
	var project Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &project, nil
}

// Update updates an existing project.
func (c *Client) Update(ctx context.Context, id string, req *UpdateProjectRequest) (*Project, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/projects/"+url.PathEscape(id), bytes.NewReader(body))
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
	var project Project
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &project, nil
}

// Delete deletes a project by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/projects/"+url.PathEscape(id), nil)
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

// ListMembers returns all members of a project.
func (c *Client) ListMembers(ctx context.Context, projectID string) ([]ProjectMember, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(projectID)+"/members", nil)
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
	var members []ProjectMember
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return members, nil
}

// RemoveMember removes a user from a project.
func (c *Client) RemoveMember(ctx context.Context, projectID, userID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/projects/"+url.PathEscape(projectID)+"/members/"+url.PathEscape(userID), nil)
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
