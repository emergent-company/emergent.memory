# Streaming

The `chat` client supports real-time streaming via Server-Sent Events (SSE). Use `StreamChat` to get an incremental token stream from the AI model.

## Basic Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    sdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
    "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/chat"
)

func streamResponse(client *sdk.Client, conversationID string) {
    ctx := context.Background()

    stream, err := client.Chat.StreamChat(ctx, &chat.StreamRequest{
        ConversationID: conversationID,
        Message:        "What objects are in this project?",
    })
    if err != nil {
        log.Fatal("failed to start stream:", err)
    }
    defer stream.Close()

    fmt.Print("Response: ")
    for event := range stream.Events() {
        switch event.Type {
        case "token":
            fmt.Print(event.Token)
        case "done":
            fmt.Println()
            fmt.Println("[stream complete]")
            return
        case "error":
            log.Printf("stream error: %s", event.Error)
            return
        }
    }

    // Check for stream-level error after channel closes
    if err := stream.Err(); err != nil {
        log.Printf("stream ended with error: %v", err)
    }
}
```

## StreamRequest

```go
type StreamRequest struct {
    ConversationID string // Required: ID of the conversation to stream into
    Message        string // Required: User message to send
}
```

## Stream Type

```go
type Stream struct { /* ... */ }

func (s *Stream) Events() <-chan *StreamEvent  // Receive events as they arrive
func (s *Stream) Close() error                 // Close the stream and release resources
func (s *Stream) Err() error                   // Check for a stream-level error after close
```

`Events()` returns a read-only channel. The channel is closed automatically when the stream ends (either with a `done` event or an error).

## StreamEvent Types

| `Type` | Meaning | Field |
|--------|---------|-------|
| `"meta"` | Stream metadata | `ConversationID` contains the conversation ID |
| `"token"` | Incremental text token | `Token` contains text to append to output |
| `"done"` | Stream complete | `Message` may contain final message text |
| `"error"` | Stream-level error | `Error` contains error description |

## Usage Pattern

Always `defer stream.Close()` immediately after receiving the `*Stream`. This ensures the HTTP connection is released even if you exit early.

```go
stream, err := client.Chat.StreamChat(ctx, req)
if err != nil {
    return err
}
defer stream.Close()  // ← always close

for event := range stream.Events() {
    // process events
}
```

## Creating a Conversation First

`StreamChat` requires an existing conversation ID. Create one first with `CreateConversation`:

```go
conv, err := client.Chat.CreateConversation(ctx, &chat.CreateConversationRequest{
    Title: "My conversation",
})
if err != nil {
    log.Fatal(err)
}

stream, err := client.Chat.StreamChat(ctx, &chat.StreamRequest{
    ConversationID: conv.ID,
    Message:        "Hello!",
})
```

See the [chat reference](reference/chat.md) for the complete Chat client API.
