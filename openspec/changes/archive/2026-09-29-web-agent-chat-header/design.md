# Design

## Reuse `description`, do not add a field

`kb.agent_definitions.description` already exists and round-trips through
`AgentDefinitionDTO`, `AgentDefinitionSummaryDTO` (`ToSummaryDTO`), the create
and update DTOs, and blueprints. The gateway's `AgentDefinition` also already
carries it and renders it as the agent dashboard subtitle — but the web editor
never exposed it and `AgentDefinitionSummary` drops it, so the chat page (which
receives only summaries) cannot see it. This change closes those two gateway
gaps rather than introducing a parallel `shortDescription`.

## Header contract (server ↔ script)

The header is server-rendered for the active agent so it is correct before
`chat.js` runs, then kept live by the client.

- Stable hooks on the header: `data-testid="chat-agent-header"`,
  `id="chat-header-icon"`, `id="chat-header-title"`,
  `id="chat-header-desc"`.
- `#chat-agent` options carry `data-icon`, `data-color` (existing) plus
  `data-description` (new); `chat.js` reads the selected option and mirrors
  icon/name/description into the header.
- Active agent resolution server-side: the conversation's agent when `?c=`
  names one, else the preselected/default agent.

## Untrusted text

`description` is user/agent-authored. It is rendered server-side through templ
(escaped), and in `chat.js` set via `textContent` — never `innerHTML`
(gateway `AGENTS.md` rule).

## Dynamic behavior

- Picker change (`#chat-agent`): update title/description and swap the icon.
- Session resume: `resumeConversation(id, agentId, …)` already switches the
  picker to the conversation's agent; the header update rides the same hook,
  so it needs no extra fetch.

## Fallback

No agents and no active conversation → keep today's `Chat with Memory` /
`Chat by text[ or voice].` copy so the empty/error path is unchanged.
