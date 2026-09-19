// Package a2a provides a Go client for the A2A Protocol v1.0 HTTP+JSON surface
// exposed by the Memory server.
//
// The A2A surface is mounted at the server root (outside /api/):
//
//	GET  /.well-known/agent-card.json   (unauthenticated global card)
//	GET  /extendedAgentCard             (Bearer emt_* project token, agents:read)
//	POST /message:send                  (agents:write)
//	POST /message:stream                (SSE, agents:write)
//	GET  /tasks/{id}                    (agents:read)
//	GET  /tasks                         (agents:read)
//	POST /tasks/{id}:cancel             (agents:write)
//
// Content type is application/a2a+json; errors use the google.rpc.Status
// envelope. Auth is the same emt_* project API tokens as the rest of the API.
//
// Example usage:
//
//	client := a2a.NewClient("http://localhost:5300", "emt_abc123...")
//	card, err := client.ExtendedAgentCard(ctx)
package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A2AContentType is the A2A HTTP+JSON media type.
const A2AContentType = "application/a2a+json"

// A2AProtocolVersion is the only A2A protocol version this client speaks.
const A2AProtocolVersion = "1.0"

// A2AVersionHeader is the header carrying the negotiated protocol version.
const A2AVersionHeader = "A2A-Version"

// A2AErrorDomain is the canonical A2A error detail domain.
const A2AErrorDomain = "a2a-protocol.org"

// SkillIDMetadataKey is the message metadata key under which a first-party
// client encodes the target AgentSkill id. A2A's SendMessageRequest has no
// skill selector, so the CLI/SDK address a skill via this metadata extension;
// unknown metadata keys are ignored by conformant third-party clients.
const SkillIDMetadataKey = "skillId"

// Client provides access to the A2A v1.0 API.
type Client struct {
	http  *http.Client
	base  string
	token string
}

// NewClient creates a new A2A client.
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

// NewClientWithHTTP creates a new A2A client with a custom HTTP client.
func NewClientWithHTTP(base, token string, httpClient *http.Client) *Client {
	return &Client{
		http:  httpClient,
		base:  strings.TrimRight(base, "/"),
		token: token,
	}
}

// --- Enums ---

// TaskState is the A2A task lifecycle state.
type TaskState string

const (
	TaskStateUnspecified   TaskState = "TASK_STATE_UNSPECIFIED"
	TaskStateSubmitted     TaskState = "TASK_STATE_SUBMITTED"
	TaskStateWorking       TaskState = "TASK_STATE_WORKING"
	TaskStateCompleted     TaskState = "TASK_STATE_COMPLETED"
	TaskStateFailed        TaskState = "TASK_STATE_FAILED"
	TaskStateCanceled      TaskState = "TASK_STATE_CANCELED"
	TaskStateInputRequired TaskState = "TASK_STATE_INPUT_REQUIRED"
	TaskStateRejected      TaskState = "TASK_STATE_REJECTED"
	TaskStateAuthRequired  TaskState = "TASK_STATE_AUTH_REQUIRED"
)

// Role is the A2A message role.
type Role string

const (
	RoleUnspecified Role = "ROLE_UNSPECIFIED"
	RoleUser        Role = "ROLE_USER"
	RoleAgent       Role = "ROLE_AGENT"
)

// --- Discovery ---

// AgentCard is the A2A agent descriptor.
type AgentCard struct {
	Name                 string                    `json:"name"`
	Description          string                    `json:"description"`
	SupportedInterfaces  []AgentInterface          `json:"supportedInterfaces"`
	Provider             *AgentProvider            `json:"provider,omitempty"`
	Version              string                    `json:"version"`
	DocumentationURL     string                    `json:"documentationUrl,omitempty"`
	Capabilities         AgentCapabilities         `json:"capabilities"`
	SecuritySchemes      map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	SecurityRequirements []SecurityRequirement     `json:"securityRequirements,omitempty"`
	DefaultInputModes    []string                  `json:"defaultInputModes"`
	DefaultOutputModes   []string                  `json:"defaultOutputModes"`
	Skills               []AgentSkill              `json:"skills"`
	IconURL              string                    `json:"iconUrl,omitempty"`
	Signatures           []any                     `json:"signatures,omitempty"`
}

// AgentProvider identifies the agent provider.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentInterface describes one transport binding for the agent.
type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	Tenant          string `json:"tenant,omitempty"`
	ProtocolVersion string `json:"protocolVersion"`
}

// AgentCapabilities describes optional agent capabilities.
type AgentCapabilities struct {
	Streaming         bool             `json:"streaming"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
	ExtendedAgentCard bool             `json:"extendedAgentCard"`
}

// AgentExtension describes an agent extension.
type AgentExtension struct {
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Version     string `json:"version,omitempty"`
}

// AgentSkill describes a single agent skill.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// SecurityScheme is an A2A security scheme. Only the HTTP auth variant is
// represented (bearer).
type SecurityScheme struct {
	HTTPAuth *HTTPAuthSecurityScheme `json:"httpAuthSecurityScheme,omitempty"`
}

// HTTPAuthSecurityScheme is the HTTP authentication security scheme.
type HTTPAuthSecurityScheme struct {
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// SecurityRequirement declares the schemes required for a given operation.
type SecurityRequirement struct {
	Schemes map[string]SecuritySchemeReference `json:"schemes"`
}

// SecuritySchemeReference is a reference to a declared security scheme.
type SecuritySchemeReference struct {
	List []string `json:"list"`
}

// --- Task / Message / Artifact ---

// Task is the A2A task object.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus is the current status of a task.
type TaskStatus struct {
	State     TaskState  `json:"state"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// Message is an A2A message.
type Message struct {
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             Role           `json:"role"`
	Parts            []Part         `json:"parts"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
}

// Part is a single content part of a message. Exactly one of Text/Raw/URL/Data
// is set; there is no `kind` discriminator on the wire.
type Part struct {
	Text      *string        `json:"text,omitempty"`
	Raw       *string        `json:"raw,omitempty"` // base64-encoded bytes
	URL       *string        `json:"url,omitempty"`
	Data      any            `json:"data,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
}

// Artifact is a named collection of parts produced by a task.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
}

// TextPart builds a text part.
func TextPart(text string) Part {
	return Part{Text: strPtr(text)}
}

// --- Streaming ---

// StreamResponse is the SSE payload for message:stream. Exactly one member is
// set; there is no `kind` discriminator on the wire.
type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// TaskStatusUpdateEvent is a status transition stream event.
type TaskStatusUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent is an artifact delta stream event.
type TaskArtifactUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  Artifact       `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// --- Message flow request/response ---

// SendMessageRequest is the body of POST /message:send (and /message:stream).
type SendMessageRequest struct {
	Tenant        string                    `json:"tenant,omitempty"`
	Message       Message                   `json:"message"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// SendMessageConfiguration configures message.send behaviour.
type SendMessageConfiguration struct {
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	HistoryLength       int      `json:"historyLength,omitempty"`
	ReturnImmediately   bool     `json:"returnImmediately,omitempty"`
}

// SendMessageResponse is the response to POST /message:send. Exactly one of
// Task/Message is set.
type SendMessageResponse struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// ListTasksRequest captures the query-bound filters for GET /tasks.
type ListTasksRequest struct {
	ContextID     string
	Status        string
	PageSize      int
	PageToken     string
	HistoryLength int
}

// ListTasksResponse is the response to GET /tasks.
type ListTasksResponse struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken,omitempty"`
	PageSize      int    `json:"pageSize,omitempty"`
	TotalSize     int    `json:"totalSize,omitempty"`
}

// --- Errors ---

// Error represents an A2A protocol error returned by the server, parsed from
// the google.rpc.Status JSON envelope. It carries the HTTP status, the negative
// A2A error code, and the UPPER_SNAKE reason from the first details entry.
type Error struct {
	StatusCode int    // HTTP status code (e.g. 404)
	Code       int    // google.rpc.Status code (negative, e.g. -32001)
	Status     string // e.g. "NOT_FOUND"
	Message    string // human-readable message
	Reason     string // UPPER_SNAKE reason, e.g. "TASK_NOT_FOUND"
	Domain     string // "a2a-protocol.org"
}

// Error implements the error interface.
func (e *Error) Error() string {
	var parts []string
	if e.Code != 0 {
		parts = append(parts, fmt.Sprintf("[%d]", e.Code))
	}
	if e.Status != "" {
		parts = append(parts, e.Status)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Reason != "" {
		parts = append(parts, "("+e.Reason+")")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return strings.Join(parts, " ")
}

// IsNotFound reports whether err is an A2A TASK_NOT_FOUND error (or a bare
// 404 with no parsed envelope).
func IsNotFound(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason == "TASK_NOT_FOUND" || e.StatusCode == http.StatusNotFound
	}
	return false
}

// Reason returns the UPPER_SNAKE reason string from an A2A error, or "" when
// err is not an A2A *Error.
func Reason(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

// parseErrorResponse parses an A2A google.rpc.Status error envelope into an
// *Error. Falls back to a plain-text error when the body is not the expected
// shape.
func parseErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("failed to read error response: %v", err),
		}
	}

	var env struct {
		Error struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				Type     string         `json:"@type"`
				Reason   string         `json:"reason"`
				Domain   string         `json:"domain"`
				Metadata map[string]any `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &env); err == nil && (env.Error.Message != "" || env.Error.Code != 0) {
		e := &Error{
			StatusCode: resp.StatusCode,
			Code:       env.Error.Code,
			Status:     env.Error.Status,
			Message:    env.Error.Message,
		}
		if len(env.Error.Details) > 0 {
			e.Reason = env.Error.Details[0].Reason
			e.Domain = env.Error.Details[0].Domain
		}
		return e
	}

	return &Error{
		StatusCode: resp.StatusCode,
		Message:    strings.TrimSpace(string(body)),
	}
}

// --- SSE ---

// SSEStream provides a streaming reader for A2A SSE StreamResponse events from
// message:stream. Call Next() to receive events; call Close() when done.
type SSEStream struct {
	resp    *http.Response
	scanner *bufio.Scanner
}

// Next reads the next StreamResponse event from the stream.
// Returns io.EOF when the stream ends.
func (s *SSEStream) Next() (*StreamResponse, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// SSE data lines start with "data: ".
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil, io.EOF
			}

			var event StreamResponse
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return nil, fmt.Errorf("failed to decode SSE event: %w", err)
			}
			return &event, nil
		}

		// Skip empty lines and comments (: prefixed lines).
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
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// setA2AHeaders applies the A2A content type, accept, and version headers.
func setA2AHeaders(req *http.Request, body bool) {
	if body {
		req.Header.Set("Content-Type", A2AContentType)
	}
	req.Header.Set("Accept", A2AContentType)
	req.Header.Set(A2AVersionHeader, A2AProtocolVersion)
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
	setA2AHeaders(req, body != nil)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp)
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

// --- API Methods ---

// ExtendedAgentCard fetches the authenticated per-project AgentCard.
// GET /extendedAgentCard
func (c *Client) ExtendedAgentCard(ctx context.Context) (*AgentCard, error) {
	var card AgentCard
	if err := c.doJSON(ctx, "GET", "/extendedAgentCard", nil, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// SendMessage sends a message (synchronously or async via configuration.
// returnImmediately). Returns the response carrying either a task or a message.
// POST /message:send
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResponse, error) {
	var result SendMessageResponse
	if err := c.doJSON(ctx, "POST", "/message:send", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// StreamMessage sends a message over SSE and returns a stream of
// StreamResponse events. The caller must call stream.Close() when done.
// POST /message:stream
func (c *Client) StreamMessage(ctx context.Context, req SendMessageRequest) (*SSEStream, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/message:stream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuth(httpReq)
	setA2AHeaders(httpReq, true)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use a client without timeout for streaming connections.
	streamClient := &http.Client{
		Transport: c.http.Transport,
	}

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, parseErrorResponse(resp)
	}

	return &SSEStream{
		resp:    resp,
		scanner: bufio.NewScanner(resp.Body),
	}, nil
}

// GetTask retrieves a single task by ID, optionally requesting the given
// history length.
// GET /tasks/{id}?historyLength=
func (c *Client) GetTask(ctx context.Context, id string, historyLength int) (*Task, error) {
	path := "/tasks/" + url.PathEscape(id)
	if historyLength > 0 {
		path += "?historyLength=" + strconv.Itoa(historyLength)
	}
	var task Task
	if err := c.doJSON(ctx, "GET", path, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks lists project-scoped tasks, optionally filtered by context and/or
// status.
// GET /tasks?contextId=&status=&pageSize=&pageToken=&historyLength=
func (c *Client) ListTasks(ctx context.Context, req ListTasksRequest) (*ListTasksResponse, error) {
	q := url.Values{}
	if req.ContextID != "" {
		q.Set("contextId", req.ContextID)
	}
	if req.Status != "" {
		q.Set("status", req.Status)
	}
	if req.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(req.PageSize))
	}
	if req.PageToken != "" {
		q.Set("pageToken", req.PageToken)
	}
	if req.HistoryLength > 0 {
		q.Set("historyLength", strconv.Itoa(req.HistoryLength))
	}

	path := "/tasks"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var result ListTasksResponse
	if err := c.doJSON(ctx, "GET", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelTask cancels a running or queued task and returns the updated task.
// POST /tasks/{id}:cancel
func (c *Client) CancelTask(ctx context.Context, id string) (*Task, error) {
	var task Task
	path := "/tasks/" + url.PathEscape(id) + ":cancel"
	if err := c.doJSON(ctx, "POST", path, nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func strPtr(s string) *string {
	return &s
}
