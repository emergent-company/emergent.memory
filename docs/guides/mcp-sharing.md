# Sharing Read-Only MCP Access

Give a teammate or AI agent read-only access to your knowledge graph in under a minute — no account required on their end.

---

## How it works

Calling `POST /api/projects/:projectId/mcp/share` generates a project-scoped API token with read-only scopes (`data:read`, `schema:read`, `agents:read`, `projects:read`) and returns pre-formatted config snippets for popular MCP clients. Optionally, it sends an invite email with the token and setup instructions.

The generated token appears in your project's token list and can be revoked at any time.

---

## Step 1 — Generate a share token

```bash
curl -X POST "https://api.dev.emergent-company.ai/api/projects/<project-id>/mcp/share" \
  -H "Authorization: Bearer <your-admin-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Alice read-only access"
  }'
```

**Response:**

```json
{
  "token": "emt_abc123...",
  "mcpUrl": "https://api.dev.emergent-company.ai/api/mcp",
  "projectId": "<project-id>",
  "snippets": {
    "claudeDesktop": "{ \"mcpServers\": { \"memory\": { \"url\": \"...\", \"headers\": { \"X-API-Key\": \"emt_abc123...\" } } } }",
    "cursor": "{ \"mcpServers\": { \"memory\": { \"url\": \"...\", \"headers\": { \"X-API-Key\": \"emt_abc123...\" } } } }"
  }
}
```

> **Save the token now** — it is only returned once.

---

## Step 2 — Send an invite email (optional)

Add `"emails"` to the request body and the server will send each address an email containing the token, MCP endpoint, and setup instructions for Claude Desktop and Cursor.

```bash
curl -X POST "https://api.dev.emergent-company.ai/api/projects/<project-id>/mcp/share" \
  -H "Authorization: Bearer <your-admin-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Alice read-only access",
    "emails": ["alice@example.com"]
  }'
```

The email is sent asynchronously — the API responds immediately with the token.

---

## Step 3 — Configure the MCP client

### Claude Desktop

Open `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows) and add:

```json
{
  "mcpServers": {
    "memory": {
      "url": "https://api.dev.emergent-company.ai/api/mcp",
      "headers": {
        "X-API-Key": "emt_abc123..."
      }
    }
  }
}
```

Restart Claude Desktop. The `memory` server will appear in the tools panel.

---

### Cursor

Open Cursor → Settings → MCP and add:

```json
{
  "mcpServers": {
    "memory": {
      "url": "https://api.dev.emergent-company.ai/api/mcp",
      "headers": {
        "X-API-Key": "emt_abc123..."
      }
    }
  }
}
```

---

### OpenCode

Add to `opencode.json` in your project directory:

```json
{
  "mcp": {
    "memory": {
      "type": "remote",
      "url": "https://api.dev.emergent-company.ai/api/mcp",
      "headers": { "X-API-Key": "emt_abc123..." },
      "enabled": true
    }
  }
}
```

Verify: `opencode mcp list` — should show `memory` as connected.

---

### Any MCP-compatible client

| Setting | Value |
|---|---|
| Transport | HTTP (streamable) |
| URL | `https://api.dev.emergent-company.ai/api/mcp` |
| Auth header | `X-API-Key: emt_abc123...` |
| Protocol version | `2025-11-25` |

---

## What read-only access can do

| Scope | Capability |
|---|---|
| `data:read` | Query entities, relationships, search the graph |
| `schema:read` | Browse entity types and relationship types |
| `agents:read` | View agent definitions and run history |
| `projects:read` | Read project metadata |

Write operations (`entity-create`, `entity-update`, `relationship-create`, etc.) will be rejected with a 403.

---

## Revoking access

From the API:

```bash
# List tokens to find the ID
curl "https://api.dev.emergent-company.ai/api/projects/<project-id>/tokens" \
  -H "Authorization: Bearer <your-admin-token>"

# Revoke by token ID
curl -X DELETE "https://api.dev.emergent-company.ai/api/projects/<project-id>/tokens/<token-id>" \
  -H "Authorization: Bearer <your-admin-token>"
```

The token is invalidated immediately — any agent using it will receive 401 on the next request.

---

## Request reference

`POST /api/projects/:projectId/mcp/share`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | No | Display name for the token. Defaults to `"MCP Read-Only Share — YYYY-MM-DD HH:MM:SS"` |
| `emails` | string[] | No | Email addresses to send the invite to |

**Errors:**

| Status | Code | Cause |
|---|---|---|
| 401 | `unauthorized` | Missing or invalid auth token |
| 403 | `forbidden` | Caller is not a project admin |
| 422 | `invalid_email` | One or more email addresses are malformed |
