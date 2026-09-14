# 02 — Memory Backend (Emergent Memory)

Emergent Memory is the **single brain and store** for Memory. It already implements
everything Memory needs except voice. This file records what we depend on and how we
address it.

- **Repo:** `/root/emergent.memory` · Go, PostgreSQL 16 + pgvector · running on `:5300`
- **SDKs:** Go (29 service clients), Python (13 sub-clients), Swift

## Capabilities we depend on (all verified live, not spec-only)

| Capability | What it gives Memory |
|---|---|
| **Agent definitions** | `AgentDefinition` entity: system prompt, model, tools, banned tools, skills, flow type, max steps, tool policies, sandbox, visibility, ACP config, dispatch mode. Full CRUD at `/api/projects/:projectId/agent-definitions`. |
| **Agent runtime** | `Agent` entity + executor (Google ADK `llmagent`), run persistence (`agent_runs`, `agent_run_messages`, `agent_run_tool_calls`). |
| **Chat** | `POST /api/chat/stream` — streaming SSE, multi-turn, tool-calling, conversation + message persistence. |
| **MCP registry** | `/api/mcp` + MCP registry domain — ~90–120 tools; schema/search/entity/relationship/document/agent/provider/skill/token/embedding/trace/remember/forget categories. |
| **Skills** | `domain/skills` — named skills referenced by `AgentDefinition.Skills` + `AutoLoadSkills` (deferred for Memory v1). |
| **A2A** | `trigger_agent`, `spawn_agents`, `list_available_agents`, dispatch queue, depth limits, suspend/resume cascade (deferred for Memory v1). |
| **ACP** | Agent Card Protocol: `agent-list`, `trigger-run`, run status/events. |
| **Tenancy** | org → project → user; scoped `emt_*` tokens; project roles. |
| **Project deletion** | Async grace-period soft-delete: `DELETE /api/projects/:id` sets `deletion_scheduled_for` and returns `202`; a durable scheduler sweep hard-purges after the grace period; `POST /api/projects/:id/restore` cancels; `GET /api/projects?include_pending=true` lists pending rows with `deletionStatus`/`deletionScheduledFor` (D40). |
| **LLM** | Gemini (google/vertex) + OpenAI-compatible (openai, deepseek). Embeddings. Provider creds encrypted at rest. |
| **Blueprints** | Installable packs (schema + agent definitions + seed) — provisioned via `POST /api/blueprints` (`create`→`publish`→`apply`); applied state tracked in `blueprint_applications` (`GET /applied`, `POST .../unapply`); upgrades supersede the prior version and auto-uninstall it after migration. |
| **Schema migrations** | Version-aware migration between schema ids: `POST .../migrate/preview` returns a structured `plan` (`type_renames`, `property_renames`, `removed_properties`, `added_types`, `added_properties`) plus per-type `migrated/dropped/added/coerced_props`; `.../migrate/execute` (`force` bypasses the `dangerous`-risk block), `.../migrate/rollback`, `.../migrate/commit`. |

## Interfaces (summary — see 10-api-contracts.md for shapes)

| Interface | Endpoint | Used by |
|---|---|---|
| Chat (streaming) | `POST /api/chat/stream` | Go gateway (web text) + voice bridge |
| Stateless query/ask/remember/forget | `/api/projects/:projectId/query`, `/ask`, `/remember`, `/forget` | agents, tools |
| Agent + definition CRUD | `/api/projects/:projectId/agents`, `/agent-definitions` | Go gateway (proxied) |
| MCP | `/api/mcp` (+ `/rpc`, `/sse/:projectId`) | agent tool-calling (internal) |
| A2A / ACP | `trigger_agent`/`spawn_agents` tools; `trigger-run`/`agent-list` | agent→agent (deferred) |

*The Go gateway's memory client is REST-only — it never speaks MCP (MCP is reserved for
in-agent tool calling). The blueprints schema catalog reads `GET /api/schemas/projects/:projectId`
and the agent-memory list reads `GET /api/graph/objects/search` — the REST mirrors of the MCP
`schema-list`/`entity-query` tools (memory PR #389).*

## Verified specifics

Confirmed from source (2026-08) — these pin the design:

- **Chat needs only an `AgentDefinition`** — no runtime `Agent` row, no enable step. Memory
  auto-creates a dummy runtime agent. So "create a definition → chat" is the whole ad-hoc path.
- **Token-level streaming** — chat emits `token` deltas (variable size), so the bridge can
  start TTS before a turn completes (low perceived latency).
- **Thinking not streamed** — memory persists the agent's reasoning as `operator` messages
  (the planning monologue carried by `content.function_calls`; the final answer is also
  `operator` but without `function_calls`), yet strips it from the live SSE stream. Memory
  requests a `thinking` SSE event to surface it live — contract in 10-api-contracts.md §3.
  `chat.js` already consumes it and falls back to history-only until memory ships it.
- **Provider credentials must exist** at org level (encrypted at rest). Without them chat
  returns `503 no_provider`. Model names must carry a provider prefix (`deepseek/…`,
  `google/…`, `openai/…`).
- **Single-owner config**: memory supports `STANDALONE_MODE=true` + `STANDALONE_API_KEY`
  (one "standalone" user, all scopes, auto-provisioned Default Org/Project) — or mint one
  `emt_*` token with the needed scopes. Memory uses an `emt_*` token; project is **derived
  from the token**, no `X-Project-ID` header needed.
- **Gotcha — MCP server secrets are plaintext**: `headers`/`env` on registered MCP servers
  are stored and returned as plaintext JSONB (not encrypted). Acceptable only behind the
  authenticated gateway boundary (see 09-security.md); do not assume encryption.

## Tenancy — how Memory uses it

Memory requires org → project → user scoping (every entity carries `project_id`).

Memory uses one back-end `emt_*` token per deployment (one memory project by default; scopes:
`agents:read/write`, `chat:use/admin`, `data:read`, `schema:read`, `projects:read`), project
derived from the token. The gateway default is `AUTH_MODE=session` (public, Zitadel-
authenticated; `dev` is an explicit local-only escape hatch). In `AUTH_MODE=session`, the
gateway surfaces memory's org/project tenancy — `ListOrgs`/`ListProjects`/`CreateProject`
proxy `GET /api/orgs`, `GET/POST /api/projects`, and the web UI shows an org-grouped project
switcher; session-token requests carry `X-Project-ID`/`X-Org-ID` headers (the `emt_*` path
stays header-free — the token is already project-bound). Clients never see the memory token
— the Go app is the sole memory caller.

## Agent definition (memory's `AgentDefinition`)

Fields we surface to users (see 03-agent-model.md for the Memory-facing model):

- `system_prompt` — the agent's instructions
- `model` — name (provider-prefixed: `deepseek/deepseek-v4-flash`, `google/gemini-2.5-flash`, `openai/gpt-4o`), temperature, max tokens, native tools, thinking
- `tools` / `banned_tools` — MCP tool references (allowlist) and exclusions
- `skills` / `auto_load_skills` — (deferred)
- `flow_type`, `max_steps`, `tool_policies` — loop control (confirm/disable per tool)
- `visibility`, `dispatch_mode`, `acp_config` — exposure and trigger control

## What memory does NOT provide (Memory must fill)

- **No voice.** No TTS, no audio streaming, no duplex transport. Chat is SSE text only.
- **No real-time audio turn-taking / barge-in.** Voice turn-taking lives in the bridge.
- **LLM providers limited** to Gemini and OpenAI-compatible endpoints (this covers DeepSeek
  via LiteLLM; no native Anthropic/xAI — use LiteLLM as the compatibility layer if needed).

## Reliability contract

- Memory is the **durable** store — its Postgres is the system's persistence layer.
- Memory's Go app and bridge workers are **stateless** and can restart freely.
- Memory downtime degrades Memory to "unavailable" for chat + agent config; voice and web
  both depend on it. (No local cache — by design, D6/no-tenancy keeps this simple.)

## Production instance (as-built)

The memory instance Memory actually uses:

| Item | Value |
|---|---|
| URL | `https://memory.emergent-company.ai` (v0.45.2) |
| Org | `28a02664-de4f-484b-ad88-5422bc470f1a` "Dev Superadmin's Org" |
| Project | `alfred` (`558b793e-6f30-4813-9105-44e796ba56eb`) |
| Generative model | `openai/deepseek-v4-flash` (DeepSeek via LiteLLM) |
| Embedding model | `google/gemini-embedding-001` |
| Agents | `diane` (`cd81333b…`), `alfred` (`9101c2e3…`) |
| Credentials | account token (`admin:all`) + project token (full scopes) — `.env`, git-ignored |

**DeepSeek:** works for chat on v0.45.0+ — fixed in
[#316](https://github.com/emergent-company/emergent.memory/issues/316) (model-fallback in
`ModelFactory.CreateModel`). Generative = DeepSeek via LiteLLM; embedding = Gemini
(`google/gemini-embedding-001`) — the `configure-project --embedding-model` resolver bug was
fixed in [#317](https://github.com/emergent-company/emergent.memory/issues/317).

**MCP registry scope:** the `admin:read`/`admin:write` ungrantable-scope bug was fixed in
[#318](https://github.com/emergent-company/emergent.memory/issues/318) (v0.45.2 — collapsed to
the grantable `admin` scope). A project token with `admin` can now register/list/update MCP
servers, unblocking the gateway's MCP-server CRUD proxy.

**ha-mcp deferred:** the ha-mcp server (`ha-home:9583`) is not reachable from production
memory, so Memory's home-control tools are not yet wired. Revisit when memory is co-located
with ha-home (or ha-mcp is exposed).
