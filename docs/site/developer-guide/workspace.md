# Workspaces

Workspaces are isolated compute environments where AI agents execute code. They support multiple sandbox technologies and can be used for both one-off agent runs (ephemeral) and long-running development sessions (persistent).

## Concepts

| Concept | Description |
|---|---|
| **Workspace** | An isolated sandbox container with optional git checkout |
| **Provider** | The underlying sandbox technology |
| **Lifecycle** | `ephemeral` (auto-cleaned) or `persistent` (manually managed) |
| **Container type** | `agent_workspace` (for code execution) or `mcp_server` (for hosted MCP) |
| **Hosted MCP server** | A persistent workspace running an MCP server container |

---

## Providers

| Provider | `provider` value | Technology |
|---|---|---|
| Firecracker | `firecracker` | Firecracker microVMs — highest isolation |
| E2B | `e2b` | E2B cloud sandboxes |
| gVisor | `gvisor` | gVisor container runtime |

---

## Workspace lifecycle

```
creating → ready → (stopping → stopped)
                ↘ error
```

| Status | Description |
|---|---|
| `creating` | Workspace is being provisioned |
| `ready` | Workspace is running and accepting tool calls |
| `stopping` | Graceful shutdown in progress |
| `stopped` | Workspace is stopped (can be resumed) |
| `error` | Provisioning or runtime error |

---

## Managing workspaces

### List workspaces

```bash
curl https://api.dev.emergent-company.ai/api/v1/agent/workspaces \
  -H "Authorization: Bearer <token>"
```

### List available providers

```bash
curl https://api.dev.emergent-company.ai/api/v1/agent/workspaces/providers \
  -H "Authorization: Bearer <token>"
```

### Create a workspace

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "firecracker",
    "lifecycle": "ephemeral",
    "repositoryUrl": "https://github.com/my-org/my-repo",
    "branch": "main",
    "resourceLimits": {
      "cpu": 2,
      "memoryMb": 2048,
      "diskGb": 10
    },
    "metadata": {
      "purpose": "code-review"
    }
  }'
```

### Create from snapshot

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/from-snapshot \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "snapshotId": "<snapshot-id>",
    "provider": "firecracker"
  }'
```

### Stop a workspace

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/stop \
  -H "Authorization: Bearer <token>"
```

### Resume a stopped workspace

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/resume \
  -H "Authorization: Bearer <token>"
```

### Take a snapshot

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/snapshot \
  -H "Authorization: Bearer <token>"
```

Returns `{"snapshotId": "..."}`. Use with `from-snapshot` to create pre-warmed workspaces.

### Delete a workspace

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id> \
  -H "Authorization: Bearer <token>"
```

---

## Tool execution

Once a workspace is `ready`, you can execute tools inside it. All tool calls are recorded in the audit log.

### Bash

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/bash \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "command": "npm test",
    "timeout": 60000,
    "workdir": "/workspace"
  }'
```

Response: `{ "stdout": "...", "stderr": "...", "exitCode": 0, "timedOut": false }`

### Read a file

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/read \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "filePath": "/workspace/src/main.go", "offset": 1, "limit": 100 }'
```

Response: `{ "content": "...", "lineCount": 100, "truncated": false }`

### Write a file

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/write \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "filePath": "/workspace/output.txt", "content": "Hello, world!" }'
```

### Edit a file (exact string replacement)

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/edit \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "filePath": "/workspace/config.yaml",
    "oldString": "debug: false",
    "newString": "debug: true",
    "replaceAll": false
  }'
```

Response: `{ "replacements": 1 }`

### Glob

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/glob \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "pattern": "**/*.go", "path": "/workspace/src" }'
```

Response: `{ "files": ["/workspace/src/main.go", ...] }`

### Grep

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/grep \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "pattern": "TODO", "path": "/workspace", "include": "*.go", "maxResults": 50 }'
```

Response: `{ "matches": [{ "file": "...", "line": 42, "content": "// TODO: fix this" }] }`

### Git

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/workspaces/<id>/git \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "args": ["status"], "workdir": "/workspace" }'
```

Response: `{ "stdout": "...", "stderr": "", "exitCode": 0 }`

---

## Tool audit log

Every tool call is recorded in `kb.workspace_audit_log`:

| Field | Description |
|---|---|
| `workspaceId` | The workspace where the tool ran |
| `agentSessionId` | The agent session that triggered the call |
| `toolName` | `bash`, `read`, `write`, `edit`, `glob`, `grep`, or `git` |
| `requestPayloadHash` | SHA-256 hash of the request body |
| `responseStatus` | HTTP status of the tool response |
| `durationMs` | Execution time in milliseconds |

Audit records are written asynchronously — tool call latency is not affected.

---

## Workspace entity reference

**`AgentWorkspace`** — table `kb.agent_workspaces`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `agentSessionId` | UUID | Owning agent session (nullable) |
| `containerType` | string | `agent_workspace` \| `mcp_server` |
| `provider` | string | `firecracker` \| `e2b` \| `gvisor` |
| `providerWorkspaceId` | string | Provider-assigned ID |
| `repositoryUrl` | string | Git repo to check out |
| `branch` | string | Git branch |
| `deploymentMode` | string | `managed` \| `self-hosted` |
| `lifecycle` | string | `ephemeral` \| `persistent` |
| `status` | string | See lifecycle diagram above |
| `resourceLimits` | object | CPU, memory, disk constraints |
| `snapshotId` | string | ID of last snapshot (nullable) |
| `mcpConfig` | object | MCP config (for `mcp_server` type) |
| `metadata` | object | Free-form key-value metadata |
| `createdAt` | timestamp | |
| `lastUsedAt` | timestamp | Updated on every tool call |
| `expiresAt` | timestamp | Used by cleanup worker (nullable) |

---

## Hosted MCP servers

You can run persistent MCP server containers inside the workspace system. These are separate from the project MCP registry — they are containerized processes managed by the workspace orchestrator.

### List hosted servers

```bash
curl https://api.dev.emergent-company.ai/api/v1/mcp/hosted \
  -H "Authorization: Bearer <token>"
```

### Register a hosted server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/mcp/hosted \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-server",
    "imageRef": "my-registry.example.com/my-mcp-server:latest",
    "command": ["node", "server.js"],
    "env": { "NODE_ENV": "production" },
    "mcpConfig": { "tools": ["my_tool"] }
  }'
```

### Call a hosted server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/mcp/hosted/<id>/call \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "method": "tools/call",
    "params": { "name": "my_tool", "arguments": {} }
  }'
```

### Restart a hosted server

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/mcp/hosted/<id>/restart \
  -H "Authorization: Bearer <token>"
```

### Delete a hosted server

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/v1/mcp/hosted/<id> \
  -H "Authorization: Bearer <token>"
```

---

## Workspace images

Workspace images are the base Docker images used when creating workspaces. Global images are available to all projects; project-scoped images are private.

### List images

```bash
curl https://api.dev.emergent-company.ai/api/admin/workspace-images \
  -H "Authorization: Bearer <token>"
```

### Register a custom image

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/workspace-images \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "node-18-workspace",
    "dockerRef": "my-registry.example.com/node-18-workspace:latest",
    "provider": "gvisor",
    "type": "custom"
  }'
```

Image status transitions: `pending → pulling → ready` (or `error` on failure).

### Delete an image

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/admin/workspace-images/<id> \
  -H "Authorization: Bearer <token>"
```
