// Package notifications provides the Notifications service client for the Emergent API SDK.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Notifications API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// NewClient creates a new notifications client.
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
	c.orgID = orgID
	c.projectID = projectID
}

// Notification represents a notification entity.
type Notification struct {
	ID                  string          `json:"id"`
	ProjectID           *string         `json:"projectId,omitempty"`
	UserID              string          `json:"userId"`
	Title               string          `json:"title"`
	Message             string          `json:"message"`
	Type                *string         `json:"type,omitempty"`
	Severity            string          `json:"severity"`
	RelatedResourceType *string         `json:"relatedResourceType,omitempty"`
	RelatedResourceID   *string         `json:"relatedResourceId,omitempty"`
	Read                bool            `json:"read"`
	Dismissed           bool            `json:"dismissed"`
	DismissedAt         *time.Time      `json:"dismissedAt,omitempty"`
	Actions             json.RawMessage `json:"actions"`
	ExpiresAt           *time.Time      `json:"expiresAt,omitempty"`
	ReadAt              *time.Time      `json:"readAt,omitempty"`
	Importance          string          `json:"importance"`
	ClearedAt           *time.Time      `json:"clearedAt,omitempty"`
	SnoozedUntil        *time.Time      `json:"snoozedUntil,omitempty"`
	Category            *string         `json:"category,omitempty"`
	SourceType          *string         `json:"sourceType,omitempty"`
	SourceID            *string         `json:"sourceId,omitempty"`
	ActionURL           *string         `json:"actionUrl,omitempty"`
	ActionLabel         *string         `json:"actionLabel,omitempty"`
	GroupKey            *string         `json:"groupKey,omitempty"`
	Details             json.RawMessage `json:"details,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	ActionStatus        *string         `json:"actionStatus,omitempty"`
	ActionStatusAt      *time.Time      `json:"actionStatusAt,omitempty"`
	ActionStatusBy      *string         `json:"actionStatusBy,omitempty"`
	TaskID              *string         `json:"taskId,omitempty"`
}

// NotificationStats represents aggregated notification statistics.
type NotificationStats struct {
	Unread    int64 `json:"unread"`
	Dismissed int64 `json:"dismissed"`
	Total     int64 `json:"total"`
}

// NotificationCounts represents counts by tab.
type NotificationCounts struct {
	All       int64 `json:"all"`
	Important int64 `json:"important"`
	Other     int64 `json:"other"`
	Snoozed   int64 `json:"snoozed"`
	Cleared   int64 `json:"cleared"`
}

// NotificationCountsResponse wraps the counts response.
type NotificationCountsResponse struct {
	Data NotificationCounts `json:"data"`
}

// NotificationListResponse wraps the notification list response.
type NotificationListResponse struct {
	Data []Notification `json:"data"`
}

// MarkAllReadResponse represents the response from marking all notifications as read.
type MarkAllReadResponse struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// ListOptions holds options for listing notifications.
type ListOptions struct {
	Tab        string // "all", "important", "other", "snoozed", "cleared"
	Category   string // Filter by notification category
	UnreadOnly bool   // Show only unread notifications
	Search     string // Search notifications by title or message
}

// setHeaders adds auth and context headers to the request.
func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}
	return nil
}

// GetStats returns aggregated notification statistics.
func (c *Client) GetStats(ctx context.Context) (*NotificationStats, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/notifications/stats", nil)
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

	var stats NotificationStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &stats, nil
}

// GetCounts returns notification counts grouped by tab.
func (c *Client) GetCounts(ctx context.Context) (*NotificationCountsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/notifications/counts", nil)
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

	var result NotificationCountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// List returns filtered list of notifications.
func (c *Client) List(ctx context.Context, opts *ListOptions) (*NotificationListResponse, error) {
	u, err := url.Parse(c.base + "/api/notifications")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if opts != nil {
		if opts.Tab != "" {
			q.Set("tab", opts.Tab)
		}
		if opts.Category != "" {
			q.Set("category", opts.Category)
		}
		if opts.UnreadOnly {
			q.Set("unread_only", "true")
		}
		if opts.Search != "" {
			q.Set("search", opts.Search)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
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

	var result NotificationListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// MarkRead marks a specific notification as read.
func (c *Client) MarkRead(ctx context.Context, notificationID string) error {
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/notifications/"+notificationID+"/read", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// Dismiss dismisses a notification.
func (c *Client) Dismiss(ctx context.Context, notificationID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/notifications/"+notificationID+"/dismiss", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// MarkAllRead marks all unread notifications as read. Returns the count of marked notifications.
func (c *Client) MarkAllRead(ctx context.Context) (*MarkAllReadResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/notifications/mark-all-read", nil)
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

	var result MarkAllReadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
