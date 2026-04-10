// Package acp provides a Go client for the Agent Communication Protocol (ACP) v1 API.
//
// The ACP API is mounted at /acp/v1/ and uses Bearer token authentication
// (the same emt_* project API tokens as the rest of the Memory API).
//
// Example usage:
//
//	client := acp.NewClient("http://localhost:5300", "emt_abc123...")
//	agents, err := client.ListAgents(ctx)
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the ACP v1 API.
type Client struct {
	http  *http.Client
	base  string
	token string
}

// NewClient creates a new ACP client.
// base is the server URL (e.g. "http://localhost:5300").
// token is the emt_* project API token.
func NewClient(base, token string) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		base:  strings.TrimRight(base, "/"),
		token: token,
	}
}

// NewClientWithHTTP creates a new ACP client with a custom HTTP client.
func NewClientWithHTTP(base, token string, httpClient *http.Client) *Client {
	return &Client{
		http:  httpClient,
		base:  strings.TrimRight(base, "/"),
		token: token,
	}
}

// --- Types ---

// AgentManifest is the ACP agent card returned by discovery endpoints.
type AgentManifest struct {
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Provider           *Provider      `json:"provider,omitempty"`
	Version            string         `json:"version"`
	Capabilities       *Capabilities  `json:"capabilities,omitempty"`
	DefaultInputModes  []string       `json:"default_input_modes"`
	DefaultOutputModes []string       `json:"default_output_modes"`
	Status             *StatusMetrics `json:"status,omitempty"`
	Tags               []string       `json:"tags,omitempty"`
	Domains            []string       `json:"domains,omitempty"`
	Documentation      string         `json:"documentation,omitempty"`
	Framework          string         `json:"framework,omitempty"`
}

// Provider identifies the agent provider.
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// Capabilities describes what the agent supports.
type Capabilities struct {
	Streaming      bool `json:"streaming"`
	HumanInTheLoop bool `json:"human_in_the_loop"`
	SessionSupport bool `json:"session_support"`
}

// StatusMetrics holds live computed metrics for an agent.
type StatusMetrics struct {
	AvgRunTokens      *float64 `json:"avg_run_tokens,omitempty"`
	AvgRunTimeSeconds *float64 `json:"avg_run_time_seconds,omitempty"`
	SuccessRate       *float64 `json:"success_rate,omitempty"`
}

// Message is an ACP protocol message with role and parts.
type Message struct {
	Role  string        `json:"role"`
	Parts []MessagePart `json:"parts"`
}

// MessagePart is a single part of an ACP message.
type MessagePart struct {
	ContentType string              `json:"content_type"`
	Content     string              `json:"content"`
	Metadata    *TrajectoryMetadata `json:"metadata,omitempty"`
}

// TrajectoryMetadata describes a tool call trajectory attached to a message part.
type TrajectoryMetadata struct {
	Type       string `json:"type"`
	ToolName   string `json:"tool_name"`
	ToolInput  string `json:"tool_input"`
	ToolOutput string `json:"tool_output"`
}

// RunObject is the ACP run representation returned by run endpoints.
type RunObject struct {
	ID           string         `json:"id"`
	AgentName    string         `json:"agent_name"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    *time.Time     `json:"updated_at,omitempty"`
	Output       []Message      `json:"output,omitempty"`
	AwaitRequest *AwaitRequest  `json:"await_request,omitempty"`
	Error        *RunError      `json:"error,omitempty"`
	SessionID    *string        `json:"session_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// RunError describes an error that occurred during a run.
type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AwaitRequest represents a human-in-the-loop question posed by the agent.
type AwaitRequest struct {
	QuestionID string           `json:"question_id"`
	Question   string           `json:"question"`
	Options    []QuestionOption `json:"options,omitempty"`
}

// QuestionOption represents a structured choice for an agent question.
type QuestionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// SessionObject is the ACP session representation.
type SessionObject struct {
	ID        string       `json:"id"`
	AgentName *string      `json:"agent_name,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	History   []RunSummary `json:"history"`
}

// RunSummary is a lightweight run reference inside a session.
type RunSummary struct {
	ID        string    `json:"id"`
	AgentName string    `json:"agent_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SSEEvent represents a server-sent event from a streaming run.
type SSEEvent struct {
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
}

// --- Request Types ---

// CreateRunRequest is the request body for creating a run.
type CreateRunRequest struct {
	Message   []MessagePart `json:"message"`
	Mode      string        `json:"mode,omitempty"` // sync (default), async, stream
	SessionID *string       `json:"session_id,omitempty"`
}

// ResumeRunRequest is the request body for resuming a paused run.
type ResumeRunRequest struct {
	Message []MessagePart `json:"message"`
	Mode    string        `json:"mode,omitempty"` // sync (default), async, stream
}

// CreateSessionRequest is the request body for creating a session.
type CreateSessionRequest struct {
	AgentName *string `json:"agent_name,omitempty"`
}

// SSEStream provides a streaming reader for SSE events from a run.
// Call Next() to receive events; call Close() when done.
type SSEStream struct {
	resp    *http.Response
	scanner *bufio.Scanner
}

// Next reads the next SSE event from the stream.
// Returns io.EOF when the stream ends.
func (s *SSEStream) Next() (*SSEEvent, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// SSE data lines start with "data: "
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil, io.EOF
			}

			var event SSEEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return nil, fmt.Errorf("failed to decode SSE event: %w", err)
			}
			return &event, nil
		}

		// Skip empty lines and comments (: prefixed lines)
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream read error: %w", err)
	}

	return nil, io.EOF
}

// Close closes the SSE stream and releases resources.
func (s *SSEStream) Close() error {
	return s.resp.Body.Close()
}

// --- Internal helpers ---

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return nil
}

// doJSONWithStatus is like doJSON but also returns the HTTP status code.
func (c *Client) doJSONWithStatus(ctx context.Context, method, path string, body any, result any) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, sdkerrors.ParseErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return resp.StatusCode, fmt.Errorf("failed to decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	return resp.StatusCode, nil
}

// --- API Methods ---

// Ping checks that the ACP endpoint is reachable.
// GET /acp/v1/ping
func (c *Client) Ping(ctx context.Context) error {
	return c.doJSON(ctx, "GET", "/acp/v1/ping", nil, nil)
}

// ListAgents returns all externally-visible ACP agents.
// GET /acp/v1/agents
func (c *Client) ListAgents(ctx context.Context) ([]AgentManifest, error) {
	var result []AgentManifest
	if err := c.doJSON(ctx, "GET", "/acp/v1/agents", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetAgent returns a single ACP agent manifest by slug name.
// GET /acp/v1/agents/:name
func (c *Client) GetAgent(ctx context.Context, name string) (*AgentManifest, error) {
	var result AgentManifest
	if err := c.doJSON(ctx, "GET", "/acp/v1/agents/"+url.PathEscape(name), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateRun creates a new run for an agent (sync or async mode).
// For streaming, use CreateRunStream instead.
// POST /acp/v1/agents/:name/runs
func (c *Client) CreateRun(ctx context.Context, agentName string, req CreateRunRequest) (*RunObject, error) {
	if req.Mode == "stream" {
		return nil, fmt.Errorf("use CreateRunStream for streaming mode")
	}
	var result RunObject
	if err := c.doJSON(ctx, "POST", "/acp/v1/agents/"+url.PathEscape(agentName)+"/runs", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateRunStream creates a streaming run and returns an SSEStream for reading events.
// The caller must call stream.Close() when done.
// POST /acp/v1/agents/:name/runs with mode=stream
func (c *Client) CreateRunStream(ctx context.Context, agentName string, req CreateRunRequest) (*SSEStream, error) {
	req.Mode = "stream"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/acp/v1/agents/"+url.PathEscape(agentName)+"/runs",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use a client without timeout for streaming
	streamClient := &http.Client{
		Transport: c.http.Transport,
		// No timeout — streaming connections are long-lived
	}

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	return &SSEStream{
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
	}, nil
}

// GetRun retrieves a specific run by ID.
// GET /acp/v1/agents/:name/runs/:runId
func (c *Client) GetRun(ctx context.Context, agentName, runID string) (*RunObject, error) {
	var result RunObject
	path := fmt.Sprintf("/acp/v1/agents/%s/runs/%s", url.PathEscape(agentName), url.PathEscape(runID))
	if err := c.doJSON(ctx, "GET", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelRun cancels a running or queued run. Returns the updated run object.
// DELETE /acp/v1/agents/:name/runs/:runId
func (c *Client) CancelRun(ctx context.Context, agentName, runID string) (*RunObject, error) {
	var result RunObject
	path := fmt.Sprintf("/acp/v1/agents/%s/runs/%s", url.PathEscape(agentName), url.PathEscape(runID))
	if err := c.doJSON(ctx, "DELETE", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ResumeRun resumes a paused run with user input (sync or async mode).
// For streaming, use ResumeRunStream instead.
// POST /acp/v1/agents/:name/runs/:runId/resume
func (c *Client) ResumeRun(ctx context.Context, agentName, runID string, req ResumeRunRequest) (*RunObject, error) {
	if req.Mode == "stream" {
		return nil, fmt.Errorf("use ResumeRunStream for streaming mode")
	}
	var result RunObject
	path := fmt.Sprintf("/acp/v1/agents/%s/runs/%s/resume", url.PathEscape(agentName), url.PathEscape(runID))
	if err := c.doJSON(ctx, "POST", path, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ResumeRunStream resumes a paused run with streaming output.
// The caller must call stream.Close() when done.
// POST /acp/v1/agents/:name/runs/:runId/resume with mode=stream
func (c *Client) ResumeRunStream(ctx context.Context, agentName, runID string, req ResumeRunRequest) (*SSEStream, error) {
	req.Mode = "stream"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	path := fmt.Sprintf("/acp/v1/agents/%s/runs/%s/resume", url.PathEscape(agentName), url.PathEscape(runID))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	streamClient := &http.Client{
		Transport: c.http.Transport,
	}

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	return &SSEStream{
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
	}, nil
}

// GetRunEvents returns persisted events for a run.
// GET /acp/v1/agents/:name/runs/:runId/events
func (c *Client) GetRunEvents(ctx context.Context, agentName, runID string) ([]SSEEvent, error) {
	var result []SSEEvent
	path := fmt.Sprintf("/acp/v1/agents/%s/runs/%s/events", url.PathEscape(agentName), url.PathEscape(runID))
	if err := c.doJSON(ctx, "GET", path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateSession creates a new ACP session.
// POST /acp/v1/sessions
func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionObject, error) {
	var result SessionObject
	if err := c.doJSON(ctx, "POST", "/acp/v1/sessions", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSession retrieves a session by ID, including run history.
// GET /acp/v1/sessions/:sessionId
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionObject, error) {
	var result SessionObject
	if err := c.doJSON(ctx, "GET", "/acp/v1/sessions/"+url.PathEscape(sessionID), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
