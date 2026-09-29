## Why

The chat workspace header is hardcoded to the generic `Chat with Memory` / `Chat by text.` block (`apps/web-ui/gateway/chat.templ`), so once a user picks an agent (or resumes a session) the pane gives no sign of which agent they are talking to. Every other agent surface already leads with the agent's icon (`agent-appearance`), and the backend already stores an agent `description` — but the gateway drops it from `AgentDefinitionSummary`, so no chat surface can render it.

Note: PR #565 (`fix/agent-icon-affordances`) added the agent glyph to the **session rail rows** and the **agent dashboard header** only. The chat pane header itself was never made agent-aware.

## What Changes

- The chat pane header renders the **active agent's** identity: its icon tile (`ui_config` icon/color, neutral `lucide--bot` when unset), its name as the title, and its stored `description` as the second line (hidden when empty).
- The header tracks the active agent live: it updates when the agent picker (`#chat-agent`) changes, and reflects the conversation's agent when a session is resumed.
- Reuse the existing agent `description` field — no new column, DTO, or migration. The gateway only stops dropping it: `AgentDefinitionSummary` gains `description`, and the agent General settings form gains an editable **Short description** input that persists it.
- With no agents and no conversation, the header falls back to the current generic copy (no crash, no empty header).

## Capabilities

### New Capabilities

- `web-chat-header`: the chat pane header renders the active agent's icon, name, and description, and tracks the active agent as the picker or session changes.

### Modified Capabilities

- `agent-appearance`: the "render appearance everywhere" surface list gains the chat pane header (icon tile for the active agent).
- `web-agent-settings`: the General settings form exposes the existing `description` as an editable short-description field.

## Impact

- `apps/web-ui/gateway/chat.templ` — agent-aware header (icon tile + name + description), ids/`data-testid` for the client hook; `#chat-agent` options gain `data-description`.
- `apps/web-ui/gateway/chat.js` — update the header from the selected option on picker change and on conversation resume.
- `apps/web-ui/gateway/agent.templ` — Short description field in the General form.
- `apps/web-ui/gateway/memory.go` — `AgentDefinitionSummary.Description`.
- `apps/web-ui/gateway/agent.go` — `applyAgentGeneralSection` maps `description`.
- Tests: gateway render/contract tests for the header and the field.
- No server, API, schema, DB, or blueprint change: `description` already round-trips through `AgentDefinitionDTO` / `AgentDefinitionSummaryDTO`.
