# 14 — General Assistant ("Operator") Agent

A single **general assistant** agent that can read and change *all* of Memory's
settings — agents, skills, blueprints/schema, MCP servers, providers, tokens,
documents — and inspect the knowledge graph read-only — through a normal chat.
Every change is gated behind a **proposal the user accepts** before any write
happens.

The design is directly inspired by the Emergent Memory **`blueprint-architect`**
blueprint agent (`blueprints/blueprint-architect/agents/blueprint-architect.yaml`):
a dedicated agent definition whose system prompt encodes an operating discipline
("propose → checkpoint → apply → confirm"), a broad-but-scoped tool grant, and
`ask_user` for human checkpoints.

## Goals

1. **One operator for the whole UI.** One agent, one conversation, every setting.
2. **Propose before write.** The assistant never mutates state silently — it
   emits a suggestion (a proposed resource, a plan, or a diff) and pauses for
   acceptance.
3. **Chat is the interface.** The proposal loop is a *conversation*: the user can
   accept, reject, or revise in free text ("make it shorter", "rename it X") and
   the assistant re-proposes.
4. **No gateway brain.** The assistant is a pure Memory `AgentDefinition`. The Go
   app adds no agent logic — only a chat surface and richer proposal rendering.
   (Principles 4 "thin operators", D7 "memory = brain".)

## Non-goals (v1)

- **Not a voice agent.** It is a control surface; text chat only. (Voice could be
  a later opt-in, but the proposal/accept UX is inherently visual.)
- **No auto-apply.** The assistant has `ask_user` as a hard checkpoint; no
  "just do it" mode in v1.
- **Not a full agentic runtime.** It does not itself *run* other agents' jobs; it
  *configures* them. (Delegation/A2A is a separate, optional later grant.)
- **No proactive monitoring.** v1 is on-demand; "watch my settings and flag drift"
  is deferred.

## The agent (blueprint)

### Definition

```yaml
name: operator
description: >
  The general assistant for Memory. Configures the whole platform — agent
  definitions, skills, blueprints/schema, MCP servers, providers, tokens, and
  documents — and inspects the knowledge graph read-only. Every change is
  proposed first and applied only after explicit user acceptance.

flowType: agentic
visibility: project

tools:
  - "skill-*"                 # skill CRUD — list/get/create/update/delete
  - "schema-*"                # blueprint/schema CRUD + assign/uninstall/migrate/compile
  - "migration-archive-list"  # migration archive read
  - "migration-archive-get"
  - "agent-def-*"             # agent definition CRUD (list/get/create/delete)
  - "update_agent_definition" # the agent-def update verb (not covered by the glob)
  - "agent-list"              # runtime agent read/write
  - "agent-get"
  - "agent-create"
  - "update_agent"
  - "agent-delete"
  - "agent-list-available"    # enumerate other agents (context)
  - "mcp-server-*"            # MCP registry CRUD + inspect
  - "update_mcp_server"
  - "toggle_mcp_server_tool"
  - "sync_mcp_server_tools"
  - "search_mcp_registry"
  - "mcp-registry-get"
  - "mcp-registry-install"
  - "provider-*"              # provider list/config/models/usage/test
  - "embedding-*"             # embedding status/pause/resume/config
  - "token-*"                 # token list/create/get/revoke
  - "document-*"              # document list/get/upload/delete
  - "project-get"             # graph: read-only (no entity/relationship writes)
  - "project-briefing"
  - "entity-type-list"
  - "entity-query"
  - "entity-search"
  - "entity-history"
  - "entity-edges-get"
  - "relationship-list"
  - "graph-traverse"
  - "tag-list"
  - "search-*"                # search-hybrid/semantic/similar/knowledge
  - "ask_user"                # proposal checkpoint (explicit opt-in)
  - "set_session_title"

config:
  agentType: operator

systemPrompt: |
  You are Memory's general assistant — the single operator for the whole
  platform. You configure every setting: agents, skills, blueprints/schema,
  MCP servers, providers, tokens, and documents. You inspect the knowledge
  graph read-only. You do this by talking to the user in the chat and, when a
  change is needed, proposing it before you make it.

  ## Operating principles

  1. Human time is scarce; agent work is cheap. Design thoroughly, but never
     mutate state without an explicit user checkpoint.
  2. Propose before write. Present the concrete change (the resource you will
     create, the fields you will set, or the diff you will apply) and pause via
     ask_user with "Accept", "Reject", and — when there is a genuine choice —
     named alternatives. Never call a write tool before the user accepts.
  3. Read before you write. Inspect current state first (agent-def-get,
     skill-get, schema-*, entity-query) so proposals are grounded, not
     invented. Do not overwrite existing configuration you have not read.
  4. Model the intent, not the ask. If the user asks for something underspecified,
     ask one focused clarifying question before proposing.
  5. A rejected proposal is information, not failure. Revise and re-propose, or
     abandon if the user declines.

  ## Workflow

  ### Understand
  1. Read the request. Determine what setting(s) it touches.
  2. Inspect current state with the read tools (agent-def-list/get,
     skill-list/get, schema-*, mcp-server-list, entity-query, search-*).
  3. If ambiguous, ask_user for the missing detail.

  ### Propose
  4. Draft the concrete change as structured content: the full YAML/JSON of a
     skill, agent definition, or schema; the exact entity/relationship to write;
     or a field-by-field diff of an update.
  5. Present it to the user via ask_user with interaction_type "buttons" and
     options [Accept, Reject, <named alternative…>]. Put the proposed content in
     the question text (fenced code block) so the card can render a preview.
  6. Invite free-text revision: tell the user they may reply with edits instead
     of clicking a button.

  ### Apply
  7. On "Accept", call the corresponding write tool exactly as proposed.
  8. Confirm the result by re-reading the resource (e.g. skill-get after
     skill-create) and report what changed and its id.

  ### Emit (optional)
  9. When the user asks for something reusable, also emit the standalone
     definition (the YAML they could save and re-install), mirroring
     blueprint-architect's "emit the blueprint" step.

  ## Guardrails

  - Do not invent tool names — use only tools actually granted to you.
  - Do not delete or disable anything without an explicit accept on a proposal
    that names the resource and its consequences.
  - Do not change the assistant's own definition, tools, or model without asking.
  - Prefer keyed/idempotent writes so a re-proposal does not duplicate resources.
  - You never create, update, or delete entities or relationships — graph data
    is outside your scope. You configure the platform that stores it. If asked
    to write graph objects, explain that you inspect the graph read-only and
    propose the schema/agent configuration that would support the change.
  - When in doubt about the domain, ask. A wrong schema or agent is costlier
    than a clarifying question.
```

### Design notes

- **Tool grant is config-scoped and least-privilege.** `skill-*`, `schema-*`
  (+ `migration-archive-*`), and `agent-def-*` (+ `update_agent_definition`)
  cover the three core skills (skill creation, blueprint creation, agent
  configuration); `mcp-server-*`/`provider-*`/`embedding-*`/`token-*`/`document-*`
  cover the rest of the platform. Graph access is **read-only**: `entity-query`,
  `entity-search`, `entity-history`, `entity-edges-get`, `entity-type-list`,
  `relationship-list`, `graph-traverse`, `tag-list`, `search-*` — never
  `entity-create`/`relationship-create` or any graph write verb. `remember` and
  `forget` are deliberately excluded (graph-write scope).
- **`ask_user` is the checkpoint.** It is not in the default tool pool — an
  agent must list it to receive it (`executor.go` `buildAskUserTool`). When the
  agent calls it, Memory pauses the run (`input-required`) and records a
  `kb.agent_questions` row; the gateway rewrites the `ask_user` tool call into a
  `question` SSE event that `chat.js`/`sidepanel.js` render as an interactive
  card (`renderQuestion`), so the accept/reject UX is live. The answer is
  POSTed back to Memory's
  `POST /api/projects/{projectId}/agent-questions/{questionId}/respond`, which
  resumes the run in the background — the gateway relays the JSON result (not an
  SSE stream) and the client re-reads the transcript once the resumed run lands.
- **Model.** `deepseek-v4-pro` (`openai/deepseek-v4-pro` via LiteLLM). v4-pro
  supports tool calling; the earlier failure (`Thinking mode does not support
  this tool_choice`) was a memory bug — `isReasonerModel` omitted `deepseek-v4-pro`,
  so memory sent an explicit `tool_choice` that v4-pro's thinking mode rejects.
  Fixed in memory (PR #335, `reasonerKeywords` → `deepseek-v4-`) and deployed.
- **Verification step (roadmap P1):** confirm the exact tool names against the
  live `tools/list` before finalizing the grant — the document-ingestion glob
  and any newer admin verbs must be validated, never assumed.

## Proposal / accept flow

Two modes, both already supported by the chat loop:

### A. Structured proposal (ask_user card)

The assistant calls `ask_user` with a fenced-code-block question and
`Accept` / `Reject` / named-alternative options. The gateway synthesizes a
`question` event from the `ask_user` tool call and the UI renders the question
card; the user clicks one option, the gateway
`POST /api/chat/questions/:id/respond` proxies to Memory's
`agent-questions/:id/respond`, Memory resumes the run in the background, and the
client re-renders the transcript once the resumed run completes.

```
user: "create a skill that greets me in the morning"
  → assistant inspects existing skills, drafts content
  → ask_user("Proposal — skill 'morning-greeting':
      ```yaml …```", options=[Accept, Reject])
  → [user clicks Accept]
  → assistant: skill-create(…) → skill-get(id) → "Done: skill <id>"
```

### B. Conversational proposal (free text)

For open-ended asks ("design an agent that helps me plan my week"), the assistant
ends its turn with a markdown proposal and asks a plain question. The user replies
with free text in the normal composer — edit, reject, or "go ahead". This is the
"we should have a chat about it" path: the proposal is refined over multiple turns
before any write.

**v1 uses both**; the assistant's prompt decides which fits. No new backend is
required for either.

### C. Richer proposal card (UI enhancement, P2)

The existing `ask_user` card renders a text question + options. To make proposals
first-class, extend the card to recognise a convention (fenced `yaml`/`json` block
in the question) and render it as a **proposal card**: syntax-highlighted preview,
scope badges (which setting type), and primary `Accept` / secondary `Reject` /
`Edit…` actions (Edit feeds back as free text). This is presentation-only — it
reuses `questionId` + `/respond` unchanged.

## Tool approval policy (enforcement)

The proposal/accept flow above is **prompt-level** — the operator's system prompt
says "propose before write", which is advisory, not enforced. Tool approval policy
is the **enforcement layer**: a per-tool gate the memory executor checks before
dispatching a tool, independent of the model's behaviour.

- **Policy per tool** — `allow` (run silently), `ask` (intercept + require
  approval), `deny` (block). An agent definition carries `ToolPolicies`
  (per-tool overrides) and a `DefaultToolPolicy` (fallback for unlisted tools).
- **UI** — agent → Settings → Tools: each tool row has an on/off toggle and a
  policy dropdown (Inherit / Allow / Ask / Deny); a "Default approval" select
  sits above the groups. The dropdown is disabled/dimmed while the tool is off.
- **Interception** — when an `ask`-policy tool is called, the executor pauses the
  run, emits an in-stream `approval` event, and records a pending
  `agent_tool_approvals` row. The gateway renders an approval card (tool + args)
  in the chat / side panel.
- **Decision** — Approve (execute + real result), Reject (structured
  `{policy_decision:"rejected", reason}` — the model stops that direction, not a
  retryable error), or Cancel (`not_taken`). Decisions resume the run exactly
  once (batch-safe for parallel tool calls).
- **Audit** — every decision lands in `agent_tool_approvals`; `/settings/approvals`
  lists them (real args, secret keys redacted) with Approve/Reject/Cancel actions
  for pending ones. Each row links back to the chat session that produced it
  (`conversationId` resolved via `run_id → acp_session_id → conversation`).

## UI integration

### 1. Dedicated assistant surface (P2)

- New sidebar entry **"Assistant"** (icon `lucide--sparkles`) under the existing
  "Agents" group, routing to `/assistant`.
- `/assistant` renders the **existing chat workspace** (`chatWorkspace`) with the
  agent picker preselected to the `operator` agent, and assistant-specific
  welcome chips ("Create a skill", "Review my agents", "Design an agent for …").
- No new chat plumbing — it reuses `/api/chat`, `/api/chat/questions/:id/respond`,
  the session rail, and streaming.

### 2. Contextual "Ask Memory" (P3)

On each settings/detail page (agent, skill, blueprint, document, object), add an
"Ask Memory" affordance that opens `/assistant?agent=<id>&prompt=<about this …>`.
The `prompt` pre-fills (and auto-sends) a scoped message, e.g.
"Review the agent <name> and suggest improvements." This makes the assistant a
per-page copilot without a bespoke panel.

### 3. Floating drawer (P4, optional)

A persistent "Memory" button that opens a right-side drawer with the assistant
chat, available from every page. Same backend, extra chrome only. Defer until the
dedicated surface proves out.

**Shipped session model (drawer):** the drawer (`gateway/sidepanel.templ` +
`sidepanel.js`) persists the *in-flight* conversation in `localStorage`
(`memory.sidepanel.v1`), while the session list comes from the backend
(`GET /api/conversations`, filtered client-side to the assistant agent). A
header dropdown shows the 3 most recent sessions (expandable) and can resume one
(`GET /api/conversations/:id/history`) — selecting a session sets its
`conversationId` so subsequent turns continue it. A contextual launch
(`seed()`, e.g. "Add with assistant" on a skills page) always starts a **new**
conversation; the top-bar toggle icon only opens/closes the drawer.

## Decisions

| # | Decision | Status |
|---|---|---|
| A1 | Agent name | **Decided — `operator`** (blueprint dir `operator/`) |
| A2 | Model | **v4-pro** — `openai/deepseek-v4-pro` (active) |
| A3 | Proposal surface | **v1 = structured `ask_user` + free text; P2 = proposal card** |
| A4 | Blueprint delivery | **Decided — extend Memory's bundled-blueprint format to carry agent definitions.** A pure-agent blueprint (no schema) installs its agents. |
| A5 | Scope of "all settings" | **Agents + skills + blueprints/schema + MCP registry + providers + tokens + embeddings + documents; graph read-only.** No entity/relationship writes, no `remember`/`forget`. |
| A6 | Tool approval enforcement | **Shipped — per-tool allow/ask/deny (`ToolPolicies` + `DefaultToolPolicy`), approval card, and audit view.** A hard tool gate that makes the prompt-level propose→accept enforceable. |

## Status

- **P0–P1 (shipped):** `gateway/blueprints/operator/` — a pure-agent blueprint
  (`project.yaml` + `agents/operator.yaml`). The bundled-blueprint loader now
  reads `agents/`, and `installBundledBlueprint` creates agents idempotently.
- **Test (shipped):** `TestOperatorBlueprintDefinesOperatorAgent` asserts the
  operator's tool grant includes `ask_user` and its system prompt mandates
  propose→accept — the definitional contract that a chat turn must emit a
  "question with a suggestion to confirm". `TestInstallOperatorBlueprintCreatesAgentNotSchema`
  and `TestInstallOperatorBlueprintIdempotent` verify install behaviour.
- **Not yet verified against live memory:** the exact tool names (document-ingestion
  glob, any newer admin verbs) must be confirmed against a running `tools/list`
  before the grant is trusted in production. The tool names here were cross-checked
  against Memory's `agentcompat` `internalToolNames` set and the `toolRequiredScope`
  map (graph:read vs graph:write); verify once more against a live `tools/list`
  before the first production install.

- **Tool approval (shipped):** memory-side policy engine (allow/ask/deny +
  default, in-stream `approval` event, `agent_tool_approvals` audit table,
  structured reject/cancel, batch-safe resume) + gateway UI (per-tool toggle +
  policy dropdown in the tools picker, approval card in chat/side panel,
  `/settings/approvals` audit view with approve/reject/cancel + session link).

## Roadmap

| Phase | Work | Deliverable |
|---|---|---|
| P0 | This spec | `14-assistant-agent.md` |
| P1 | Author the agent definition; verify tool names against live `tools/list` | **Done** — bundled `operator` blueprint + gateway tests |
| P2 | `/assistant` route + nav entry + proposal-card rendering; smoke-test the create-skill accept flow | Dedicated assistant UI |
| P3 | "Ask Memory" deep-links on settings pages | Contextual copilot |
| P4 | (Optional) floating drawer + proactive suggestions | Ubiquitous assistant |

## Verification (each phase)

1. `go build ./...`
2. `templ generate` (if `.templ` changed)
3. `task lint`
4. Server restart + browser smoke test of the accept/reject flow (create a skill,
   reject it, re-accept with an edit).
