# Sandboxes

Sandboxes are isolated compute environments where AI agents execute code. They support multiple sandbox technologies and can be used for both one-off agent runs (ephemeral) and long-running development sessions (persistent).

## Concepts

| Concept | Description |
|---|---|
| **Sandbox** | An isolated container with optional git checkout |
| **Provider** | The underlying sandbox technology |
| **Lifecycle** | `ephemeral` (auto-cleaned) or `persistent` (manually managed) |
| **Container type** | `agent_sandbox` (for code execution) or `mcp_server` (for hosted MCP) |
| **Hosted MCP server** | A persistent sandbox running an MCP server container |

---

## Providers

| Provider | `provider` value | Technology |
|---|---|---|
| Firecracker | `firecracker` | Firecracker microVMs — highest isolation |
| E2B | `e2b` | E2B cloud sandboxes |
| gVisor | `gvisor` | gVisor container runtime |

---

## Sandbox lifecycle

```
creating → ready → (stopping → stopped)
                ↘ error
```

| Status | Description |
|---|---|
| `creating` | Sandbox is being provisioned |
| `ready` | Sandbox is running and accepting tool calls |
| `stopping` | Graceful shutdown in progress |
| `stopped` | Sandbox is stopped (can be resumed) |
| `error` | Provisioning or runtime error |

---

## Managing sandboxes

### List sandboxes

```bash
curl https://api.dev.emergent-company.ai/api/v1/agent/sandboxes \
  -H "Authorization: Bearer <token>"
```

### List available providers

```bash
curl https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/providers \
  -H "Authorization: Bearer <token>"
```

### Create a sandbox

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes \
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
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/from-snapshot \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "snapshotId": "<snapshot-id>",
    "provider": "firecracker"
  }'
```

### Stop a sandbox

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/stop \
  -H "Authorization: Bearer <token>"
```

### Resume a stopped sandbox

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/resume \
  -H "Authorization: Bearer <token>"
```

### Take a snapshot

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/snapshot \
  -H "Authorization: Bearer <token>"
```

Returns `{"snapshotId": "..."}`. Use with `from-snapshot` to create pre-warmed sandboxes.

### Delete a sandbox

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id> \
  -H "Authorization: Bearer <token>"
```

---

## Tool execution

Once a sandbox is `ready`, you can execute tools inside it. All tool calls are recorded in the audit log.

### Bash

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/bash \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "command": "npm test",
    "timeout": 60000,
    "workdir": "/sandbox"
  }'
```

Response: `{ "stdout": "...", "stderr": "...", "exitCode": 0, "timedOut": false }`

### Read a file

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/read \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "filePath": "/sandbox/src/main.go", "offset": 1, "limit": 100 }'
```

Response: `{ "content": "...", "lineCount": 100, "truncated": false }`

### Write a file

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/write \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "filePath": "/sandbox/output.txt", "content": "Hello, world!" }'
```

### Edit a file (exact string replacement)

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/edit \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "filePath": "/sandbox/config.yaml",
    "oldString": "debug: false",
    "newString": "debug: true",
    "replaceAll": false
  }'
```

Response: `{ "replacements": 1 }`

### Glob

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/glob \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "pattern": "**/*.go", "path": "/sandbox/src" }'
```

Response: `{ "files": ["/sandbox/src/main.go", ...] }`

### Grep

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/grep \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "pattern": "TODO", "path": "/sandbox", "include": "*.go", "maxResults": 50 }'
```

Response: `{ "matches": [{ "file": "...", "line": 42, "content": "// TODO: fix this" }] }`

### Git

```bash
curl -X POST https://api.dev.emergent-company.ai/api/v1/agent/sandboxes/<id>/git \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{ "args": ["status"], "workdir": "/sandbox" }'
```

Response: `{ "stdout": "...", "stderr": "", "exitCode": 0 }`

---

## Tool audit log

Every tool call is recorded in `kb.sandbox_audit_log`:

| Field | Description |
|---|---|
| `sandboxId` | The sandbox where the tool ran |
| `agentSessionId` | The agent session that triggered the call |
| `toolName` | `bash`, `read`, `write`, `edit`, `glob`, `grep`, or `git` |
| `requestPayloadHash` | SHA-256 hash of the request body |
| `responseStatus` | HTTP status of the tool response |
| `durationMs` | Execution time in milliseconds |

Audit records are written asynchronously — tool call latency is not affected.

---

## Sandbox entity reference

**`AgentSandbox`** — table `kb.agent_sandboxes`

| Field | Type | Description |
|---|---|---|
| `id` | UUID | Primary key |
| `agentSessionId` | UUID | Owning agent session (nullable) |
| `containerType` | string | `agent_sandbox` \| `mcp_server` |
| `provider` | string | `firecracker` \| `e2b` \| `gvisor` |
| `providerSandboxId` | string | Provider-assigned ID |
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

You can run persistent MCP server containers inside the sandbox system. These are separate from the project MCP registry — they are containerized processes managed by the sandbox orchestrator.

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

## Sandbox images

Sandbox images are the base Docker images used when creating sandboxes. Global images are available to all projects; project-scoped images are private.

### List images

```bash
curl https://api.dev.emergent-company.ai/api/admin/sandbox-images \
  -H "Authorization: Bearer <token>"
```

### Register a custom image

```bash
curl -X POST https://api.dev.emergent-company.ai/api/admin/sandbox-images \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "node-18-sandbox",
    "dockerRef": "my-registry.example.com/node-18-sandbox:latest",
    "provider": "gvisor",
    "type": "custom"
  }'
```

Image status transitions: `pending → pulling → ready` (or `error` on failure).

### Delete an image

```bash
curl -X DELETE https://api.dev.emergent-company.ai/api/admin/sandbox-images/<id> \
  -H "Authorization: Bearer <token>"
```
