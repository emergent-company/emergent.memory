# Emergent Go SDK

Official Go client library for the Emergent API.

## Installation

```bash
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@latest
```

## Features

- ✅ **Type-safe API client** for all Emergent endpoints
- ✅ **Dual authentication** - API key (standalone) and OAuth (full deployment)
- ✅ **Multi-tenancy support** - Organization and project context management
- ✅ **Service clients** - Documents, Chunks, Search, Graph, Chat, Projects, Orgs, Users, API Tokens, Health, MCP
- ✅ **Error handling** - Structured errors with predicates
- ✅ **Streaming support** - SSE for chat responses
- ✅ **OAuth device flow** - Interactive authentication with token refresh
- ✅ **Management APIs** - Complete CRUD for Projects, Organizations, Users, API Tokens
- ✅ **Working examples** - 4 ready-to-run example programs (see `examples/`)
- ✅ **Comprehensive tests** - 43+ test cases with 37.6% coverage
- ⏳ **Pagination iterators** - Auto-pagination for large result sets (coming soon)

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

    for _, doc := range docs.Data {
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

### Documents

```go
// List documents with pagination
docs, err := client.Documents.List(ctx, &documents.ListOptions{
    Limit: 50,
})

// Get a single document
doc, err := client.Documents.Get(ctx, "doc_123")
```

### Chunks

```go
// List all chunks for a document
chunks, err := client.Chunks.List(ctx, &chunks.ListOptions{
    DocumentID: "doc_123",
    Limit:      100,
})
```

### Search

```go
// Perform a search query
results, err := client.Search.Search(ctx, &search.SearchRequest{
    Query:    "artificial intelligence",
    Strategy: "hybrid", // lexical, semantic, or hybrid
    Limit:    10,
})

for _, result := range results.Results {
    fmt.Printf("Score: %.2f - %s\n", result.Score, result.Content)
}
```

### Graph

```go
// List graph objects
objects, err := client.Graph.ListObjects(ctx)

// Get a specific object
obj, err := client.Graph.GetObject(ctx, "obj_123")

// List relationships
relationships, err := client.Graph.ListRelationships(ctx)
```

### Chat

```go
// List conversations
conversations, err := client.Chat.ListConversations(ctx)

// Send a message (non-streaming)
msg, err := client.Chat.SendMessage(ctx, "conv_123", &chat.SendMessageRequest{
    Content: "What is machine learning?",
})

// Send a message with streaming
stream, err := client.Chat.SendMessageStream(ctx, "conv_123", &chat.SendMessageRequest{
    Content: "Explain neural networks",
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

### Projects

```go
// List all projects
projects, err := client.Projects.List(ctx, &projects.ListOptions{
    Limit: 100,
    OrgID: "org_123", // optional: filter by organization
})

// Get a specific project
project, err := client.Projects.Get(ctx, "proj_456")

// Create a new project
newProject, err := client.Projects.Create(ctx, &projects.CreateProjectRequest{
    Name:  "My New Project",
    OrgID: "org_123",
})

// Update project settings
updated, err := client.Projects.Update(ctx, "proj_456", &projects.UpdateProjectRequest{
    Name:               &newName,
    KBPurpose:          &purpose,
    AutoExtractObjects: &autoExtract,
})

// Delete a project
err = client.Projects.Delete(ctx, "proj_456")

// List project members
members, err := client.Projects.ListMembers(ctx, "proj_456")

// Remove a member
err = client.Projects.RemoveMember(ctx, "proj_456", "user_789")
```

### Organizations

```go
// List all organizations
orgs, err := client.Orgs.List(ctx)

// Get a specific organization
org, err := client.Orgs.Get(ctx, "org_123")

// Create a new organization
newOrg, err := client.Orgs.Create(ctx, &orgs.CreateOrganizationRequest{
    Name: "My Company",
})

// Delete an organization
err = client.Orgs.Delete(ctx, "org_123")
```

### Users

```go
// Get current user's profile
profile, err := client.Users.GetProfile(ctx)
fmt.Printf("User: %s (%s)\n", *profile.DisplayName, profile.Email)

// Update profile
updatedProfile, err := client.Users.UpdateProfile(ctx, &users.UpdateProfileRequest{
    FirstName:   &firstName,
    LastName:    &lastName,
    DisplayName: &displayName,
    PhoneE164:   &phone,
})
```

### API Tokens

```go
// Create a new API token
token, err := client.APITokens.Create(ctx, "proj_456", &apitokens.CreateTokenRequest{
    Name:   "Production Token",
    Scopes: []string{"documents:read", "documents:write"},
})
fmt.Printf("Save this token: %s\n", token.Token) // Only shown once!

// List all tokens for a project
tokens, err := client.APITokens.List(ctx, "proj_456")

// Get a specific token
token, err := client.APITokens.Get(ctx, "proj_456", "token_id")

// Revoke a token
err = client.APITokens.Revoke(ctx, "proj_456", "token_id")
```

### Health

```go
// Check service health
health, err := client.Health.Health(ctx)
fmt.Printf("Status: %s (Uptime: %s)\n", health.Status, health.Uptime)

// Check readiness (for load balancers)
ready, err := client.Health.Ready(ctx)
if ready {
    fmt.Println("Service is ready")
}

// Liveness probe
err = client.Health.Healthz(ctx)
```

### MCP (Model Context Protocol)

```go
// Initialize MCP session
err = client.MCP.Initialize(ctx)

// List available tools
tools, err := client.MCP.ListTools(ctx)

// Call an MCP tool
result, err := client.MCP.CallTool(ctx, "search_entities", map[string]interface{}{
    "query": "machine learning",
    "type_filter": []string{"Document"},
})

// List resources
resources, err := client.MCP.ListResources(ctx)

// Read a resource
content, err := client.MCP.ReadResource(ctx, "emergent://schema/entity-types")

// List prompts
prompts, err := client.MCP.ListPrompts(ctx)

// Get a prompt
prompt, err := client.MCP.GetPrompt(ctx, "explore_entity_type", map[string]interface{}{
    "entity_type": "Person",
    "limit": 50,
})
```

## Multi-Tenancy Context

You can set default organization and project context on the client:

```go
// Set context for all subsequent requests
client.SetContext("org_789", "proj_012")

// Or override per request (coming soon)
docs, err := client.Documents.List(ctx, sdk.WithOrg("org_xyz"), sdk.WithProject("proj_abc"))
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

## Development Status

This SDK has completed **Phases 1, 2, 3, and partial Phase 4**. Current status:

- ✅ Core client infrastructure
- ✅ API key authentication
- ✅ OAuth device flow with token refresh
- ✅ Documents service
- ✅ Chunks service
- ✅ Search service
- ✅ Graph service (objects, relationships)
- ✅ Chat service with SSE streaming
- ✅ **Projects service** (CRUD, members management)
- ✅ **Organizations service** (CRUD)
- ✅ **Users service** (profile management)
- ✅ **API Tokens service** (create, list, revoke)
- ✅ **Health service** (health, readiness, liveness probes)
- ✅ **MCP service** (Model Context Protocol JSON-RPC)
- ✅ **Test coverage** - 43 test cases, 37.6% coverage (Phase 4)
- ✅ **Working examples** - 4 example programs in `examples/` (Phase 4)
- ⏳ CLI migration (Phase 5)

## Architecture

The SDK uses a manual implementation approach (not code-generated) for better Go idioms, while leveraging the complete OpenAPI specification (195 endpoints, 17,289 lines) as the source of truth for API contracts.

**Design decisions:**

- Separate service clients under main `Client` struct (inspired by AWS SDK)
- `auth.Provider` interface for pluggable authentication
- SDK-specific types matching OpenAPI schemas
- Context propagation for all API calls
- Iterator pattern for pagination (coming soon)

## Examples

See the `examples/` directory for complete working examples:

- `examples/basic/` - Basic SDK setup and health check
- `examples/documents/` - Document and chunk management
- `examples/search/` - Search queries with different strategies
- `examples/projects/` - Project CRUD operations

Each example includes:

- Full source code with comments
- Usage instructions
- Expected output
- Error handling patterns

**Run an example:**

```bash
cd apps/server-go/pkg/sdk/examples/basic
export EMERGENT_API_KEY="your_api_key"
go run main.go
```

See `examples/README.md` for detailed documentation.

## Contributing

This SDK is part of the [Emergent project](https://github.com/emergent-company/emergent).

For implementation details, see:

- [Proposal](../../openspec/changes/add-go-sdk-library/proposal.md)
- [Design Document](../../openspec/changes/add-go-sdk-library/design.md)
- [Task Checklist](../../openspec/changes/add-go-sdk-library/tasks.md)

## License

See the main [Emergent LICENSE](../../LICENSE) file.
