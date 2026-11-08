# MCP Servers

Memory integrates with the [Model Context Protocol](https://modelcontextprotocol.io) in two ways:

1. **Built-in MCP server** — Memory itself is an MCP server. Clients like Claude Desktop or Cursor connect directly to Memory to read/write the knowledge graph.
2. **MCP registry** — Per-project registry of *external* MCP servers that agents can call as tools.

---

## Built-in Memory MCP server

Memory is a **remote** MCP server: there is no local stdio subcommand to run.
The current spec transport is Streamable HTTP at `/api/mcp`; the project-scoped
SSE endpoints remain available for clients that speak only SSE.

| Transport | Endpoint | Authentication |
|---|---|---|
| Streamable HTTP (spec 2025-11-25, recommended) | `/api/mcp` — `POST` for JSON-RPC, `GET` for the server-initiated stream, `DELETE` to end the session | `X-API-Key: <api_token>` + `X-Project-ID: <project_id>`, or `Authorization: Bearer <api_token>` |
| SSE (legacy, project-scoped) | `GET /api/mcp/sse/<projectId>`, `POST /api/mcp/sse/<projectId>/message` | `X-API-Key`, `Authorization: Bearer`, or `?token=<api_token>` |
| JSON-RPC over HTTP (legacy) | `POST /api/mcp/rpc` | same token, as `X-API-Key` or `Authorization: Bearer` |

Send the project id as the `X-Project-ID` header on `/api/mcp`; the SSE routes
carry it in the path instead.

> **Tip:** `memory mcp-guide` prints these snippets for your server URL, with the
> token redacted to `${MEMORY_PROJECT_TOKEN}` (pass `--show-secret` to inline the
> real value). It uses your **first** project, so check the `X-Project-ID` value
> before pasting if you belong to more than one.

### Connecting Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "memory": {
      "type": "http",
      "url": "https://api.example.com/api/mcp",
      "headers": {
        "X-Project-ID": "<project-id>",
        "X-API-Key": "<your-api-token>"
      }
    }
  }
}
```

Clients that only implement the older SSE transport use the project-scoped
endpoint instead — the project id is part of the URL there:

```json
{
  "mcpServers": {
    "memory": {
      "type": "sse",
      "url": "https://api.example.com/api/mcp/sse/<project-id>",
      "headers": {
        "X-API-Key": "<your-api-token>"
      }
    }
  }
}
```

A client that can only speak `Authorization: Bearer` works too: the same token
is accepted in that header. Clients that only support stdio need their own
documented bridge to a remote server — Memory does not ship a stdio mode.

### Connecting OpenCode

Add to `opencode.json` in your project directory:

```json
{
  "mcp": {
    "memory": {
      "type": "remote",
      "url": "https://api.example.com/api/mcp",
      "headers": {
        "X-Project-ID": "<project-id>",
        "X-API-Key": "<your-api-token>"
      },
      "enabled": true
    }
  }
}
```

Replace `<project-id>` with your project UUID (`memory projects list`) and
`<your-api-token>` with a project-scoped API token. Run `opencode mcp list` to
verify the connection.

### Available MCP tools (built-in)

The built-in server exposes tools including:

- **Knowledge graph** — create, update, search objects and relationships
- **Documents** — upload, list, retrieve documents
- **Search** — unified vector + keyword search
- **Chat** — create sessions, send messages
- **Agents** — list agents, trigger runs
- **MCP registry** — `search_mcp_registry` and `mcp-registry-get`. Installing a
  registry entry (`mcp-registry-install`) requires a platform-admin token, since
  it opens an outbound connection. A project's own servers are administered over
  REST, not through client tools — see
  [Managing external servers](#managing-external-servers)

> See `apps/server/domain/mcp/README.md` for the complete tools, resources, and prompts reference.

### Sending feedback about Memory

The built-in server exposes **`feedback-submit`**, and the same submission is
available over REST as `POST /api/feedback`. Both accept the credential you
already use and file the report as a GitHub issue, with your account, project and
basic environment details attached automatically — you never need to paste a
token or your email.

There is no strict template. Write what you were trying to do, what happened,
and what you expected. If you can, add the steps that reproduce it and the exact
error text; one or two sentences is fine when that is all you have.

```json
{
  "message": "The entity search tool returns stale results for a few minutes after an update.",
  "category": "bug",
  "severity": "medium",
  "include_context": true
}
```

`category` (`bug`, `feature`, `usability`, `docs`, `other`) and `severity`
(`low`, `medium`, `high`) are optional. `include_context` defaults to `true`;
set it to `false` to send only your message and account identity. A caller is
limited to a handful of submissions per hour, and an immediate retry of the same
text returns the issue created by the first call instead of filing a duplicate.

A deployment that has no feedback tracker configured answers `503` and sends
nothing anywhere — the endpoint is present, the destination is not.

### Sharing a scoped key with someone else

To hand an outside client — a teammate's agent, a partner, a demo — access to
one project without giving it your own token, create a **share**: a named key
bound to the exact tools you choose.

**In the web UI:** Settings → MCP Servers → **Sharing** →
**New share**. Name the share, tick the tools it may call, and optionally enter
one or more email addresses under **Email the key**. Each recipient receives the
endpoint, the key, and ready-to-paste client configs; the sender is named in the
email.

**Presets.** The create form offers a preset row that ticks a curated tool set in
one click — it replaces the current selection, and you can adjust the ticks
afterwards:

| Preset | What it grants |
|---|---|
| **Read-only** (default) | Search, graph reads, document and journal reads, schema reads, skill reads, and agent/run reads. No MCP tool in this set writes. |
| **Knowledge writer** | Read-only plus creating, editing, and deleting objects, relationships, documents, and journal notes. |
| **Schema & blueprints** | Graph reads, then schema read/write, schema migrations, and blueprints. |
| **Agent observer** | Agent, run, question, and session reads, plus search and graph reads. |
| **Documents** | Upload, list, retrieve, and delete documents, plus document classification, keyword search, and semantic search. |
| **Full access** | Every tool a share key can carry. |

A share key is also an ordinary project API token, so the scopes behind the
selected tools are honoured by the REST API as well as the MCP tools. Presets
keep that set as narrow as the tools allow — the Documents preset, for
example, grants document read/write and search but no knowledge or chat
writes.

Presets never exceed what a share may grant: platform administration, project
settings, credential minting, and agent execution/management are not shareable,
so **Full access** is the ceiling rather than "admin". Each preset's tool count
is resolved against your project's catalog when the page loads, so it always
matches the tools the backend currently offers.

The same data is available to API clients: `GET /api/projects/{projectId}/mcp/tools`
returns `tools` plus a `templates` array (id, label, description, `tools`,
`toolCount`, and `default` on the preselected one). Creating a share still sends
an explicit `tools` list — a template is a UI convenience, not a new request
field. The list is required: a request with no `tools` (or an empty array) is
rejected with HTTP 422. Shares created before the allowlist existed, and the
legacy `POST /api/projects/{projectId}/mcp/share` endpoint, keep an unrestricted
allowlist and remain read-only through their token scopes.

**Copy an existing share.** To start a new share from one that already works,
open its **⋯** menu → **Duplicate share**. The create form opens pre-filled with
that share's description and tool selection, under a free name (`<name> (copy)`,
or `(copy 2)` and so on when that name is taken); adjust anything and save. The
copy is an ordinary new share with its own key — the source share, its key, and
its invite recipients are untouched. Legacy shares can be copied this way too,
which is how you move off one, even though they cannot be edited.

The key is shown to *you* exactly once, on the creation screen. If you also
entered recipients, they have their own copy; otherwise copy it before closing
the dialog.

To send the key to someone **after** the share exists, open the share's **⋯**
menu → **Invite by email**. That emails the *current* key and the same
ready-to-paste client configs, so anyone already using the share keeps working.
**Issue a new key** in the same menu is the destructive alternative: the old key
stops working immediately and its dialog offers the same optional email field.
A share whose key was stored before the deployment enabled key encryption cannot
be re-sent — the invite dialog says so and offers **Issue a new key instead**.

**Who has the key, and what it did.** A share records the addresses its key was
emailed to: the Sharing list shows them (hover the chip for the full list) and
marks a share **read & write** when its tools can modify the graph. The invite
email says the same thing, so a recipient is never told a writable key is
read-only. Choose **View writes** on a share to open the object browser filtered
to the objects that share's key created or last updated. The filter follows the
share's *current* key, so after **Issue a new key** it starts from that rotation
instead of covering the retired key.

**From the CLI:** `memory projects share-mcp --project my-project
--email alice@example.com` does the same through the legacy share endpoint and
mints one key per recipient.

Sharing only ever narrows access: a share token carries just the scopes its
selected tools require, so it cannot reach project administration or other
projects.

### Brave Search integration

The built-in server includes a `brave_web_search` tool backed by the Brave Search API. The API key can be set globally (via environment variable) or overridden per-org or per-project — see [Built-in tool configuration](#built-in-tool-configuration) below.

---

## Built-in tool configuration

Built-in tools can be enabled/disabled and configured at three levels. The most specific level wins:

```
project setting  →  org default  →  global env var
```

If a project has no explicit setting, it inherits from the org default. If neither is set, the global environment variable is used.

### Available built-in tools

| Tool | Env var (global fallback) | Config field |
|---|---|---|
| `brave_web_search` | `BRAVE_SEARCH_API_KEY` | `api_key` |

### Project-level configuration

List the builtin server and its tools for your project:

```bash
# 1. Find the builtin server ID (auto-created on first list)
curl "https://api.example.com/api/admin/mcp-servers?projectId=<projectId>" \
  -H "Authorization: Bearer <token>"
# → find the entry with "type": "builtin"

# 2. List its tools (includes inheritedFrom field)
curl https://api.example.com/api/admin/mcp-servers/<builtinServerId>/tools \
  -H "Authorization: Bearer <token>"
```

Each tool in the response includes an `inheritedFrom` field: `"project"`, `"org"`, or `"global"`, indicating where the current effective setting comes from.

**Enable/disable a built-in tool at project level:**

```bash
curl -X PATCH "https://api.example.com/api/admin/mcp-servers/<builtinServerId>/tools/<toolId>" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

**Set a per-project API key for `brave_web_search`:**

```bash
curl -X PATCH "https://api.example.com/api/admin/mcp-servers/<builtinServerId>/tools/<toolId>" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "config": { "api_key": "BSA..." }
  }'
```

Once set, this project's agent runs use this API key instead of the global `BRAVE_SEARCH_API_KEY`.

### Org-level defaults

Org defaults apply to all projects in the org that have no project-level override.

**List current org defaults:**

```bash
curl https://api.example.com/api/admin/orgs/<orgId>/tool-settings \
  -H "Authorization: Bearer <token>"
```

**Set an org default (enable + API key):**

```bash
curl -X PUT "https://api.example.com/api/admin/orgs/<orgId>/tool-settings/brave_web_search" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "config": { "api_key": "BSA..." }
  }'
```

**Disable `brave_web_search` for all projects in the org by default:**

```bash
curl -X PUT "https://api.example.com/api/admin/orgs/<orgId>/tool-settings/brave_web_search" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

**Remove an org default** (projects fall back to the global env var):

```bash
curl -X DELETE "https://api.example.com/api/admin/orgs/<orgId>/tool-settings/brave_web_search" \
  -H "Authorization: Bearer <token>"
```

### Inheritance example

Suppose you want `brave_web_search` enabled for most projects but one project should use a different API key:

```
Global env: BRAVE_SEARCH_API_KEY=BSA-global-key

Org default: enabled=true, api_key=BSA-org-key      ← applies to all projects by default
  Project A: (no override)                          → uses org key (inheritedFrom: "org")
  Project B: api_key=BSA-project-b-key             → uses project B key (inheritedFrom: "project")
  Project C: enabled=false                          → tool disabled for this project
```

### UI

The **MCP Integration** settings page (Project Settings → Integrations → MCP Integration) has a Built-in Tools section where you can toggle tools and set config fields for the current project.

The **Tool Defaults** page (Project Settings → Organization → Tool Defaults) lets org admins set org-wide defaults for all built-in tools.

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
curl https://api.example.com/api/admin/mcp-servers \
  -H "Authorization: Bearer <token>"
```

### Register a stdio server

```bash
curl -X POST https://api.example.com/api/admin/mcp-servers \
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
curl -X POST https://api.example.com/api/admin/mcp-servers \
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
curl -X POST https://api.example.com/api/admin/mcp-servers \
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
curl -X POST https://api.example.com/api/admin/mcp-servers/<serverId>/inspect \
  -H "Authorization: Bearer <token>"
```

Returns the tool list from the live server without persisting anything.

### Sync tool catalog

```bash
curl -X POST https://api.example.com/api/admin/mcp-servers/<serverId>/sync \
  -H "Authorization: Bearer <token>"
```

Connects to the server, calls `tools/list`, and upserts results into the local tool catalog. Safe to call repeatedly (idempotent).

### List synced tools

```bash
curl https://api.example.com/api/admin/mcp-servers/<serverId>/tools \
  -H "Authorization: Bearer <token>"
```

### Enable / disable a tool

```bash
curl -X PATCH https://api.example.com/api/admin/mcp-servers/<serverId>/tools/<toolId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

Disabled tools are filtered from the agent context.

### Update a server

```bash
curl -X PATCH https://api.example.com/api/admin/mcp-servers/<serverId> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Delete a server

```bash
curl -X DELETE https://api.example.com/api/admin/mcp-servers/<serverId> \
  -H "Authorization: Bearer <token>"
```

---

## Official MCP registry

Browse and install servers from the official MCP registry at `registry.modelcontextprotocol.io`.

### Search the registry

```bash
curl "https://api.example.com/api/admin/mcp-registry/search?q=github" \
  -H "Authorization: Bearer <token>"
```

### Get details for a registry server

```bash
curl https://api.example.com/api/admin/mcp-registry/servers/github \
  -H "Authorization: Bearer <token>"
```

### Install from the registry

```bash
curl -X POST https://api.example.com/api/admin/mcp-registry/install \
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

For persistent, containerized MCP servers managed by the workspace system, contact your platform administrator.
