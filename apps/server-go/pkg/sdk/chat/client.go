// Package chat provides the Chat service client for the Emergent API SDK.
// Supports both standard CRUD and streaming chat interactions via SSE.
package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Chat API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// =============================================================================
// SDK Types
// =============================================================================

// Conversation represents a chat conversation.
type Conversation struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	OwnerUserID  *string   `json:"ownerUserId,omitempty"`
	IsPrivate    bool      `json:"isPrivate"`
	ProjectID    *string   `json:"projectId,omitempty"`
	DraftText    *string   `json:"draftText,omitempty"`
	ObjectID     *string   `json:"objectId,omitempty"`
	CanonicalID  *string   `json:"canonicalId,omitempty"`
	EnabledTools []string  `json:"enabledTools,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Message represents a chat message.
type Message struct {
	ID               string          `json:"id"`
	ConversationID   string          `json:"conversationId"`
	Role             string          `json:"role"` // user, assistant, system
	Content          string          `json:"content"`
	Citations        json.RawMessage `json:"citations,omitempty"`
	ContextSummary   *string         `json:"contextSummary,omitempty"`
	RetrievalContext json.RawMessage `json:"retrievalContext,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
}

// ConversationWithMessages is the response for GetConversation.
type ConversationWithMessages struct {
	Conversation
	Messages []Message `json:"messages"`
}

// ListConversationsOptions holds query parameters for listing conversations.
type ListConversationsOptions struct {
	Limit  int
	Offset int
}

// ListConversationsResponse is the response from listing conversations.
type ListConversationsResponse struct {
	Conversations []Conversation `json:"conversations"`
	Total         int            `json:"total"`
}

// CreateConversationRequest is the request body for creating a conversation.
type CreateConversationRequest struct {
	Title       string  `json:"title"`
	Message     string  `json:"message"`
	CanonicalID *string `json:"canonicalId,omitempty"`
}

// UpdateConversationRequest is the request body for updating a conversation.
type UpdateConversationRequest struct {
	Title     *string `json:"title,omitempty"`
	DraftText *string `json:"draftText,omitempty"`
}

// AddMessageRequest is the request body for adding a message.
type AddMessageRequest struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	RetrievalContext json.RawMessage `json:"retrievalContext,omitempty"`
}

// StreamRequest is the request body for starting a chat stream.
type StreamRequest struct {
	ConversationID *string `json:"conversationId,omitempty"`
	Message        string  `json:"message"`
	CanonicalID    *string `json:"canonicalId,omitempty"`
}

// StreamEvent represents an SSE event from the chat stream.
type StreamEvent struct {
	Type    string `json:"type"` // meta, token, done, error
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	// Meta fields (emitted in "meta" event)
	ConversationID string `json:"conversationId,omitempty"`
}

// Stream represents an active SSE stream.
type Stream struct {
	resp   *http.Response
	reader *bufio.Reader
	events chan *StreamEvent
	done   chan struct{}
	err    error
}

// =============================================================================
// Constructor / context
// =============================================================================

// NewClient creates a new Chat service client.
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

// =============================================================================
// Internal helpers
// =============================================================================

func (c *Client) prepareRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}

	return req, nil
}

func (c *Client) doJSON(req *http.Request, result any) error {
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

func (c *Client) postJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	return c.doJSON(req, result)
}

func (c *Client) patchJSON(ctx context.Context, reqURL string, reqBody any, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.prepareRequest(ctx, "PATCH", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	return c.doJSON(req, result)
}

// =============================================================================
// Conversation CRUD
// =============================================================================

// ListConversations retrieves conversations for the current project.
// Server: GET /api/chat/conversations?limit=&offset=
func (c *Client) ListConversations(ctx context.Context, opts *ListConversationsOptions) (*ListConversationsResponse, error) {
	req, err := c.prepareRequest(ctx, "GET", c.base+"/api/chat/conversations", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
		req.URL.RawQuery = q.Encode()
	}

	var result ListConversationsResponse
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateConversation creates a new conversation with an initial message.
// Server: POST /api/chat/conversations → 201
func (c *Client) CreateConversation(ctx context.Context, req *CreateConversationRequest) (*Conversation, error) {
	var result Conversation
	if err := c.postJSON(ctx, c.base+"/api/chat/conversations", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetConversation retrieves a conversation with all its messages.
// Server: GET /api/chat/:id
func (c *Client) GetConversation(ctx context.Context, id string) (*ConversationWithMessages, error) {
	req, err := c.prepareRequest(ctx, "GET", c.base+"/api/chat/"+id, nil)
	if err != nil {
		return nil, err
	}

	var result ConversationWithMessages
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateConversation updates conversation properties.
// Server: PATCH /api/chat/:id (requires chat:admin scope)
func (c *Client) UpdateConversation(ctx context.Context, id string, req *UpdateConversationRequest) (*Conversation, error) {
	var result Conversation
	if err := c.patchJSON(ctx, c.base+"/api/chat/"+id, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteConversation permanently deletes a conversation and all its messages.
// Server: DELETE /api/chat/:id → 200 {status: "deleted"} (requires chat:admin scope)
func (c *Client) DeleteConversation(ctx context.Context, id string) error {
	req, err := c.prepareRequest(ctx, "DELETE", c.base+"/api/chat/"+id, nil)
	if err != nil {
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

// =============================================================================
// Messages
// =============================================================================

// AddMessage adds a message to an existing conversation.
// Server: POST /api/chat/:id/messages → 201
func (c *Client) AddMessage(ctx context.Context, conversationID string, req *AddMessageRequest) (*Message, error) {
	var result Message
	if err := c.postJSON(ctx, c.base+"/api/chat/"+conversationID+"/messages", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// =============================================================================
// Streaming
// =============================================================================

// StreamChat starts a streaming chat session via SSE.
// Server: POST /api/chat/stream → SSE stream
func (c *Client) StreamChat(ctx context.Context, req *StreamRequest) (*Stream, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := c.prepareRequest(ctx, "POST", c.base+"/api/chat/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	stream := &Stream{
		resp:   resp,
		reader: bufio.NewReader(resp.Body),
		events: make(chan *StreamEvent),
		done:   make(chan struct{}),
	}

	go stream.readEvents()

	return stream, nil
}

// Events returns a channel of stream events.
func (s *Stream) Events() <-chan *StreamEvent {
	return s.events
}

// Close closes the stream.
func (s *Stream) Close() error {
	close(s.done)
	return s.resp.Body.Close()
}

// Err returns any error that occurred during streaming.
func (s *Stream) Err() error {
	return s.err
}

func (s *Stream) readEvents() {
	defer close(s.events)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				s.err = err
				s.events <- &StreamEvent{Type: "error", Error: err.Error()}
			}
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				s.err = err
				s.events <- &StreamEvent{Type: "error", Error: err.Error()}
				return
			}

			s.events <- &event

			if event.Type == "done" || event.Type == "error" {
				return
			}
		}
	}
}
