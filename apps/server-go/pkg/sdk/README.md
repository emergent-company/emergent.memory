# Emergent Go SDK

Official Go client library for the Emergent API.

## Installation

```bash
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@latest
```

## Features

- **Type-safe API client** for all Emergent endpoints
- **Dual authentication** - API key (standalone) and OAuth (full deployment)
- **Multi-tenancy support** - Organization and project context management
- **26 service clients** covering the full API surface
- **Error handling** - Structured errors with predicates
- **Streaming support** - SSE for chat responses
- **OAuth device flow** - Interactive authentication with token refresh
- **Working examples** - Ready-to-run example programs (see `examples/`)

## Quick Start

### API Key Authentication (Standalone Mode)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
)

func main() {
    // Create client with API key
    client, err := sdk.New(sdk.Config{
        ServerURL: "http://localhost:9090",
        Auth: sdk.AuthConfig{
            Mode:   "apikey",
            APIKey: "emt_abc123...",
        },
        OrgID:     "org_123",
        ProjectID: "proj_456",
    })
    if err != nil {
        log.Fatal(err)
    }

    // List documents
    docs, err := client.Documents.List(context.Background(), nil)
    if err != nil {
        log.Fatal(err)
    }

    for _, doc := range docs.Documents {
        fmt.Printf("Document: %s - %s\n", doc.ID, doc.Title)
    }
}
```

### OAuth Authentication (Full Deployment)

```go
// OAuth device flow - interactive authentication
client, err := sdk.NewWithDeviceFlow(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        ClientID:  "emergent-sdk",
        CredsPath: "~/.emergent/credentials.json",
    },
})
// Displays URL and code for browser authentication
// Waits for user to complete OAuth flow
// Stores credentials for future use
```

## Service Clients

All service clients are accessible as fields on the main `sdk.Client`:

### Context-Scoped Clients (org/project aware)

| Client          | Field                    | Description                                              |
| --------------- | ------------------------ | -------------------------------------------------------- |
| Documents       | `client.Documents`       | Document CRUD, upload, download, batch operations        |
| Chunks          | `client.Chunks`          | Chunk listing, retrieval, search, deletion               |
| Chunking        | `client.Chunking`        | Re-chunk documents with current strategy                 |
| Search          | `client.Search`          | Unified search (lexical, semantic, hybrid)               |
| Graph           | `client.Graph`           | Knowledge graph objects, relationships, branches, search |
| Chat            | `client.Chat`            | Conversations CRUD, streaming chat                       |
| Branches        | `client.Branches`        | Graph branch management                                  |
| Projects        | `client.Projects`        | Project CRUD, members                                    |
| Orgs            | `client.Orgs`            | Organization CRUD                                        |
| Users           | `client.Users`           | User profile management                                  |
| APITokens       | `client.APITokens`       | API token lifecycle                                      |
| MCP             | `client.MCP`             | Model Context Protocol (JSON-RPC)                        |
| UserActivity    | `client.UserActivity`    | User activity tracking                                   |
| TypeRegistry    | `client.TypeRegistry`    | Project type definitions                                 |
| Notifications   | `client.Notifications`   | Notification management                                  |
| Tasks           | `client.Tasks`           | Background task tracking                                 |
| Monitoring      | `client.Monitoring`      | Extraction job monitoring                                |
| Agents          | `client.Agents`          | Background agent management (admin)                      |
| DataSources     | `client.DataSources`     | Data source integrations, sync jobs                      |
| DiscoveryJobs   | `client.DiscoveryJobs`   | Type discovery workflows                                 |
| EmbeddingPolicy | `client.EmbeddingPolicy` | Embedding policy configuration                           |
| Integrations    | `client.Integrations`    | Third-party integrations                                 |
| TemplatePacks   | `client.TemplatePacks`   | Template pack assignment and types                       |

### Non-Context Clients

| Client     | Field               | Description                                     |
| ---------- | ------------------- | ----------------------------------------------- |
| Health     | `client.Health`     | Health checks, readiness, debug, metrics        |
| Superadmin | `client.Superadmin` | Administrative operations (requires superadmin) |
| APIDocs    | `client.APIDocs`    | Built-in API documentation                      |

### Documents

```go
// List documents with pagination
docs, err := client.Documents.List(ctx, &documents.ListOptions{
    Limit: 50,
})

// Get a single document
doc, err := client.Documents.Get(ctx, "doc_123")

// Upload a file
doc, err := client.Documents.Upload(ctx, "file.pdf", fileReader, fileSize)

// Download a document (returns signed URL)
url, err := client.Documents.Download(ctx, "doc_123")
```

### Graph

```go
// Search graph objects
objects, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
    Query: "machine learning",
    Limit: 10,
})

// Create an object
obj, err := client.Graph.CreateObject(ctx, &graph.CreateObjectRequest{
    Name: "Neural Networks",
    Type: "Concept",
})

// Search relationships
rels, err := client.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{
    ObjectID: "obj_123",
})
```

### Chat

```go
// List conversations
conversations, err := client.Chat.ListConversations(ctx)

// Stream a chat response
stream, err := client.Chat.StreamChat(ctx, &chat.StreamRequest{
    ConversationID: "conv_123",
    Message:        "What is machine learning?",
})
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for event := range stream.Events() {
    switch event.Type {
    case "token":
        fmt.Print(event.Token)
    case "done":
        fmt.Println("\nResponse complete")
    case "error":
        log.Printf("Error: %s", event.Error)
    }
}
```

### Health

```go
// Check service health
health, err := client.Health.Health(ctx)
fmt.Printf("Status: %s (Uptime: %s)\n", health.Status, health.Uptime)

// Check readiness (for load balancers)
ready, err := client.Health.Ready(ctx)

// Debug info
debug, err := client.Health.Debug(ctx)

// Job metrics
metrics, err := client.Health.JobMetrics(ctx)
```

## Multi-Tenancy Context

```go
// Set context for all subsequent requests
client.SetContext("org_789", "proj_012")
```

## Error Handling

The SDK provides structured errors with type predicates:

```go
doc, err := client.Documents.Get(ctx, "invalid-id")
if err != nil {
    if sdkerrors.IsNotFound(err) {
        fmt.Println("Document not found")
    } else if sdkerrors.IsUnauthorized(err) {
        fmt.Println("Authentication failed")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Architecture

The SDK uses a manual implementation approach (not code-generated) for better Go idioms.

**Design decisions:**

- Separate service clients under main `Client` struct (inspired by AWS SDK)
- `auth.Provider` interface for pluggable authentication
- SDK-specific types (no domain imports)
- `string` for all IDs (not `uuid.UUID`)
- Context propagation for all API calls
- All URLs use `/api/` prefix

## Examples

See the `examples/` directory for complete working examples:

- `examples/basic/` - Basic SDK setup and health check
- `examples/documents/` - Document and chunk management
- `examples/search/` - Search queries with different strategies
- `examples/projects/` - Project CRUD operations

```bash
cd apps/server-go/pkg/sdk/examples/basic
export EMERGENT_API_KEY="your_api_key"
go run main.go
```

## License

See the main [Emergent LICENSE](../../LICENSE) file.
