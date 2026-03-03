# Go SDK

The Emergent Go SDK is a fully type-safe client library for the Emergent API, providing 29 service clients that cover the complete API surface.

## Installation

```bash
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@latest
```

**Module path:** `github.com/emergent-company/emergent/apps/server-go/pkg/sdk`

!!! note "Sub-module tags"
    The SDK lives inside the `emergent` monorepo as a Go sub-module. Tags follow the pattern
    `apps/server-go/pkg/sdk/vX.Y.Z`. Use `@latest` or a specific tag like
    `@apps/server-go/pkg/sdk/v0.8.0`.

## Current Version

**v0.8.0** (unreleased) — See [Changelog](changelog.md) for full history.

## Features

| Feature | Details |
|---------|---------|
| **Service clients** | 29 clients covering the full API surface |
| **Authentication** | API key, API token (`emt_*`), and OAuth device flow |
| **Multi-tenancy** | `SetContext(orgID, projectID)` propagates to all 25 context-scoped clients |
| **Streaming** | SSE streaming for chat responses via `chat.StreamChat` |
| **Error handling** | Structured `errors.Error` with `IsNotFound`, `IsForbidden`, etc. |
| **Graph utilities** | `graphutil.IDSet`, `ObjectIndex`, `UniqueByEntity` for the dual-ID model |
| **Thread safety** | `SetContext` and all service clients use `sync.RWMutex` |

## Quick Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
)

func main() {
    client, err := sdk.New(sdk.Config{
        ServerURL: "https://api.emergent-company.ai",
        Auth: sdk.AuthConfig{
            Mode:   "apikey",
            APIKey: "your-api-key",
        },
        OrgID:     "org_abc123",
        ProjectID: "proj_xyz789",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // List graph objects
    resp, err := client.Graph.ListObjects(context.Background(), nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Found %d objects\n", len(resp.Objects))
}
```

## Service Clients

### Context-Scoped (25 clients)

These clients send `X-Org-ID` and `X-Project-ID` headers on every request. Call `client.SetContext(orgID, projectID)` to update all of them atomically.

| Client | Field | Description |
|--------|-------|-------------|
| Graph | `client.Graph` | Graph objects, relationships, search, traversal |
| Documents | `client.Documents` | Document upload, retrieval, deletion |
| Search | `client.Search` | Unified lexical/semantic/hybrid search |
| Chat | `client.Chat` | Conversations, messages, SSE streaming |
| Chunks | `client.Chunks` | Chunk retrieval and deletion |
| Chunking | `client.Chunking` | Re-chunk documents |
| Agents | `client.Agents` | AI agent lifecycle and execution |
| AgentDefinitions | `client.AgentDefinitions` | Agent definition CRUD |
| Branches | `client.Branches` | Graph branch management |
| Projects | `client.Projects` | Project CRUD and member management |
| Orgs | `client.Orgs` | Organization CRUD |
| Users | `client.Users` | User profile management |
| APITokens | `client.APITokens` | API token lifecycle |
| MCP | `client.MCP` | MCP JSON-RPC client |
| MCPRegistry | `client.MCPRegistry` | MCP server registry |
| DataSources | `client.DataSources` | Data source integrations |
| DiscoveryJobs | `client.DiscoveryJobs` | Type discovery workflow |
| EmbeddingPolicy | `client.EmbeddingPolicy` | Embedding policy management |
| Integrations | `client.Integrations` | Third-party integrations |
| TemplatePacks | `client.TemplatePacks` | Template pack assignment |
| TypeRegistry | `client.TypeRegistry` | Project type definitions |
| Notifications | `client.Notifications` | Notification management |
| Tasks | `client.Tasks` | Background task tracking |
| Monitoring | `client.Monitoring` | Extraction job monitoring |
| UserActivity | `client.UserActivity` | User activity tracking |

### Non-Context (4 clients)

These clients do not require org/project context.

| Client | Field | Description |
|--------|-------|-------------|
| Health | `client.Health` | Health probes, debug info, job metrics |
| Superadmin | `client.Superadmin` | Administrative operations (superadmin role required) |
| APIDocs | `client.APIDocs` | Built-in API documentation browser |
| Provider | `client.Provider` | AI provider/model configuration |

## Guides

- [Authentication](authentication.md) — API key, API token, and OAuth device flow
- [Multi-Tenancy](multi-tenancy.md) — `SetContext` and context-scoped clients
- [Error Handling](error-handling.md) — Structured errors with predicates
- [Streaming](streaming.md) — SSE chat streaming
- [Graph ID Model](graph-id-model.md) — Dual-ID model: `VersionID` vs `EntityID`
