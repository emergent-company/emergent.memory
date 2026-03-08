# MCP Servers

Memory integrates with the [Model Context Protocol](https://modelcontextprotocol.io) in two ways:

1. **Built-in MCP server** — Memory itself is an MCP server. Clients like Claude Desktop or Cursor connect directly to Memory to read/write the knowledge graph.
2. **MCP registry** — Per-project registry of *external* MCP servers that agents can call as tools.

---

## Built-in Memory MCP server

Memory exposes its own MCP server at two endpoints:

| Transport | Endpoint | Authentication |
|---|---|---|
| SSE (legacy) | `GET /mcp/sse` | `?token=<api_token>` query param |
| Streamable HTTP (current spec) | `POST /mcp` | `Authorization: Bearer <api_token>` |

### Connecting Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "memory": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/client-sse"],
      "env": {
        "MCP_SERVER_URL": "https://api.dev.emergent-company.ai/mcp/sse",
        "MCP_API_TOKEN": "<your-api-token>"
      }
    }
  }
}
```

Or using the streamable HTTP transport with a client that supports it:

```
https://api.dev.emergent-company.ai/mcp
Authorization: Bearer <api-token>
```

### Available MCP tools (built-in)

The built-in server exposes tools including:

- **Knowledge graph** — create, update, search objects and relationships
- **Documents** — upload, list, retrieve documents
- **Search** — unified vector + keyword search
- **Chat** — create sessions, send messages
- **Agents** — list agents, trigger runs
- **MCP registry** — `mcp-servers/list`, `mcp-servers/call`

> See `apps/server/domain/mcp/README.md` for the complete tools, resources, and prompts reference.

### Brave Search integration

The built-in server includes a web-search tool when the environment variable `BRAVE_SEARCH_API_KEY` is set. When configured, agents gain access to a `web_search` tool backed by the Brave Search API.

---

## External MCP server registry

Each project can register external MCP servers. These servers are proxied on demand and their tool catalogs are mirrored locally so agents can discover tools without connecting to every server on each request.

### Server types

| Type | `type` value | Connection |
|---|---|---|
| Built-in (Memory) | `builtin` | Internal; not configurable |
| Subprocess (local) | `stdio` | Launches a command with arguments |
| Server-Sent Events | `sse` | Long-lived HTTP SSE connection |
| Plain HTTP | `http` | Per-request HTTP |

---

## Managing external servers

### List registered servers

```bash
curl https://api.dev.emergent-company.ai/api/admin/mcp-servers \
  -H "Authorization: Bearer <token>"
```

### Register a stdio server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-servers \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "filesystem",
    "type": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp/workspace"],
    "env": {},
    "enabled": true
  }'
```

### Register an SSE server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-servers \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-sse-server",
    "type": "sse",
    "url": "https://my-mcp-server.example.com/sse",
    "headers": { "Authorization": "Bearer <server-token>" },
    "enabled": true
  }'
```

### Register an HTTP server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-servers \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-http-server",
    "type": "http",
    "url": "https://my-mcp-server.example.com/mcp",
    "headers": {},
    "enabled": true
  }'
```

### Inspect a server (preview tools without saving)

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId>/inspect \
  -H "Authorization: Bearer <token>"
```

Returns the tool list from the live server without persisting anything.

### Sync tool catalog

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId>/sync \
  -H "Authorization: Bearer <token>"
```

Connects to the server, calls `tools/list`, and upserts results into the local tool catalog. Safe to call repeatedly (idempotent).

### List synced tools

```bash
curl https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId>/tools \
  -H "Authorization: Bearer <token>"
```

### Enable / disable a tool

```bash
curl -X PATCH https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId>/tools/<toolId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

Disabled tools are filtered from the agent context.

### Update a server

```bash
curl -X PATCH https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Delete a server

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/admin/mcp-servers/<serverId> \
  -H "Authorization: Bearer <token>"
```

---

## Official MCP registry

Browse and install servers from the official MCP registry at `registry.modelcontextprotocol.io`.

### Search the registry

```bash
curl "https://api.dev.emergent-company.ai/api/admin/mcp-registry/search?q=github" \
  -H "Authorization: Bearer <token>"
```

### Get details for a registry server

```bash
curl https://api.dev.emergent-company.ai/api/admin/mcp-registry/servers/github \
  -H "Authorization: Bearer <token>"
```

### Install from the registry

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/mcp-registry/install \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "serverName": "github",
    "config": {
      "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_..." }
    }
  }'
```

This creates a new `MCPServer` record for your project and immediately runs a sync to populate the tool catalog.

---

## Server entity reference

**`MCPServer`** — table `kb.mcp_servers`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `projectId` | UUID | Owning project |
| `name` | string | Display name |
| `enabled` | bool | Disabled servers are not proxied |
| `type` | string | `stdio` \| `sse` \| `http` \| `builtin` |
| `command` | string | Executable path (stdio only) |
| `args` | string[] | Arguments (stdio only) |
| `env` | object | Environment variables (stdio only) |
| `url` | string | Server URL (sse/http only) |
| `headers` | object | HTTP headers (sse/http only) |
| `createdAt` | timestamp | |
| `updatedAt` | timestamp | |

**`MCPServerTool`** — table `kb.mcp_server_tools`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `serverId` | UUID | FK → `mcp_servers` |
| `toolName` | string | As reported by the server |
| `description` | string | Tool description |
| `inputSchema` | object | JSON Schema for tool inputs |
| `enabled` | bool | Disabled tools are hidden from agents |
| `createdAt` | timestamp | |

---

## Hosted MCP servers (workspace)

For persistent, containerized MCP servers managed by the workspace system, see [Workspaces — Hosted MCP Servers](workspace.md#hosted-mcp-servers).
