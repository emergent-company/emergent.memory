package clickup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent/pkg/logger"
)

const (
	baseURLV2 = "https://api.clickup.com/api/v2"
	baseURLV3 = "https://api.clickup.com/api/v3"

	// ClickUp rate limit: 100 requests per minute per workspace
	defaultMaxRequests = 100
	defaultWindowMs    = 60000 // 1 minute
)

// Client is a stateless ClickUp API client.
// Each method accepts the API token, making it suitable for the DataSourceProvider pattern.
type Client struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	log         *slog.Logger
}

// NewClient creates a new ClickUp API client
func NewClient(log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: NewRateLimiter(defaultMaxRequests, defaultWindowMs),
		log:         log.With(logger.Scope("clickup-client")),
	}
}

// ----------------------------------------------------------------------------
// Rate Limiter
// ----------------------------------------------------------------------------

// RateLimiter implements a sliding window rate limiter
type RateLimiter struct {
	mu          sync.Mutex
	timestamps  []int64
	maxRequests int
	windowMs    int64
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxRequests int, windowMs int) *RateLimiter {
	return &RateLimiter{
		timestamps:  make([]int64, 0, maxRequests),
		maxRequests: maxRequests,
		windowMs:    int64(windowMs),
	}
}

// WaitForSlot blocks until a request slot is available
func (r *RateLimiter) WaitForSlot(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		r.mu.Lock()
		now := time.Now().UnixMilli()

		// Remove timestamps outside the window
		var newTimestamps []int64
		for _, ts := range r.timestamps {
			if now-ts < r.windowMs {
				newTimestamps = append(newTimestamps, ts)
			}
		}
		r.timestamps = newTimestamps

		// Check if we have capacity
		if len(r.timestamps) < r.maxRequests {
			r.timestamps = append(r.timestamps, now)
			r.mu.Unlock()
			return nil
		}

		// Calculate wait time until oldest request expires
		oldestTs := r.timestamps[0]
		waitMs := r.windowMs - (now - oldestTs) + 100 // +100ms buffer
		r.mu.Unlock()

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(waitMs) * time.Millisecond):
			continue
		}
	}
}

// Reset clears all timestamps (useful for testing)
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timestamps = r.timestamps[:0]
}

// ----------------------------------------------------------------------------
// HTTP Helpers
// ----------------------------------------------------------------------------

// request makes an authenticated API request
func (c *Client) request(ctx context.Context, apiToken, method, urlStr string) ([]byte, error) {
	// Wait for rate limit slot
	if err := c.rateLimiter.WaitForSlot(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	return body, nil
}

// ----------------------------------------------------------------------------
// ClickUp API v2 Methods
// ----------------------------------------------------------------------------

// GetWorkspaces retrieves all workspaces (teams) the user has access to
func (c *Client) GetWorkspaces(ctx context.Context, apiToken string) (*WorkspacesResponse, error) {
	urlStr := fmt.Sprintf("%s/team", baseURLV2)

	body, err := c.request(ctx, apiToken, http.MethodGet, urlStr)
	if err != nil {
		c.log.Error("failed to get workspaces", logger.Error(err))
		return nil, fmt.Errorf("get workspaces: %w", err)
	}

	var response WorkspacesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse workspaces response: %w", err)
	}

	return &response, nil
}

// GetSpaces retrieves all spaces in a workspace
func (c *Client) GetSpaces(ctx context.Context, apiToken, workspaceID string, archived bool) (*SpacesResponse, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/team/%s/space", baseURLV2, workspaceID))
	if archived {
		q := u.Query()
		q.Set("archived", "true")
		u.RawQuery = q.Encode()
	}

	body, err := c.request(ctx, apiToken, http.MethodGet, u.String())
	if err != nil {
		c.log.Error("failed to get spaces", logger.Error(err), slog.String("workspace_id", workspaceID))
		return nil, fmt.Errorf("get spaces: %w", err)
	}

	var response SpacesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse spaces response: %w", err)
	}

	return &response, nil
}

// ----------------------------------------------------------------------------
// ClickUp API v3 Methods (Docs)
// ----------------------------------------------------------------------------

// GetDocs retrieves all docs in a workspace, optionally filtered by parent
func (c *Client) GetDocs(ctx context.Context, apiToken, workspaceID string, cursor, parentID, parentType string) (*DocsResponse, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/workspaces/%s/docs", baseURLV3, workspaceID))
	q := u.Query()
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if parentID != "" {
		q.Set("parent", parentID)
	}
	if parentType != "" {
		q.Set("parent_type", parentType)
	}
	u.RawQuery = q.Encode()

	body, err := c.request(ctx, apiToken, http.MethodGet, u.String())
	if err != nil {
		c.log.Error("failed to get docs", logger.Error(err), slog.String("workspace_id", workspaceID))
		return nil, fmt.Errorf("get docs: %w", err)
	}

	var response DocsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse docs response: %w", err)
	}

	return &response, nil
}

// GetDoc retrieves a specific doc by ID
func (c *Client) GetDoc(ctx context.Context, apiToken, workspaceID, docID string) (*Doc, error) {
	urlStr := fmt.Sprintf("%s/workspaces/%s/docs/%s", baseURLV3, workspaceID, docID)

	body, err := c.request(ctx, apiToken, http.MethodGet, urlStr)
	if err != nil {
		c.log.Error("failed to get doc", logger.Error(err), slog.String("doc_id", docID))
		return nil, fmt.Errorf("get doc: %w", err)
	}

	var doc Doc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse doc response: %w", err)
	}

	return &doc, nil
}

// GetDocPages retrieves all pages for a doc
func (c *Client) GetDocPages(ctx context.Context, apiToken, workspaceID, docID string) ([]Page, error) {
	urlStr := fmt.Sprintf("%s/workspaces/%s/docs/%s/pages", baseURLV3, workspaceID, docID)

	body, err := c.request(ctx, apiToken, http.MethodGet, urlStr)
	if err != nil {
		c.log.Error("failed to get doc pages", logger.Error(err), slog.String("doc_id", docID))
		return nil, fmt.Errorf("get doc pages: %w", err)
	}

	var pages []Page
	if err := json.Unmarshal(body, &pages); err != nil {
		return nil, fmt.Errorf("parse pages response: %w", err)
	}

	return pages, nil
}

// GetPage retrieves a specific page from a doc
func (c *Client) GetPage(ctx context.Context, apiToken, workspaceID, docID, pageID string) (*Page, error) {
	urlStr := fmt.Sprintf("%s/workspaces/%s/docs/%s/pages/%s", baseURLV3, workspaceID, docID, pageID)

	body, err := c.request(ctx, apiToken, http.MethodGet, urlStr)
	if err != nil {
		c.log.Error("failed to get page", logger.Error(err), slog.String("page_id", pageID))
		return nil, fmt.Errorf("get page: %w", err)
	}

	var page Page
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse page response: %w", err)
	}

	return &page, nil
}

// ResetRateLimiter resets the rate limiter (for testing)
func (c *Client) ResetRateLimiter() {
	c.rateLimiter.Reset()
}
