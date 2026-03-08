# Multi-Tenancy

The Emergent platform is multi-tenant: resources are scoped to an **organization** and a **project**. The SDK propagates these scopes via `X-Org-ID` and `X-Project-ID` HTTP headers on every API call made through context-scoped service clients.

## Setting Context

Call `client.SetContext(orgID, projectID)` to update the active org and project for all context-scoped clients atomically:

```go
client.SetContext("org_abc123", "proj_xyz789")

// All subsequent calls use the new context
resp, err := client.Graph.ListObjects(ctx, nil)
```

`SetContext` is concurrency-safe — it holds a write lock while updating all sub-clients, so no API call can observe a partially-updated context.

You can also set context at construction time via `sdk.Config`:

```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth:      ...,
    OrgID:     "org_abc123",
    ProjectID: "proj_xyz789",
})
```

## Context-Scoped vs Non-Context Clients

### Context-Scoped (25 clients)

These clients send `X-Org-ID` and `X-Project-ID` on every request. **21 of them** have a `SetContext` method and are updated atomically when you call `client.SetContext`. The remaining 4 (Projects, Orgs, Users, APITokens) use the org/project set at construction time and require re-initialization to change context.

| Field | Package |
|-------|---------|
| `client.Documents` | `documents` |
| `client.Chunks` | `chunks` |
| `client.Search` | `search` |
| `client.Graph` | `graph` |
| `client.Chat` | `chat` |
| `client.MCPRegistry` | `mcpregistry` |
| `client.Branches` | `branches` |
| `client.UserActivity` | `useractivity` |
| `client.TypeRegistry` | `typeregistry` |
| `client.Notifications` | `notifications` |
| `client.Tasks` | `tasks` |
| `client.Monitoring` | `monitoring` |
| `client.Agents` | `agents` |
| `client.AgentDefinitions` | `agentdefinitions` |
| `client.DataSources` | `datasources` |
| `client.DiscoveryJobs` | `discoveryjobs` |
| `client.EmbeddingPolicy` | `embeddingpolicies` |
| `client.Integrations` | `integrations` |
| `client.TemplatePacks` | `templatepacks` |
| `client.Chunking` | `chunking` |
| `client.Projects` | `projects` |
| `client.Orgs` | `orgs` |
| `client.Users` | `users` |
| `client.APITokens` | `apitokens` |
| `client.MCP` | `mcp` (project-only) |

!!! note "MCP is a special case"
    `client.MCP.SetContext(projectID string)` only takes `projectID` — it does not use `orgID`.
    When you call `client.SetContext(orgID, projectID)`, it calls `mcp.SetContext(projectID)`.

### Non-Context Clients (4 clients)

These clients do not require or send org/project headers. They have no `SetContext` method and are not updated by `client.SetContext`.

| Field | Package | Notes |
|-------|---------|-------|
| `client.Health` | `health` | Health probes, debug, job metrics |
| `client.Superadmin` | `superadmin` | Requires superadmin role |
| `client.APIDocs` | `apidocs` | Built-in API documentation |
| `client.Provider` | `provider` | AI provider/model configuration |

## Switching Projects

To serve multiple projects from a single client, call `SetContext` between operations:

```go
// Work in project A
client.SetContext("org_abc", "proj_A")
doWorkInProjectA(client)

// Switch to project B
client.SetContext("org_abc", "proj_B")
doWorkInProjectB(client)
```

!!! warning "Not safe for concurrent cross-project access"
    `SetContext` changes the context for **all** service clients. If you need concurrent
    access to multiple projects, create separate `sdk.Client` instances — one per project.
