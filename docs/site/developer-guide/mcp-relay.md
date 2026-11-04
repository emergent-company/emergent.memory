# MCP Relay

The MCP relay lets a remote process (e.g. an AI agent running on a separate machine) expose its local MCP tools to a Memory project via a persistent WebSocket connection. Memory acts as the relay hub — the agent registers its tools once, and any caller (another agent, the CLI, or a direct API call) can invoke those tools through Memory without needing direct network access to the remote process.

---

## How it works

```
Remote agent  ──WebSocket──►  Memory relay hub  ◄──REST──  Caller (CLI / agent)
  (provider)                  /api/mcp-relay/               (consumer)
```

1. The remote agent opens a WebSocket to `/api/mcp-relay/connect?projectId=<id>`.
2. It sends a **register frame** advertising its instance ID and tool list.
3. Memory stores the live session in memory and exposes it via REST.
4. Any caller can list sessions, inspect tools, and forward tool calls — Memory proxies the request over the WebSocket and returns the response.

---

## WebSocket protocol

### Connection

```
GET /api/mcp-relay/connect?projectId=<projectId>
Authorization: Bearer <api_token>
Upgrade: websocket
```

### Frame types

| Frame | Direction | Description |
|---|---|---|
| `register` | provider → hub | First frame after connecting. Advertises instance ID and tools. |
| `registered` | hub → provider | Acknowledgement after successful registration. |
| `call` | hub → provider | Hub forwards a tool invocation. |
| `response` | provider → hub | Provider returns the tool result. |
| `ping` | provider → hub | Keepalive ping. |
| `pong` | hub → provider | Keepalive reply. |
| `error` | hub → provider | Protocol error (e.g. malformed register frame). |

### Register frame

```json
{
  "type": "register",
  "instance_id": "my-agent-v1",
  "version": "1.0.0",
  "tools": {
    "tools": [
      {
        "name": "search_codebase",
        "description": "Search the local codebase",
        "inputSchema": { "type": "object", "properties": {} }
      }
    ]
  }
}
```

`instance_id` must be unique per project. If a session with the same ID is already connected it will be replaced when the new connection registers.

---

## REST API

All REST endpoints require `Authorization: Bearer <api_token>` and a `projectId` query parameter (or project context from a project-scoped token).

### List sessions

```
GET /api/mcp-relay/sessions?projectId=<id>
```

Returns all currently connected relay instances for the project.

```json
{
  "sessions": [
    {
      "instance_id": "my-agent-v1",
      "version": "1.0.0",
      "tool_count": 3,
      "connected_at": "2025-04-26T10:00:00Z"
    }
  ]
}
```

### Get tools for an instance

```
GET /api/mcp-relay/sessions/{instanceId}/tools?projectId=<id>
```

Returns the raw `tools/list` payload advertised by the relay instance.

### Call a tool

```
POST /api/mcp-relay/sessions/{instanceId}/call?projectId=<id>
Content-Type: application/json

{
  "name": "search_codebase",
  "arguments": { "query": "auth middleware" }
}
```

Memory forwards the call over the WebSocket and returns the result once the provider responds (default timeout: 30 s).

---

## CLI

```bash
# List connected relay instances
memory mcp-relay sessions

# Show tools for a specific instance
memory mcp-relay tools <instance-id>

# Call a tool on a relay instance
memory mcp-relay call <instance-id> <tool-name>
memory mcp-relay call <instance-id> <tool-name> --args '{"query":"auth"}'
```

All commands respect the standard `--project` / `MEMORY_PROJECT_ID` context.

---

## Implementing a relay provider

A minimal relay provider in pseudocode:

```python
import websocket, json

ws = websocket.connect(
    f"{MEMORY_URL}/api/mcp-relay/connect?projectId={PROJECT_ID}",
    headers={"Authorization": f"Bearer {API_TOKEN}"}
)

# 1. Register
ws.send(json.dumps({
    "type": "register",
    "instance_id": "my-agent",
    "tools": {"tools": [{"name": "ping", "description": "Ping", "inputSchema": {}}]}
}))

ack = json.loads(ws.recv())  # {"type":"registered","instance_id":"my-agent"}

# 2. Handle incoming calls
while True:
    msg = json.loads(ws.recv())
    if msg["type"] == "call":
        result = handle_tool(msg["params"]["name"], msg["params"].get("arguments", {}))
        ws.send(json.dumps({
            "type": "response",
            "id": msg["id"],
            "result": result
        }))
    elif msg["type"] == "ping":
        ws.send(json.dumps({"type": "pong"}))
```

---

## Notes

- Sessions are **in-memory only** — a server restart clears all relay sessions.
- The relay hub enforces a **30-second call timeout**. Providers must respond within that window.
- The WebSocket connection is kept alive via ping/pong. Providers should send a `ping` frame at least every 60 seconds to avoid the idle timeout.
