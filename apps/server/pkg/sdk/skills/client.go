// Package skills provides the Skills service client for the Emergent API SDK.
// Skills are reusable Markdown workflow instructions that agents can load on demand.
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Skills API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new skills client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// --- Types ---

// Skill is the full skill representation returned from the API.
type Skill struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	ProjectID   *string        `json:"projectId,omitempty"`
	OrgID       *string        `json:"orgId,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// ListSkillsResponse is the response from list endpoints.
type ListSkillsResponse struct {
	Skills []*Skill `json:"skills"`
}

// CreateSkillRequest is the request to create a new skill.
type CreateSkillRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// UpdateSkillRequest is the request to partially update a skill.
// Only non-nil fields are sent.
type UpdateSkillRequest struct {
	Description *string        `json:"description,omitempty"`
	Content     *string        `json:"content,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// --- Internal helpers ---

func (c *Client) prepareRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
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
	return req, nil
}

func (c *Client) doJSON(req *http.Request, result any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, reqURL string, result any) error {
	req, err := c.prepareRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, result)
}

func (c *Client) postJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := c.prepareRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, result)
}

func (c *Client) patchJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := c.prepareRequest(ctx, http.MethodPatch, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, result)
}

func (c *Client) doDelete(ctx context.Context, reqURL string) error {
	req, err := c.prepareRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

func (c *Client) skillsPath() string {
	return c.base + "/api/skills"
}

func (c *Client) projectSkillsPath(projectID string) string {
	if projectID == "" {
		c.mu.RLock()
		projectID = c.projectID
		c.mu.RUnlock()
	}
	return c.base + "/api/projects/" + url.PathEscape(projectID) + "/skills"
}

// --- Global skill methods ---

// List returns all global skills (project-independent).
// Pass an empty projectID to list global skills.
// Pass a non-empty projectID to list merged global + project-scoped skills.
func (c *Client) List(ctx context.Context, projectID string) ([]*Skill, error) {
	var result ListSkillsResponse
	var reqURL string
	if projectID == "" {
		reqURL = c.skillsPath()
	} else {
		reqURL = c.projectSkillsPath(projectID)
	}
	if err := c.getJSON(ctx, reqURL, &result); err != nil {
		return nil, err
	}
	return result.Skills, nil
}

// Get retrieves a skill by ID.
func (c *Client) Get(ctx context.Context, id string) (*Skill, error) {
	var result Skill
	if err := c.getJSON(ctx, c.skillsPath()+"/"+url.PathEscape(id), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Create creates a new skill. Pass an empty projectID to create a global skill.
func (c *Client) Create(ctx context.Context, projectID string, req *CreateSkillRequest) (*Skill, error) {
	var result Skill
	var reqURL string
	if projectID == "" {
		reqURL = c.skillsPath()
	} else {
		reqURL = c.projectSkillsPath(projectID)
	}
	if err := c.postJSON(ctx, reqURL, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Update partially updates a skill by ID.
func (c *Client) Update(ctx context.Context, id string, req *UpdateSkillRequest) (*Skill, error) {
	var result Skill
	if err := c.patchJSON(ctx, c.skillsPath()+"/"+url.PathEscape(id), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete deletes a skill by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.doDelete(ctx, c.skillsPath()+"/"+url.PathEscape(id))
}

// --- Org-scoped skill methods ---

func (c *Client) orgSkillsPath(orgID string) string {
	return c.base + "/api/orgs/" + url.PathEscape(orgID) + "/skills"
}

// ListOrgSkills returns all org-scoped skills for the given organization.
func (c *Client) ListOrgSkills(ctx context.Context, orgID string) ([]*Skill, error) {
	var result ListSkillsResponse
	if err := c.getJSON(ctx, c.orgSkillsPath(orgID), &result); err != nil {
		return nil, err
	}
	return result.Skills, nil
}

// CreateOrgSkill creates a new skill scoped to the given organization.
func (c *Client) CreateOrgSkill(ctx context.Context, orgID string, req *CreateSkillRequest) (*Skill, error) {
	var result Skill
	if err := c.postJSON(ctx, c.orgSkillsPath(orgID), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateOrgSkill partially updates an org-scoped skill by ID.
func (c *Client) UpdateOrgSkill(ctx context.Context, orgID string, id string, req *UpdateSkillRequest) (*Skill, error) {
	var result Skill
	if err := c.patchJSON(ctx, c.orgSkillsPath(orgID)+"/"+url.PathEscape(id), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteOrgSkill deletes an org-scoped skill by ID.
func (c *Client) DeleteOrgSkill(ctx context.Context, orgID string, id string) error {
	return c.doDelete(ctx, c.orgSkillsPath(orgID)+"/"+url.PathEscape(id))
}
