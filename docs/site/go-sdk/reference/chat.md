# chat

Package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk/chat`

The `chat` client manages AI conversations and provides SSE streaming for real-time token delivery.

## Methods

```go
func (c *Client) ListConversations(ctx context.Context, opts *ListConversationsOptions) (*ListConversationsResponse, error)
func (c *Client) CreateConversation(ctx context.Context, req *CreateConversationRequest) (*Conversation, error)
func (c *Client) GetConversation(ctx context.Context, id string) (*ConversationWithMessages, error)
func (c *Client) UpdateConversation(ctx context.Context, id string, req *UpdateConversationRequest) (*Conversation, error)
func (c *Client) DeleteConversation(ctx context.Context, id string) error
func (c *Client) AddMessage(ctx context.Context, conversationID string, req *AddMessageRequest) (*Message, error)
func (c *Client) StreamChat(ctx context.Context, req *StreamRequest) (*Stream, error)
```

## Key Types

### Conversation

```go
type Conversation struct {
    ID        string
    Title     string
    ProjectID string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Message

```go
type Message struct {
    ID             string
    ConversationID string
    Role           string // "user" or "assistant"
    Content        string
    CreatedAt      time.Time
}
```

### ConversationWithMessages

```go
type ConversationWithMessages struct {
    Conversation
    Messages []Message
}
```

### ListConversationsOptions

```go
type ListConversationsOptions struct {
    Limit  int
    Offset int
}
```

### CreateConversationRequest

```go
type CreateConversationRequest struct {
    Title string
}
```

### StreamRequest

```go
type StreamRequest struct {
    ConversationID string // Required
    Message        string // Required: user message text
}
```

### StreamEvent

```go
type StreamEvent struct {
    Type           string // "meta", "token", "done", or "error"
    Token          string // Token text (non-empty on "token" events)
    Message        string // Final message text (non-empty on "done" events)
    Error          string // Error description (non-empty on "error" events)
    ConversationID string // Conversation ID (non-empty on "meta" events)
}
```

### Stream

```go
type Stream struct { /* ... */ }

func (s *Stream) Events() <-chan *StreamEvent
func (s *Stream) Close() error
func (s *Stream) Err() error
```

## Streaming Example

```go
// Create conversation
conv, err := client.Chat.CreateConversation(ctx, &chat.CreateConversationRequest{
    Title: "Research session",
})
if err != nil {
    return err
}

// Stream a response
stream, err := client.Chat.StreamChat(ctx, &chat.StreamRequest{
    ConversationID: conv.ID,
    Message:        "Summarize what you know about this project",
})
if err != nil {
    return err
}
defer stream.Close()

for event := range stream.Events() {
    switch event.Type {
    case "token":
        fmt.Print(event.Token)
    case "done":
        fmt.Println()
    case "error":
        return fmt.Errorf("stream error: %s", event.Error)
    }
}
```

## See Also

- [Streaming guide](../streaming.md)
