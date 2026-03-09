# Domain Guide — Emergent Memory Go Server

This guide documents all server domain packages under `apps/server/domain/`. It covers HTTP routes, key entities, enums/constants, and service/subsystem notes for each domain.

**Related references (not duplicated here):**
- `apps/server/AGENT.md` — framework patterns (fx, Echo, Bun ORM, job queues, test commands)
- `apps/server/domain/mcp/README.md` — full MCP tools, resources, and prompts reference
- `apps/server/domain/documents/UPLOAD_API.md` — document upload API

---

## Table of Contents

1. [health](#health)
2. [provider](#provider)
3. [typeregistry](#typeregistry)
4. [embeddingpolicies](#embeddingpolicies)
5. [templatepacks](#templatepacks)
6. [workspace](#workspace)
7. [workspaceimages](#workspaceimages)
8. [mcp](#mcp)
9. [mcpregistry](#mcpregistry)
10. [extraction](#extraction)
11. [scheduler](#scheduler)

---

## health

### Overview

Exposes health-check and diagnostics endpoints. No authentication required. Used by load balancers, Kubernetes probes, and on-call engineers. The `/api/diagnostics` endpoint is particularly useful for live DB pool inspection and slow-query detection.

### HTTP Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/health` | `Health` | none |
| GET | `/healthz` | `Healthz` | none |
| GET | `/ready` | `Ready` | none |
| GET | `/debug` | `Debug` | none (dev only) |
| GET | `/api/diagnostics` | `Diagnostics` | none |

### Response Shapes

**`/health`** → `HealthResponse`
```json
{
  "status": "healthy",
  "timestamp": "2026-01-01T00:00:00Z",
  "uptime": "3h12m5s",
  "version": "1.2.3",
  "checks": {
    "database": "healthy"
  }
}
```

**`/healthz`** → plain text `OK` (200) or `Service Unavailable` (503)

**`/ready`** → `{ "status": "ready" }` or `{ "status": "not_ready", "message": "..." }`

**`/debug`** (dev-only) → Go runtime memory stats + DB pool stats as JSON

**`/api/diagnostics`** → comprehensive object:
- `db_pool` — Bun connection pool stats
- `pg_stat_activity` — active Postgres sessions
- `long_queries` — queries running > 5 s
- `settings` — relevant pg settings
- `table_sizes` — top tables by size

---

## provider

### Overview

Manages LLM provider credentials and tracks token usage and cost. Credentials (API keys, service account JSON) are AES-GCM encrypted at rest and never returned via API. Supports two scopes: **org-level** (default fallback) and **project-level** (override). Currently supported providers: `google-ai` and `vertex-ai`.

Usage events are written asynchronously via a 1024-event buffered channel — callers never block on accounting. Cost estimation uses org-configured custom rates if present, otherwise falls back to global retail rates stored per 1M tokens.

### HTTP Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| PUT | `/api/v1/organizations/:orgId/providers/:provider` | `UpsertOrgProviderConfig` | authenticated |
| GET | `/api/v1/organizations/:orgId/providers/:provider` | `GetOrgProviderConfig` | authenticated |
| DELETE | `/api/v1/organizations/:orgId/providers/:provider` | `DeleteOrgProviderConfig` | authenticated |
| GET | `/api/v1/organizations/:orgId/providers` | `ListOrgProviderConfigs` | authenticated |
| GET | `/api/v1/organizations/:orgId/usage` | `GetOrgUsage` | authenticated |
| PUT | `/api/v1/projects/:projectId/providers/:provider` | `UpsertProjectProviderConfig` | authenticated |
| GET | `/api/v1/projects/:projectId/providers/:provider` | `GetProjectProviderConfig` | authenticated |
| DELETE | `/api/v1/projects/:projectId/providers/:provider` | `DeleteProjectProviderConfig` | authenticated |
| GET | `/api/v1/projects/:projectId/usage` | `GetProjectUsage` | authenticated |
| GET | `/api/v1/providers/:provider/models` | `ListModels` | authenticated |
| POST | `/api/v1/providers/:provider/test` | `TestProvider` | authenticated |

### Key Entities

**`OrgProviderConfig`** — table `kb.org_provider_configs`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `org_id` | UUID | FK → org |
| `provider` | ProviderType | `"google-ai"` \| `"vertex-ai"` |
| `encrypted_credential` | `[]byte` | AES-GCM encrypted; never returned in API |
| `encryption_nonce` | `[]byte` | AES-GCM nonce |
| `gcp_project` | string | Vertex AI only |
| `location` | string | Vertex AI only |
| `generative_model` | string | model name override |
| `embedding_model` | string | model name override |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

**`ProjectProviderConfig`** — table `kb.project_provider_configs` — same fields, `project_id` instead of `org_id`.

**`LLMUsageEvent`** — table `kb.llm_usage_events`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | nullable |
| `org_id` | UUID | |
| `provider` | ProviderType | |
| `model` | string | |
| `operation` | OperationType | |
| `text_input_tokens` | int64 | |
| `image_input_tokens` | int64 | |
| `video_input_tokens` | int64 | |
| `audio_input_tokens` | int64 | |
| `output_tokens` | int64 | |
| `estimated_cost_usd` | float64 | |
| `created_at` | time.Time | |

### Enums / Constants

```go
type ProviderType string
const (
    ProviderTypeGoogleAI  ProviderType = "google-ai"
    ProviderTypeVertexAI  ProviderType = "vertex-ai"
)

type ModelType string
const (
    ModelTypeEmbedding  ModelType = "embedding"
    ModelTypeGenerative ModelType = "generative"
)

type OperationType string
const (
    OperationTypeGenerate OperationType = "generate"
    OperationTypeEmbed    OperationType = "embed"
)
```

### Request / Response DTOs

**`UpsertProviderConfigRequest`**
```json
{
  "apiKey": "...",
  "serviceAccountJson": "...",
  "gcpProject": "my-gcp-project",
  "location": "us-central1",
  "generativeModel": "gemini-2.0-flash",
  "embeddingModel": "text-embedding-004"
}
```

**`ProviderConfigResponse`** — all `OrgProviderConfig` fields except credential bytes; includes a `configured: true/false` flag.

### Service Notes

- `UsageService.Track(event LLMUsageEvent)` — non-blocking; drops events if buffer full (logged as warning)
- `UsageService` starts a background goroutine on app start (via fx `OnStart` lifecycle hook) that batches events and bulk-inserts every 5 s or when buffer reaches 100 events
- `ProviderRegistry` resolves the active model config for a project by merging project-level config over org-level config
- `catalog.go` — static map of provider → model → pricing (retail rates per 1M tokens)

---

## typeregistry

### Overview

Manages per-project graph node and edge type definitions. Types describe the schema of objects that can be stored in the knowledge graph. Each type carries a JSON Schema, UI config, and extraction config. Types can originate from a template pack (`template`), be hand-crafted (`custom`), or be discovered automatically by the extraction pipeline (`discovered`).

### HTTP Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/api/type-registry/projects/:projectId` | `ListTypes` | authenticated |
| GET | `/api/type-registry/projects/:projectId/types/:typeName` | `GetType` | authenticated |
| GET | `/api/type-registry/projects/:projectId/stats` | `GetStats` | authenticated |
| POST | `/api/type-registry/projects/:projectId/types` | `CreateType` | authenticated |
| PUT | `/api/type-registry/projects/:projectId/types/:typeName` | `UpdateType` | authenticated |
| DELETE | `/api/type-registry/projects/:projectId/types/:typeName` | `DeleteType` | authenticated |

### Key Entity

**`ProjectObjectTypeRegistry`** — table `kb.project_object_type_registry`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | FK → project |
| `type_name` | string | unique per project |
| `source` | TypeSource | `"template"` \| `"custom"` \| `"discovered"` |
| `template_pack_id` | UUID | nullable; set when `source = "template"` |
| `schema_version` | int | incremented on schema changes |
| `json_schema` | jsonb | JSON Schema for objects of this type |
| `ui_config` | jsonb | display hints for the admin UI |
| `extraction_config` | jsonb | hints for the extraction pipeline |
| `enabled` | bool | disabled types are skipped during extraction |
| `discovery_confidence` | float64 | set when `source = "discovered"` |
| `description` | string | human-readable description |
| `created_by` | UUID | user ID |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

### Enums / Constants

```go
type TypeSource string
const (
    TypeSourceTemplate   TypeSource = "template"
    TypeSourceCustom     TypeSource = "custom"
    TypeSourceDiscovered TypeSource = "discovered"
)
```

### Request / Response DTOs

**`CreateTypeRequest`** / **`UpdateTypeRequest`**: `typeName`, `jsonSchema`, `uiConfig`, `extractionConfig`, `description`, `enabled`

**`TypeStatsResponse`**: per-project counts of total / enabled / by-source types.

---

## embeddingpolicies

### Overview

Controls which graph object types receive vector embeddings and under what conditions. Each policy is scoped to a project and an `object_type`. Policies can restrict embedding to objects that carry specific labels, exclude objects with certain statuses, and limit the size of text passed to the embedding model. The extraction pipeline checks these policies before dispatching embedding jobs.

### HTTP Routes

| Method | Path | Handler | Auth / Scope |
|--------|------|---------|--------------|
| GET | `/api/graph/embedding-policies` | `ListEmbeddingPolicies` | `graph:read` |
| GET | `/api/graph/embedding-policies/:id` | `GetEmbeddingPolicy` | `graph:read` |
| POST | `/api/graph/embedding-policies` | `CreateEmbeddingPolicy` | `graph:write` |
| PATCH | `/api/graph/embedding-policies/:id` | `UpdateEmbeddingPolicy` | `graph:write` |
| DELETE | `/api/graph/embedding-policies/:id` | `DeleteEmbeddingPolicy` | `graph:write` |

The `project_id` is extracted from the authenticated request context (not from a URL parameter) for list/create operations. Individual operations scope to the policy's own `project_id`.

### Key Entity

**`EmbeddingPolicy`** — table `kb.embedding_policies`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | FK → project |
| `object_type` | string | matches a type in `typeregistry` |
| `enabled` | bool | master on/off switch |
| `max_property_size` | int | max bytes of any single property sent for embedding |
| `required_labels` | text[] | object must have ALL of these labels |
| `excluded_labels` | text[] | object must have NONE of these labels |
| `relevant_paths` | text[] | JSON paths to include in the embedding text |
| `excluded_statuses` | text[] | object status values that skip embedding |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

### Request / Response DTOs

**`CreateEmbeddingPolicyRequest`**: `objectType`, `enabled`, `maxPropertySize`, `requiredLabels`, `excludedLabels`, `relevantPaths`, `excludedStatuses`

**`UpdateEmbeddingPolicyRequest`**: same fields, all optional (PATCH semantics).

**`EmbeddingPolicyResponse`**: mirrors the entity, field names in camelCase.

---

## templatepacks

### Overview

Template packs are versioned bundles of object type schemas, relationship type schemas, UI configs, and extraction prompts. They form a global catalog from which packs can be assigned to projects. Once assigned, a project can activate or deactivate individual packs. A helper endpoint (`compiled-types`) returns the merged type registry view for a project, combining all active packs.

### HTTP Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| POST | `/api/template-packs` | `CreateTemplatePack` | authenticated (admin) |
| GET | `/api/template-packs/:packId` | `GetTemplatePack` | authenticated |
| PUT | `/api/template-packs/:packId` | `UpdateTemplatePack` | authenticated (admin) |
| DELETE | `/api/template-packs/:packId` | `DeleteTemplatePack` | authenticated (admin) |
| GET | `/api/template-packs/projects/:projectId/available` | `ListAvailableTemplatePacks` | project scope |
| GET | `/api/template-packs/projects/:projectId/installed` | `ListInstalledTemplatePacks` | project scope |
| GET | `/api/template-packs/projects/:projectId/compiled-types` | `GetCompiledTypes` | project scope |
| POST | `/api/template-packs/projects/:projectId/assign` | `AssignTemplatePack` | project scope |
| PATCH | `/api/template-packs/projects/:projectId/assignments/:assignmentId` | `UpdateAssignment` | project scope |
| DELETE | `/api/template-packs/projects/:projectId/assignments/:assignmentId` | `RemoveAssignment` | project scope |

### Key Entities

**`GraphTemplatePack`** — table `kb.graph_template_packs`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `name` | string | unique identifier name |
| `version` | string | semver string |
| `description` | string | |
| `author` | string | |
| `source` | string | e.g. `"official"`, `"community"` |
| `license` | string | SPDX license ID |
| `repository_url` | string | |
| `documentation_url` | string | |
| `object_type_schemas` | jsonb | map of type name → JSON Schema |
| `relationship_type_schemas` | jsonb | map of rel type → schema |
| `ui_configs` | jsonb | map of type name → UI config |
| `extraction_prompts` | jsonb | map of type name → prompt config |
| `checksum` | string | SHA256 of canonical content |
| `draft` | bool | drafts are not visible to projects |
| `published_at` | time.Time | nullable |
| `deprecated_at` | time.Time | nullable |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

**`ProjectTemplatePack`** — table `kb.project_template_packs` (assignment join table)

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | |
| `template_pack_id` | UUID | |
| `active` | bool | inactive packs don't contribute compiled types |
| `installed_at` | time.Time | |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

### Service Notes

- `GetCompiledTypes` merges all active packs for a project into a single flat type map, with later-assigned packs overriding earlier ones for the same type name.
- Deleting a pack that is currently assigned to any project returns a 409 Conflict.

---

## workspace

### Overview

The largest domain. Manages **agent workspaces** — isolated compute environments (Firecracker microVMs, E2B sandboxes, or gVisor containers) where AI agents execute code. Also manages **hosted MCP servers** — persistent containers that expose MCP tool endpoints via a stdio bridge.

Key subsystems:

| Subsystem | File | Purpose |
|-----------|------|---------|
| Orchestrator | `orchestrator.go` | create/stop/resume/snapshot workspaces; lifecycle FSM |
| Checkout Service | `checkout.go` | git clone + branch setup inside a workspace |
| Warm Pool | `warm_pool.go` | pre-warmed workspace pool to reduce cold-start latency |
| Cleanup Worker | `cleanup.go` | periodic eviction of expired/idle workspaces |
| Auto Provisioner | `auto_provisioner.go` | on-demand provisioning triggered by agent sessions |
| MCP Hosting | `mcp_hosting.go` | start/stop/restart hosted MCP server containers |
| Stdio Bridge | `stdio_bridge.go` | JSON-RPC bridge between HTTP and container stdio |
| Setup Executor | `setup_executor.go` | run setup scripts inside a workspace after creation |
| Audit Middleware | `audit.go` | records every tool invocation for compliance |

### HTTP Routes — Workspace Management

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/v1/agent/workspaces` | `ListWorkspaces` | `admin:read` |
| GET | `/api/v1/agent/workspaces/providers` | `ListProviders` | `admin:read` |
| GET | `/api/v1/agent/workspaces/:id` | `GetWorkspace` | `admin:read` |
| POST | `/api/v1/agent/workspaces` | `CreateWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/from-snapshot` | `CreateFromSnapshot` | `admin:write` |
| DELETE | `/api/v1/agent/workspaces/:id` | `DeleteWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/:id/stop` | `StopWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/:id/resume` | `ResumeWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/:id/attach` | `AttachWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/:id/detach` | `DetachWorkspace` | `admin:write` |
| POST | `/api/v1/agent/workspaces/:id/snapshot` | `SnapshotWorkspace` | `admin:write` |

### HTTP Routes — Tool Execution (all require `admin:write` + audit middleware)

| Method | Path | Handler |
|--------|------|---------|
| POST | `/api/v1/agent/workspaces/:id/bash` | `BashTool` |
| POST | `/api/v1/agent/workspaces/:id/read` | `ReadTool` |
| POST | `/api/v1/agent/workspaces/:id/write` | `WriteTool` |
| POST | `/api/v1/agent/workspaces/:id/edit` | `EditTool` |
| POST | `/api/v1/agent/workspaces/:id/glob` | `GlobTool` |
| POST | `/api/v1/agent/workspaces/:id/grep` | `GrepTool` |
| POST | `/api/v1/agent/workspaces/:id/git` | `GitTool` |

### HTTP Routes — Hosted MCP Servers

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/v1/mcp/hosted` | `ListHostedServers` | `admin:read` |
| GET | `/api/v1/mcp/hosted/:id` | `GetHostedServer` | `admin:read` |
| POST | `/api/v1/mcp/hosted` | `RegisterMCPServer` | `admin:write` |
| POST | `/api/v1/mcp/hosted/:id/call` | `CallMCPServer` | `admin:write` |
| POST | `/api/v1/mcp/hosted/:id/restart` | `RestartMCPServer` | `admin:write` |
| DELETE | `/api/v1/mcp/hosted/:id` | `DeleteHostedServer` | `admin:write` |

### Key Entity

**`AgentWorkspace`** — table `kb.agent_workspaces`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `agent_session_id` | UUID | nullable; owning agent session |
| `container_type` | ContainerType | `"agent_workspace"` \| `"mcp_server"` |
| `provider` | ProviderType | `"firecracker"` \| `"e2b"` \| `"gvisor"` |
| `provider_workspace_id` | string | provider-assigned ID |
| `repository_url` | string | git repo to check out |
| `branch` | string | git branch |
| `deployment_mode` | DeploymentMode | `"managed"` \| `"self-hosted"` |
| `lifecycle` | Lifecycle | `"ephemeral"` \| `"persistent"` |
| `status` | Status | see below |
| `created_at` | time.Time | |
| `last_used_at` | time.Time | updated on every tool call |
| `expires_at` | time.Time | nullable; used by cleanup worker |
| `resource_limits` | jsonb | CPU/memory/disk constraints |
| `snapshot_id` | string | nullable; ID of last snapshot |
| `mcp_config` | jsonb | MCP server config (for `mcp_server` type) |
| `metadata` | jsonb | free-form key-value metadata |

### Enums / Constants

```go
type ContainerType string
const (
    ContainerTypeAgentWorkspace ContainerType = "agent_workspace"
    ContainerTypeMCPServer      ContainerType = "mcp_server"
)

type ProviderType string
const (
    ProviderTypeFirecracker ProviderType = "firecracker"
    ProviderTypeE2B         ProviderType = "e2b"
    ProviderTypeGVisor      ProviderType = "gvisor"
)

type DeploymentMode string
const (
    DeploymentModeManaged    DeploymentMode = "managed"
    DeploymentModeSelfHosted DeploymentMode = "self-hosted"
)

type Lifecycle string
const (
    LifecycleEphemeral  Lifecycle = "ephemeral"
    LifecyclePersistent Lifecycle = "persistent"
)

type Status string
const (
    StatusCreating Status = "creating"
    StatusReady    Status = "ready"
    StatusStopping Status = "stopping"
    StatusStopped  Status = "stopped"
    StatusError    Status = "error"
)
```

### Tool Request / Response DTOs

**`BashRequest`**: `command` (string), `timeout` (int, ms), `workdir` (string)
**`BashResponse`**: `stdout`, `stderr`, `exitCode`, `timedOut`

**`ReadRequest`**: `filePath`, `offset` (int, line), `limit` (int, lines)
**`ReadResponse`**: `content`, `lineCount`, `truncated`

**`WriteRequest`**: `filePath`, `content`

**`EditRequest`**: `filePath`, `oldString`, `newString`, `replaceAll` (bool)
**`EditResponse`**: `replacements` (int)

**`GlobRequest`**: `pattern`, `path` (base dir)
**`GlobResponse`**: `files` ([]string)

**`GrepRequest`**: `pattern`, `path`, `include` (file glob), `maxResults`
**`GrepResponse`**: `matches` ([]`GrepMatch`); `GrepMatch`: `file`, `line`, `content`

**`GitRequest`**: `args` ([]string), `workdir`
**`GitResponse`**: `stdout`, `stderr`, `exitCode`

### MCP Hosting DTOs

**`RegisterMCPServerRequest`**: `name`, `imageRef`, `command` ([]string), `env` (map), `mcpConfig` (jsonb)
**`MCPCallRequest`**: `method`, `params` (jsonb)
**`MCPCallResponse`**: `result` (jsonb), `error` (jsonb)
**`MCPServerStatus`**: `id`, `name`, `status`, `pid`, `uptime`, `lastError`

### Audit Middleware

Every tool call (bash/read/write/edit/glob/grep/git) is wrapped by `audit.go`, which records:
- workspace ID, agent session ID, tool name, request payload hash, response status, duration
- Written to `kb.workspace_audit_log` asynchronously

---

## workspaceimages

### Overview

Admin catalog of workspace base images. Images are provider-specific Docker references that the workspace orchestrator pulls when creating new workspaces. Implements the `workspace.ImageResolver` interface so the workspace domain can look up an image by name without depending on this package directly.

### HTTP Routes

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/admin/workspace-images` | `ListImages` | `admin:read` |
| GET | `/api/admin/workspace-images/:id` | `GetImage` | `admin:read` |
| POST | `/api/admin/workspace-images` | `CreateImage` | `admin:write` |
| DELETE | `/api/admin/workspace-images/:id` | `DeleteImage` | `admin:write` |

### Key Entity

**`WorkspaceImage`** — table `kb.workspace_images`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `name` | string | human-readable name; used as resolver key |
| `type` | ImageType | `"built_in"` \| `"custom"` |
| `docker_ref` | string | full Docker image reference (registry/repo:tag) |
| `provider` | ProviderName | `"firecracker"` \| `"gvisor"` |
| `status` | ImageStatus | `"pending"` \| `"pulling"` \| `"ready"` \| `"error"` |
| `error_msg` | string | populated when `status = "error"` |
| `project_id` | UUID | nullable; null = global (available to all projects) |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

### Enums / Constants

```go
type ImageType string
const (
    ImageTypeBuiltIn ImageType = "built_in"
    ImageTypeCustom  ImageType = "custom"
)

type ImageStatus string
const (
    ImageStatusPending ImageStatus = "pending"
    ImageStatusPulling ImageStatus = "pulling"
    ImageStatusReady   ImageStatus = "ready"
    ImageStatusError   ImageStatus = "error"
)

type ProviderName string
const (
    ProviderNameFirecracker ProviderName = "firecracker"
    ProviderNameGVisor      ProviderName = "gvisor"
)
```

### `workspace.ImageResolver` Interface

```go
type ImageResolver interface {
    ResolveImage(ctx context.Context, name string, provider ProviderName) (*WorkspaceImage, error)
}
```

`workspaceimages.Store` satisfies this interface. The workspace orchestrator receives it via fx dependency injection.

---

## mcp

### Overview

Core MCP (Model Context Protocol) JSON-RPC server. Exposes tools, resources, and prompts to MCP clients (Claude Desktop, Cursor, etc.). Supports two transports:

- **SSE transport** — long-lived Server-Sent Events connection (legacy MCP spec)
- **Streamable HTTP transport** — per-request HTTP with optional streaming (MCP spec 2025-11-25)

Brave Search integration provides a web-search tool when a Brave API key is configured.

> **See `apps/server/domain/mcp/README.md` for the full tools, resources, and prompts reference.**

### HTTP Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/mcp/sse` | `SSEConnect` | token in query param |
| POST | `/mcp/messages` | `SSEMessage` | token in query param |
| POST | `/mcp` | `StreamableHTTP` | Bearer token |
| GET | `/mcp` | `StreamableHTTP` (GET for SSE upgrade) | Bearer token |
| DELETE | `/mcp` | `StreamableHTTP` (session close) | Bearer token |

### Key Internals

**`EventStore`** — in-memory ring buffer of SSE events per session. Used to replay missed events when a client reconnects with `Last-Event-ID`.

**`JSONRPCHandler`** — routes `method` strings to registered tool/resource/prompt handlers. Returns `{"jsonrpc":"2.0","id":...,"result":...}` or `{"jsonrpc":"2.0","id":...,"error":{...}}`.

**`BraveSearchClient`** — wraps `api.search.brave.com/res/v1/web/search`. Enabled only when `BRAVE_SEARCH_API_KEY` env var is set.

---

## mcpregistry

### Overview

Per-project registry of external MCP servers. Manages proxy connections to external servers (stdio commands, SSE endpoints, plain HTTP endpoints) and mirrors their tool catalogs into the local database so agents can discover available tools without connecting to every server on each request.

Also provides a client for the official MCP registry at `registry.modelcontextprotocol.io` — allowing admins to search and install community MCP servers directly.

### HTTP Routes — Project MCP Servers

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/admin/mcp-servers` | `ListServers` | `admin:read` |
| GET | `/api/admin/mcp-servers/:id` | `GetServer` | `admin:read` |
| GET | `/api/admin/mcp-servers/:id/tools` | `ListServerTools` | `admin:read` |
| POST | `/api/admin/mcp-servers/:id/inspect` | `InspectServer` | `admin:read` |
| POST | `/api/admin/mcp-servers` | `CreateServer` | `admin:write` |
| PATCH | `/api/admin/mcp-servers/:id` | `UpdateServer` | `admin:write` |
| DELETE | `/api/admin/mcp-servers/:id` | `DeleteServer` | `admin:write` |
| PATCH | `/api/admin/mcp-servers/:id/tools/:toolId` | `UpdateTool` | `admin:write` |
| POST | `/api/admin/mcp-servers/:id/sync` | `SyncServer` | `admin:write` |

### HTTP Routes — Official MCP Registry

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/admin/mcp-registry/search` | `SearchRegistry` | `admin:read` |
| GET | `/api/admin/mcp-registry/servers/:name` | `GetRegistryServer` | `admin:read` |
| POST | `/api/admin/mcp-registry/install` | `InstallFromRegistry` | `admin:write` |

### Key Entities

**`MCPServer`** — table `kb.mcp_servers`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | scoped to a project |
| `name` | string | display name |
| `enabled` | bool | disabled servers are not proxied |
| `type` | MCPServerType | connection type |
| `command` | string | for `stdio` type: executable path |
| `args` | text[] | for `stdio` type: arguments |
| `env` | jsonb | environment variables for `stdio` |
| `url` | string | for `sse`/`http` types |
| `headers` | jsonb | HTTP headers for `sse`/`http` |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

**`MCPServerTool`** — table `kb.mcp_server_tools`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `server_id` | UUID | FK → `mcp_servers` |
| `tool_name` | string | as reported by the server |
| `description` | string | |
| `input_schema` | jsonb | JSON Schema for tool inputs |
| `enabled` | bool | disabled tools are filtered from agent context |
| `created_at` | time.Time | |

### Enums / Constants

```go
type MCPServerType string
const (
    MCPServerTypeBuiltin MCPServerType = "builtin"
    MCPServerTypeStdio   MCPServerType = "stdio"
    MCPServerTypeSSE     MCPServerType = "sse"
    MCPServerTypeHTTP    MCPServerType = "http"
)
```

### Service Notes

- **`ProxyManager`** (`proxy.go`) — maintains a pool of live `mcp-go` client connections, keyed by server ID. Connections are created lazily on first use and evicted when a server is deleted/disabled.
- **`SyncServer`** — connects to the external server, calls `tools/list`, and upserts results into `kb.mcp_server_tools`. Safe to call repeatedly (idempotent).
- **`InspectServer`** — like `SyncServer` but returns the tool list in the response without persisting. Used during initial setup to preview what a server exposes.
- **`RegistryClient`** (`registry_client.go`) — HTTP client for `registry.modelcontextprotocol.io/api/v0`. Supports `search`, `get`, and `install` flows. `InstallFromRegistry` creates a new `MCPServer` record and immediately runs a sync.
- **`mcp_tools.go`** — exposes `mcp-servers/list` and `mcp-servers/call` as MCP tools, making the registry itself available to agents running inside the MCP server.

---

## extraction

### Overview

The extraction pipeline orchestrates turning raw documents into structured knowledge graph objects. It manages multiple async job queues and exposes two separate HTTP APIs:

1. **Admin API** — CRUD on extraction jobs, bulk operations, logs.
2. **Embedding Control API** — internal endpoints to pause/resume/reconfigure the embedding worker (used by ops tooling; no authentication on these routes by design — they should be network-restricted).

### HTTP Routes — Admin API

| Method | Path | Handler | Scope |
|--------|------|---------|-------|
| GET | `/api/admin/extraction-jobs/projects/:projectId` | `ListJobs` | `admin:read` |
| GET | `/api/admin/extraction-jobs/projects/:projectId/statistics` | `GetStatistics` | `admin:read` |
| GET | `/api/admin/extraction-jobs/:jobId` | `GetJob` | `admin:read` |
| GET | `/api/admin/extraction-jobs/:jobId/logs` | `GetJobLogs` | `admin:read` |
| POST | `/api/admin/extraction-jobs` | `CreateJob` | `admin:write` |
| POST | `/api/admin/extraction-jobs/projects/:projectId/bulk-cancel` | `BulkCancelJobs` | `admin:write` |
| DELETE | `/api/admin/extraction-jobs/projects/:projectId/bulk-delete` | `BulkDeleteJobs` | `admin:write` |
| POST | `/api/admin/extraction-jobs/projects/:projectId/bulk-retry` | `BulkRetryJobs` | `admin:write` |
| PATCH | `/api/admin/extraction-jobs/:jobId` | `UpdateJob` | `admin:write` |
| DELETE | `/api/admin/extraction-jobs/:jobId` | `DeleteJob` | `admin:write` |
| POST | `/api/admin/extraction-jobs/:jobId/cancel` | `CancelJob` | `admin:write` |
| POST | `/api/admin/extraction-jobs/:jobId/retry` | `RetryJob` | `admin:write` |

### HTTP Routes — Embedding Control API

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/api/embeddings/status` | `GetEmbeddingStatus` | none (internal) |
| POST | `/api/embeddings/pause` | `PauseEmbeddings` | none (internal) |
| POST | `/api/embeddings/resume` | `ResumeEmbeddings` | none (internal) |
| PATCH | `/api/embeddings/config` | `UpdateEmbeddingConfig` | none (internal) |

> **Note:** Embedding control routes have no authentication middleware. They must be protected at the network/firewall level.

### Key Entities

**`ObjectExtractionJob`** — table `kb.object_extraction_jobs`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `project_id` | UUID | |
| `document_id` | UUID | nullable |
| `datasource_id` | UUID | nullable |
| `job_type` | JobType | |
| `trigger_type` | TriggerType | |
| `status` | internal job status | mapped to DTO status for API responses |
| `priority` | int | higher = processed first |
| `error_msg` | string | last error |
| `retry_count` | int | |
| `max_retries` | int | |
| `started_at` | time.Time | nullable |
| `completed_at` | time.Time | nullable |
| `created_at` | time.Time | |
| `updated_at` | time.Time | |

**`ExtractionJobLog`** — table `kb.extraction_job_logs`

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | PK |
| `job_id` | UUID | FK → extraction job |
| `operation` | LogOperation | type of step |
| `message` | string | human-readable log line |
| `metadata` | jsonb | structured context (model used, token counts, etc.) |
| `created_at` | time.Time | |

Additional job tables (same pattern, different queues):
- `kb.chunk_embedding_jobs`
- `kb.graph_embedding_jobs`
- `kb.document_parsing_jobs`
- `kb.data_source_sync_jobs`

### Enums / Constants

```go
type JobType string
const (
    JobTypeFullExtraction    JobType = "full_extraction"
    JobTypeReextraction      JobType = "reextraction"
    JobTypeIncremental       JobType = "incremental"
)

type TriggerType string
const (
    TriggerTypeManual    TriggerType = "manual"
    TriggerTypeScheduled TriggerType = "scheduled"
    TriggerTypeWebhook   TriggerType = "webhook"
)

type LogOperation string
const (
    LogOperationLLMCall              LogOperation = "llm_call"
    LogOperationChunkProcessing      LogOperation = "chunk_processing"
    LogOperationObjectCreation       LogOperation = "object_creation"
    LogOperationRelationshipCreation LogOperation = "relationship_creation"
    LogOperationSuggestionCreation   LogOperation = "suggestion_creation"
    LogOperationValidation           LogOperation = "validation"
    LogOperationError                LogOperation = "error"
)
```

### Job Status Mapping (internal → API DTO)

| Internal DB status | API DTO status |
|--------------------|----------------|
| `pending` | `"queued"` |
| `processing` | `"running"` |
| `completed` | `"completed"` |
| `failed` | `"failed"` |
| `dead_letter` | `"failed"` |
| `cancelled` | `"cancelled"` |

Note: the DTO value `"requires_review"` maps back to internal `completed` with a review flag set on the job metadata.

### Statistics Response

`GET .../statistics` returns per-project counts broken down by status and job type, plus throughput metrics (jobs per hour, average duration).

---

## scheduler

### Overview

Background cron scheduler using `robfig/cron` with seconds precision. Runs maintenance tasks on a fixed schedule. No HTTP routes — entirely internal.

### Tasks

| Task | Schedule | Purpose |
|------|----------|---------|
| `RevisionCountRefreshTask` | every 15 min | Refreshes cached revision counts on graph nodes to avoid expensive COUNT queries at read time |
| `TagCleanupTask` | every 30 min | Removes orphaned tags (tags with no associated objects) |
| `CacheCleanupTask` | every 1 hour | Evicts stale entries from in-memory and DB-backed caches |
| `StaleJobCleanupTask` | every 10 min | Marks jobs that have been in `processing` state beyond their timeout as `failed` (handles worker crashes) |

All tasks run with a **30-minute hard timeout** via `context.WithTimeout`. If a task exceeds this limit it is cancelled and the error is logged; the next scheduled run will attempt it again.

### fx Integration

```go
// module.go wires the scheduler into the fx lifecycle
fx.Provide(NewScheduler)
fx.Invoke(RegisterTasks)

// Scheduler starts on app OnStart and stops gracefully on OnStop
```

Tasks are registered by calling `scheduler.AddFunc(cronExpr, task.Run)`. Each task receives its dependencies via fx injection.

---

*This document was generated from source code in `apps/server/domain/`. For framework patterns, see `apps/server/AGENT.md`. For MCP tools/resources/prompts detail, see `apps/server/domain/mcp/README.md`.*
