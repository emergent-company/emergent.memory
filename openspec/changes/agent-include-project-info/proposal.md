## Why

Project-level context (`kb.projects.project_info`) is injected into an agent's
system prompt **unconditionally** and on **one surface only**: the
OpenAI-compatible `/v1/chat/completions` surface fetches it in
`domain/agentcompat/service.go` and prepends a `## Knowledge base context`
block via `SystemPromptAppendix`. Every other surface that runs through
`AgentExecutor.resolveInstruction` — the trusted HTTP trigger, A2A
`message:send`, and chat sessions — never injects it. The same agent therefore
behaves differently depending on how it was invoked, and operators have no way
to choose whether the project-wide description reaches the agent at all
(combined with a project-specific `systemPrompt`, the KB description can even be
duplicated).

## What Changes

- Add a per-agent-definition opt-in flag `includeProjectInfo` (boolean,
  **default `false`**), stored in the existing `kb.agent_definitions.config`
  JSONB blob — no migration.
- Move `project_info` injection into `AgentExecutor.resolveInstruction` so it
  is applied **identically on every surface** (HTTP trigger, A2A, chat sessions,
  and `/v1/chat/completions`), routed through one shared composition function.
- Gate injection on the flag: when `includeProjectInfo` is `false` (the
  default), `project_info` appears on **no** surface — including the
  `/v1/chat/completions` surface, whose unconditional fetch/injection is
  removed. When `true`, it appears on **all** surfaces in the same position with
  the same text.
- Expose the flag through the agent-definition REST API (create/update request
  DTOs and the read DTO) and through the agent **General settings** form in the
  web UI.
- `SystemPromptAppendix`'s existing behaviour for other callers (caller-supplied
  per-request constraints) is preserved unchanged; it simply no longer carries
  `project_info`.

## Capabilities

### New Capabilities

- `agent-context-injection`: how project-level context (`kb.projects.project_info`)
  is composed into an agent's system instruction — opt-in per agent definition,
  identical across all execution surfaces, and byte-stable for a given
  agent/flag.

### Modified Capabilities

- `agent-definitions`: the write/read contract gains the per-agent
  `includeProjectInfo` opt-in flag (config key `includeProjectInfo`, default
  `false`), surfaced on create, update, and read.
- `web-agent-settings`: the agent General settings form gains an
  `includeProjectInfo` control that persists the flag.

## Impact

- `apps/server/domain/agents/executor.go` — `ExecuteRequest.ProjectInfo`, a
  one-shot fetch in `runPipeline`, and the `resolveInstruction` injection point.
- `apps/server/domain/agents/entity.go` — `AgentDefinition.IncludeProjectInfo()`
  reader and the canonical config key.
- `apps/server/domain/agents/dto.go` + `handler.go` — `includeProjectInfo` on
  the read/create/update DTOs and its mapping.
- `apps/server/domain/agentcompat/service.go` — remove the unconditional
  `GetProjectInfo` + `buildSystemAppendix` injection.
- `apps/web-ui/gateway/agent.go` + `agent.templ` — General form checkbox and
  mapping.
- No database migration. No new dependencies. No breaking API change: absent
  `includeProjectInfo` preserves today's *fully-off* behaviour on every surface
  (which is a behaviour change only for `/v1/chat/completions`, where
  `project_info` previously always appeared — intended by this change).

## Follow-up (not in scope)

The issue suggests generalising this into a set of opt-in context sources
(`project_info`, project briefing, skills/`autoLoadSkills`). This change
implements exactly one source (`project_info`) and leaves the general
context-source selector as a documented follow-up.
