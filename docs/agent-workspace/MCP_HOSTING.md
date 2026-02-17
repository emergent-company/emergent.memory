# MCP Server Hosting Guide

This guide covers hosting MCP (Model Context Protocol) servers inside isolated containers using Emergent's agent workspace infrastructure.

## Overview

Emergent can host MCP servers as **persistent daemon containers** — long-running processes that survive agent session boundaries. Each MCP server runs in its own gVisor-sandboxed container with optional stdio bridging for JSON-RPC communication.

Key features:

- **Container isolation** — each MCP server runs in a sandboxed Docker container (gVisor preferred)
- **Stdio bridge** — bidirectional JSON-RPC 2.0 communication via container stdin/stdout
- **Crash recovery** — automatic restart with exponential backoff on crash loops
- **Persistent volumes** — data survives container restarts
- **Auto-start on boot** — registered servers start automatically when the platform restarts

## Architecture

```
┌─────────────────────────────────────────────┐
│  Emergent Server                            │
│  ┌───────────────────────────────────────┐  │
│  │  MCPHostingService                    │  │
│  │  ┌─────────┐  ┌───────────────────┐   │  │
│  │  │ Register │  │ Crash Monitor     │   │  │
│  │  │ Call     │  │ (per container)   │   │  │
│  │  │ Restart  │  │ 5s health check   │   │  │
│  │  │ Remove   │  │ auto-restart      │   │  │
│  │  └─────────┘  └───────────────────┘   │  │
│  │       │                               │  │
│  │  ┌────▼──────────────────────────┐    │  │
│  │  │  StdioBridge (per server)     │    │  │
│  │  │  JSON-RPC 2.0 over stdio     │    │  │
│  │  │  Mutex-serialized calls       │    │  │
│  │  └───────────────────────────────┘    │  │
│  └───────────────────────────────────────┘  │
│       │ Docker API (attach)                 │
├───────┼─────────────────────────────────────┤
│  ┌────▼──────────────────────────────────┐  │
│  │  gVisor Container (runsc runtime)     │  │
│  │  ┌────────────────────────────────┐   │  │
│  │  │  MCP Server Process            │   │  │
│  │  │  stdin ← JSON-RPC requests     │   │  │
│  │  │  stdout → JSON-RPC responses   │   │  │
│  │  └────────────────────────────────┘   │  │
│  │  /data (persistent volume)            │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

## API Reference

All endpoints require authentication and `admin:read` or `admin:write` scopes.

**Base path:** `/api/v1/mcp/hosted`

### Register a Server

```
POST /api/v1/mcp/hosted
```

**Request body:**

```json
{
  "name": "my-mcp-server",
  "image": "my-registry/my-mcp-server:latest",
  "cmd": ["node", "server.js"],
  "stdio_bridge": true,
  "restart_policy": "always",
  "environment": {
    "LOG_LEVEL": "info",
    "DATA_DIR": "/data"
  },
  "volumes": ["/data"],
  "resource_limits": {
    "cpu": "0.5",
    "memory": "512M",
    "disk": "1G"
  }
}
```

| Field             | Type     | Required | Description                                                  |
| ----------------- | -------- | -------- | ------------------------------------------------------------ |
| `name`            | string   | Yes      | Human-readable name for the server                           |
| `image`           | string   | Yes      | Docker image reference                                       |
| `cmd`             | string[] | No       | Override the image's default command                         |
| `stdio_bridge`    | bool     | No       | Enable JSON-RPC communication via stdin/stdout               |
| `restart_policy`  | string   | No       | `"always"` (default), `"on-failure"`, `"never"`              |
| `environment`     | map      | No       | Environment variables passed to the container                |
| `volumes`         | string[] | No       | Persistent mount paths (named volumes created automatically) |
| `resource_limits` | object   | No       | CPU, memory, disk limits (defaults: 0.5 CPU, 512MB, 1GB)     |

**Response:** `201 Created` with `MCPServerStatus` body.

### Call an MCP Method

```
POST /api/v1/mcp/hosted/:id/call
```

Routes a JSON-RPC 2.0 method call through the stdio bridge.

**Request body:**

```json
{
  "method": "tools/list",
  "params": {},
  "timeout_ms": 30000
}
```

| Field        | Type   | Required | Description                                            |
| ------------ | ------ | -------- | ------------------------------------------------------ |
| `method`     | string | Yes      | JSON-RPC method name (e.g. `tools/list`, `tools/call`) |
| `params`     | any    | No       | Method parameters                                      |
| `timeout_ms` | int    | No       | Call timeout in milliseconds (default: 30000)          |

**Response:** `200 OK`

```json
{
  "result": { "tools": [...] },
  "error": null
}
```

### Get Server Status

```
GET /api/v1/mcp/hosted/:id
```

**Response:** `200 OK` with `MCPServerStatus`.

### List All Servers

```
GET /api/v1/mcp/hosted
```

**Response:** `200 OK` with array of `MCPServerStatus`.

### Restart a Server

```
POST /api/v1/mcp/hosted/:id/restart
```

Gracefully restarts the container (SIGTERM → wait → SIGKILL) and re-establishes the stdio bridge. Resets crash loop backoff counters.

**Response:** `200 OK` with `MCPServerStatus`.

### Remove a Server

```
DELETE /api/v1/mcp/hosted/:id
```

Stops the container, removes it, and deletes the workspace record.

**Response:** `204 No Content`.

## Server Status Object

```json
{
  "workspace_id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "my-mcp-server",
  "image": "my-registry/my-mcp-server:latest",
  "status": "ready",
  "provider": "gvisor",
  "stdio_bridge": true,
  "bridge_connected": true,
  "restart_policy": "always",
  "restart_count": 0,
  "last_crash": null,
  "uptime": "2h15m30s",
  "volumes": ["/data"],
  "resource_limits": {
    "cpu": "0.5",
    "memory": "512M",
    "disk": "1G"
  },
  "created_at": "2026-02-17T10:00:00Z",
  "last_used_at": "2026-02-17T12:15:30Z"
}
```

## Writing an MCP Server for Hosting

### Stdio Protocol Requirements

When `stdio_bridge` is enabled, your MCP server must:

1. **Read JSON-RPC 2.0 requests from stdin** — one JSON object per line (newline-delimited)
2. **Write JSON-RPC 2.0 responses to stdout** — one JSON object per line
3. **Not write non-JSON output to stdout** — use stderr for logging

### Example: Minimal Node.js MCP Server

```javascript
// server.js — reads JSON-RPC from stdin, writes responses to stdout
const readline = require('readline');

const rl = readline.createInterface({ input: process.stdin });

const tools = [
  {
    name: 'echo',
    description: 'Echoes the input text',
    inputSchema: {
      type: 'object',
      properties: { text: { type: 'string' } },
      required: ['text'],
    },
  },
];

rl.on('line', (line) => {
  try {
    const request = JSON.parse(line);
    const response = handleRequest(request);
    process.stdout.write(JSON.stringify(response) + '\n');
  } catch (err) {
    process.stderr.write(`Error: ${err.message}\n`);
  }
});

function handleRequest(req) {
  switch (req.method) {
    case 'tools/list':
      return { jsonrpc: '2.0', id: req.id, result: { tools } };

    case 'tools/call':
      const toolName = req.params?.name;
      if (toolName === 'echo') {
        return {
          jsonrpc: '2.0',
          id: req.id,
          result: {
            content: [{ type: 'text', text: req.params.arguments.text }],
          },
        };
      }
      return {
        jsonrpc: '2.0',
        id: req.id,
        error: { code: -32601, message: `Unknown tool: ${toolName}` },
      };

    default:
      return {
        jsonrpc: '2.0',
        id: req.id,
        error: { code: -32601, message: `Method not found: ${req.method}` },
      };
  }
}
```

### Example: Dockerfile

```dockerfile
FROM node:20-slim
WORKDIR /app
COPY server.js .
CMD ["node", "server.js"]
```

### Example: Python MCP Server

```python
#!/usr/bin/env python3
import json
import sys

def handle_request(req):
    method = req.get("method", "")
    req_id = req.get("id")

    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": []}}
    elif method == "tools/call":
        tool_name = req.get("params", {}).get("name", "")
        return {"jsonrpc": "2.0", "id": req_id, "result": {
            "content": [{"type": "text", "text": f"Called {tool_name}"}]
        }}
    else:
        return {"jsonrpc": "2.0", "id": req_id,
                "error": {"code": -32601, "message": f"Method not found: {method}"}}

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        request = json.loads(line)
        response = handle_request(request)
        sys.stdout.write(json.dumps(response) + "\n")
        sys.stdout.flush()
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
```

## Crash Recovery

The hosting service monitors each container every 5 seconds. When a container exits unexpectedly:

### Restart Policies

| Policy       | Behavior                                |
| ------------ | --------------------------------------- |
| `always`     | Always restart, regardless of exit code |
| `on-failure` | Restart only on non-zero exit code      |
| `never`      | Do not restart; mark as stopped         |

### Exponential Backoff

If a server crashes **3 or more times within 60 seconds**, it enters a crash loop state:

1. First backoff: 5 seconds
2. Each subsequent: previous × 3 (15s, 45s, 135s...)
3. Maximum backoff: 5 minutes

Manual restart via the API (`POST /:id/restart`) resets the crash loop counters.

### Graceful Shutdown

On platform shutdown, all MCP servers receive SIGTERM with a 30-second grace period before SIGKILL.

## Configuration

### Environment Variables

| Variable                     | Default  | Description                                          |
| ---------------------------- | -------- | ---------------------------------------------------- |
| `ENABLE_AGENT_WORKSPACES`    | `false`  | Master switch — must be `true` to enable MCP hosting |
| `WORKSPACE_DEFAULT_PROVIDER` | `gvisor` | Default provider (MCP servers always use gVisor)     |
| `WORKSPACE_NETWORK_NAME`     | (none)   | Docker network for container isolation               |

### Resource Defaults for MCP Servers

MCP servers use lighter resource defaults than agent workspaces:

| Resource | MCP Default | Workspace Default |
| -------- | ----------- | ----------------- |
| CPU      | 0.5 cores   | 1.0 core          |
| Memory   | 512 MB      | 2 GB              |
| Disk     | 1 GB        | 10 GB             |

Override per-server via the `resource_limits` field in the registration request.

## Relationship to MCP Registry

Emergent has two MCP-related systems:

| System                                   | Package  | Purpose                                                                                                                                                   |
| ---------------------------------------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **MCP Registry** (`domain/mcpregistry/`) | Existing | Registry of MCP server definitions. Manages connections to external MCP servers via `mcp-go` client (spawns stdio subprocesses on the host).              |
| **MCP Hosting** (`domain/workspace/`)    | New      | Runs MCP servers **inside isolated containers** with crash recovery, resource limits, and persistent volumes. Uses the workspace provider infrastructure. |

The hosting system provides stronger isolation guarantees:

- Sandboxed execution (gVisor runtime)
- Resource limits enforced by cgroups
- Persistent volumes survive crashes and restarts
- Exponential backoff prevents crash loops from consuming resources
- Containers are on an isolated Docker network (when configured)

## Troubleshooting

### Server not starting

1. Check that `ENABLE_AGENT_WORKSPACES=true` is set
2. Verify the Docker image exists and is pullable: `docker pull <image>`
3. Check server logs for container creation errors
4. Verify gVisor runtime is installed: `docker info | grep runsc`

### Stdio bridge not connecting

1. Ensure `stdio_bridge: true` is set in the registration request
2. Verify the MCP server reads from stdin and writes to stdout (not stderr)
3. Check that stdout output is valid JSON-RPC 2.0 (one JSON object per line)
4. Look for "failed to establish stdio bridge" in server logs

### Server in crash loop

1. Check the server status: `GET /api/v1/mcp/hosted/:id`
2. Look at `restart_count` and `last_crash` fields
3. Manual restart resets backoff: `POST /api/v1/mcp/hosted/:id/restart`
4. Check container logs for the root cause (exit code in health check logs)
5. Consider setting `restart_policy: "never"` temporarily for debugging

### Call timeouts

1. Default timeout is 30 seconds — increase via `timeout_ms` in the call request
2. Verify the MCP server is actually processing and responding to stdin
3. Check `bridge_connected` in the server status
4. Large responses may need the buffer size increased (currently 64KB)

### Data persistence

- Volumes declared in the `volumes` field are created as Docker named volumes
- Data persists across container restarts and crashes
- Removing a server (`DELETE /api/v1/mcp/hosted/:id`) destroys the container but volumes require manual cleanup
