# Memory SDK Reference for LLMs

Memory is an AI memory and knowledge graph platform. It stores objects and relationships in a graph, chunks and embeds documents, and exposes a REST API for queries, agent orchestration, and LLM tracing.

There are two client SDKs:
- **Go SDK** — full-featured server-side client (`github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`)
- **Python SDK** — full-featured REST client (`emergent-memory` on PyPI, `sdk/python/`)

Full docs site: https://emergent-company.github.io/emergent.memory/

---

## Go SDK

Full reference: [docs/llms-go-sdk.md](llms-go-sdk.md)

**Module:** `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`

**Install:**
```bash
go get github.com/emergent-company/emergent.memory/apps/server/pkg/sdk@latest
```

**Quick start:**
```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://your-server",
    Auth: sdk.AuthConfig{Mode: "apikey", APIKey: "emt_your_token"},
    OrgID:     "org-uuid",
    ProjectID: "project-uuid",
})
client.SetContext(orgID, projectID)  // update context at runtime
```

**29 service clients** on `*Client`:

Context-scoped (25): `Documents`, `Chunks`, `Search`, `Graph`, `Chat`, `Projects`, `Orgs`, `Users`, `APITokens`, `MCP`, `MCPRegistry`, `Branches`, `UserActivity`, `TypeRegistry`, `Notifications`, `Tasks`, `Monitoring`, `Agents`, `AgentDefinitions`, `DataSources`, `DiscoveryJobs`, `EmbeddingPolicy`, `Integrations`, `TemplatePacks`, `Chunking`

Non-context (4): `Health`, `Superadmin`, `APIDocs`, `Provider`

**Auth modes:** `apikey` (X-API-Key), `apitoken` (Bearer, for `emt_*` tokens — auto-detected), `oauth` (device flow via `NewWithDeviceFlow`)

**Error predicates:** `errors.IsNotFound`, `errors.IsForbidden`, `errors.IsUnauthorized`, `errors.IsBadRequest`, `errors.ParseErrorResponse`

**Dual-ID graph model:** Every graph object has `id` (version ID, changes on update) and `canonicalID` (entity ID, stable). Use `canonicalID` for persistent references. `graphutil.UniqueByEntity` deduplicates by canonical ID.

---

## Python SDK

Full reference: [docs/llms-python-sdk.md](llms-python-sdk.md)

**Package:** `emergent-memory` · **Install:** `pip install emergent-memory-sdk`

**Quick start:**
```python
from emergent import Client

client = Client.from_api_key("https://memory.emergent-company.ai", "emt_abc123")
client.set_context(org_id="org_1", project_id="proj_1")
```

**13 sub-clients** on `client.*`:

`graph`, `chat`, `agents`, `agent_definitions`, `documents`, `search`, `mcp`, `projects`, `orgs`, `schemas`, `skills`, `branches`, `api_tokens`, `tasks`

**Auth modes:** `from_api_key()` (X-API-Key; auto-upgrades `emt_*` to Bearer), `from_oauth_token()` (Bearer), `from_env()` (auto-discovers from `~/.memory/config.yaml`, `.env.local`, `.env`, then `MEMORY_*` env vars)

**Error handling:** `APIError` with `.status_code`, `.message`, `.is_not_found`, `.is_forbidden`, `.is_unauthorized`

**Streaming:** `client.chat.stream(conversation_id, message)` yields typed SSE events: `MetaEvent`, `TokenEvent`, `MCPToolEvent`, `ErrorEvent`, `DoneEvent`

