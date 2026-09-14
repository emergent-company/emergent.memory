# 03 — Agent Model

An agent is the central object. It is fully defined in memory (as an `AgentDefinition`
plus an `Agent` runtime entity); the Go app and bridge merely *reference* it.

## Memory-facing agent model

The Go app exposes a simplified agent model to users, mapped 1:1 onto memory's
`AgentDefinition`. One agent = one assistant.

| Field | Type | Notes |
|---|---|---|
| `id` | string | memory object id |
| `name` | string | unique, human name (also the LiveKit dispatch name) |
| `system_prompt` | string | the agent's instructions |
| `model` | string | provider-prefixed, e.g. `deepseek/deepseek-v4-flash`, `google/gemini-2.5-flash` |
| `temperature`, `max_tokens` | number | optional model knobs |
| `tools` | list | MCP tool references (server id + allowlist) |
| `banned_tools` | list | excluded tools |
| `skills` | list | deferred (D10) |
| `enabled` | bool | controls whether a worker is dispatched (D5 "delete = stop") |
| `voice` | bool | derived — see below |

## Default model resolution & display

An agent whose `model` is unset ("Auto — default model") falls back to the
project's default generative model (project model-config). Memory's
`GET /agent-definitions/:id` returns the resolved choice as `effectiveModel`
(an explicit per-agent model wins). The Go gateway decodes it and shows the
**exact model** on the agent dashboard. On the Agents list it appears in the
desktop table's **Model** column (`md` and up — D37); the mobile card grid
shows only name + icon and carries no model. The list summary has no
explicit-vs-resolved signal, so there is no "(default)" suffix.

Resolution is project-config-only in memory (`ResolveGenerativeModel` returns
empty when no project default is set), so `effectiveModel` comes back empty
for an auto agent on a project without a default — the gateway then warns
instead of guessing: **error** when the project has no configured provider
(chats can't run — memory returns `503 no_provider`), **warning** when
providers exist but no default pins the model. Warning surfaces: agent
dashboard summary and the agent-settings Model section, both linking to
Settings → Providers (see session log
[2026-09-08-agent-model-default-display](../sessions/2026-09-08-agent-model-default-display.md);
the decision-log row is pending the concurrent docs WIP landing). Surfacing
the runtime/env fallback when no project default exists is a memory-side
follow-up ([task](../tasks/agent-effective-model-memory.md)).

## Explicit (pinned) models without a configured provider

An agent with an explicit model (`providerKey/modelName`) is served by the
provider named by the model's prefix — `openai/deepseek-v4-pro` needs the
`openai` provider. When the project has **no configured provider**, or has
providers but none matches that prefix, the agent's chats cannot run and the
model is flagged **error-level** everywhere it is presented: the agent
dashboard notice, the agent-settings Model section, and the agent rows on a
blueprint's details page (models pinned by blueprints included). The chat
workspace shows the same error banner for the selected agent before any
message is sent, and it updates live as the agent picker changes. The stored
model is never mutated — no prompting or dropping at install/apply — and the
warning links to Settings → Providers. A bare model (no `/`) with providers
configured is treated as satisfied (mirroring how model pickers tolerate
custom names), and a matching provider is satisfied even when the model name
isn't in that provider's catalog (custom base URLs legitimately serve
off-catalog models).

## MCP attachment

An agent's capabilities come entirely from **MCP servers** in memory's registry. No
hardcoded Python function tools remain.

- The owner registers a server in the gateway UI (**Settings → MCP Servers**,
  `/settings/mcp-servers`): name + transport (`stdio`/`sse`/`http`) + connection fields
  (URL and headers-as-key/value-rows for remote; command/args/env for stdio). Builtin
  servers render read-only.
- Per-server row actions **sync** tools (`POST /:id/sync` → memory `tools/list` discovery,
  surface pruned tools) and **inspect** the connection (`POST /:id/inspect`); every cached
  tool carries an enable toggle (`PATCH /:id/tools/:toolId`).
- The owner attaches tools to an agent via the agent **tools picker** (collapsible
  per-server groups + per-tool checkboxes). The persisted whitelist stores the **bare tool
  name** memory's tools API returns (e.g. `web_search_exa`), not the namespaced key.
- At run time, memory's agent loop resolves each whitelist entry to its pooled key and
  calls it: external tools are keyed `<slugified server>_<toolName>` (server names are
  slugified so the name is a valid LLM function name) and calls route back to the raw
  server. Bare-name resolution + slug-aware routing require memory ≥ the release with PRs
  #406/#410; earlier builds silently dropped whitelisted tools (only `set_session_title`
  reached the model).

Examples of MCP servers the owner registers:
- **ha-mcp** — Home Assistant (replaces the retired `ha_tools.py` / `ha_catalog.py`)
- **memory** (itself) — graph/search/remember/forget tools for memory-bearing agents
- **exa** / **firecrawl** — public web-search/scrape MCPs (keyless) used as dev test targets
- any user-supplied server (filesystem, web-search, etc.)

## Voice

Every enabled agent is **voice-capable by default** — no per-agent voice flag in v1. The
voice bridge treats any agent uniformly: it streams the agent's chat output to TTS. Voice
vs text is a *client* difference, not an *agent* difference (D7 "one brain").

## Memory (long-term, opt-in)

Conversation context is **fresh per session** (D14) — each text/voice session starts a new
memory conversation. Long-term memory is separate and **opt-in per agent**: when the user
enables the agent's memory tools (`search-hybrid`, `remember`, `entity-*`, etc. from the
memory MCP server), the agent can persist and recall facts across sessions. An agent without
those tools is stateless between sessions.

## Skills (deferred — D10)

Future: a loadable/searchable library of small snippets an agent can pull in on demand.
Memory already has the storage side (`domain/skills`, `AgentDefinition.Skills`,
`AutoLoadSkills`). v1 simply does not surface it. When enabled, it is a memory feature the
Go app exposes — no Python/Go agent logic.

## A2A (deferred — D9)

Agent-to-agent is **memory-internal**. Enabling it later means granting an agent the
`trigger_agent` / `spawn_agents` tools — no new plumbing in the Go app or bridge. Memory v1
agents are standalone (each is talked to directly by the owner).

## Lifecycle

```
create agent (API/web) ──► memory AgentDefinition + Agent (enabled)
      │
      └──► Go supervisor sees enabled agent ──► spawn bridge worker (name=x)
                 │
        chat (text or voice) ──► memory chat loop (agent brain)
                 │
        disable/delete ──► supervisor stops worker
```

- **Create** → immediately chat-capable (text) and voice-capable (bridge spawn is async,
  seconds).
- **Edit prompt/model/tools** → takes effect on the next chat turn (memory reads current
  definition; no worker restart required for text. Voice bridge holds no agent state, so
  edits flow through immediately).
- **Disable/delete** → worker stopped by supervisor; agent no longer dispatchable.

## Invariants

1. An agent's behavior is **fully determined by its memory definition** — the Go app and
   bridge add no behavior.
2. Text chat and voice chat reach the **same** memory chat loop → identical behavior.
3. Removing an agent's MCP servers removes its capabilities immediately (no rebuild).
