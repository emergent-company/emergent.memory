// Package chat provides the Chat service client for the Emergent API SDK.
// Supports both standard and streaming chat interactions via SSE.
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

// Conversation represents a chat conversation.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message represents a chat message.
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"` // user, assistant, system
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// SendMessageRequest represents a request to send a message.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// StreamEvent represents an SSE event from the chat stream.
type StreamEvent struct {
	Type    string `json:"type"` // token, done, error
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Stream represents an active SSE stream.
type Stream struct {
	resp   *http.Response
	reader *bufio.Reader
	events chan *StreamEvent
	done   chan struct{}
	err    error
}

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

// ListConversations retrieves a list of conversations.
func (c *Client) ListConversations(ctx context.Context) ([]Conversation, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/conversations", nil)
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

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Data []Conversation `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// SendMessage sends a message and returns the complete response.
func (c *Client) SendMessage(ctx context.Context, conversationID string, req *SendMessageRequest) (*Message, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/api/conversations/"+conversationID+"/messages",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		httpReq.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		httpReq.Header.Set("X-Project-ID", c.projectID)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result struct {
		Data Message `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result.Data, nil
}

// SendMessageStream sends a message and returns a streaming response.
func (c *Client) SendMessageStream(ctx context.Context, conversationID string, req *SendMessageRequest) (*Stream, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.base+"/api/conversations/"+conversationID+"/messages/stream",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if c.orgID != "" {
		httpReq.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		httpReq.Header.Set("X-Project-ID", c.projectID)
	}

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
