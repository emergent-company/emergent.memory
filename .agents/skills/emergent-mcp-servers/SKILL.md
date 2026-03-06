---
name: emergent-mcp-servers
description: Manage MCP (Model Context Protocol) servers in an Emergent project — register, inspect, sync tools, and delete servers. Use when the user wants to add, configure, or troubleshoot MCP servers connected to their Emergent project.
metadata:
  author: emergent
  version: "1.0"
---

Manage MCP servers connected to an Emergent project using `emergent mcp-servers`.

## Commands

### List servers
```bash
emergent mcp-servers list
emergent mcp-servers list --output json
```

### Get server details
```bash
emergent mcp-servers get <server-id>
```

### Register a new server

**SSE server:**
```bash
emergent mcp-servers create --name "my-server" --type sse --url "http://localhost:8080/sse"
```

**HTTP server:**
```bash
emergent mcp-servers create --name "my-server" --type http --url "http://localhost:8080/mcp"
```

**stdio server (spawned process):**
```bash
emergent mcp-servers create --name "github" --type stdio --command "npx" --args "-y,@modelcontextprotocol/server-github"
emergent mcp-servers create --name "github" --type stdio --command "npx" --args "-y,@modelcontextprotocol/server-github" --env "GITHUB_TOKEN=ghp_xxx"
```

**With env vars:**
```bash
emergent mcp-servers create --name "my-server" --type http --url "http://..." --env "API_KEY=abc123" --env "ENV=prod"
```

### Inspect (test connection + show capabilities)
```bash
emergent mcp-servers inspect <server-id>
```
Returns: connection status, latency, server info, list of tools/prompts/resources.

### Sync tools (refresh tool list from live server)
```bash
emergent mcp-servers sync <server-id>
```

### List tools for a server
```bash
emergent mcp-servers tools <server-id>
```

### Delete a server
```bash
emergent mcp-servers delete <server-id>
```

## Server Types

| Type | When to use | Required flags |
|---|---|---|
| `sse` | Remote server with SSE transport | `--url` |
| `http` | Remote server with HTTP transport | `--url` |
| `stdio` | Local process (spawned by Emergent) | `--command`, optionally `--args`, `--env` |

## Workflow

1. **Adding a new MCP server**: use `create`, then `inspect` to verify connectivity, then `sync` to populate tools
2. **Troubleshooting a server**: use `inspect` to check connection status and see what capabilities it reports
3. **After updating a server's tools**: run `sync` to refresh the cached tool list in Emergent
4. **Finding a server ID**: use `list --output json` and look up by name

## Notes

- `--project-id` global flag selects the project; falls back to config default
- Server IDs are UUIDs — use `list` to find them by name
- `--args` for stdio type is comma-separated: `"arg1,arg2,arg3"`
