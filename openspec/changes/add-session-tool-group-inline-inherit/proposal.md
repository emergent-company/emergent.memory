## Why

The capability taxonomy behind the agent Settings → Tools picker has no group for
session-scoped tools. `session-todo-list` and `session-todo-update` are unscoped
catalog tools, so they fall into the display-only `other` group, and
`set_session_title` — an always-injected hidden builtin — appears in no group at
all, so it cannot be turned on or off from the picker even though the executor
injects it on every run.

Separately, per-tool inheritance is surfaced as a second, non-interactive hint
label beside every policy select ("Inherits Graph · Write · Ask"). It duplicates
information the select could carry, adds a second element to scan on every row,
and collapses awkwardly on narrow screens. The inherited value belongs in the
`Inherit` option itself.

## What Changes

- Add a `session` capability group (display label **Session**) to the server-owned
  taxonomy, owning `session-todo-list`, `session-todo-update`, and
  `set_session_title`.
- Render the always-injected hidden builtin `set_session_title` in the computed
  Session group: present by default and enabled unless banned. Its picker row maps
  to ban state (checked = not banned) and carries **no** per-tool policy select,
  because the server injects it regardless of the allowed-tools whitelist — banned
  tools is its only disable mechanism.
- Inline the inherited value into the `Inherit` option: `Inherit (Ask)`,
  `Inherit (Default)`. Remove the separate per-row inheritance hint label and its
  two responsive variants; only the dropdowns remain.
- Route the unscoped session-todo tools into Session through the static
  tool→group mapping so they leave the fallback group.

## Capabilities

### Modified Capabilities

- `tool-approval-policy`: a Session tool group, and a ban-managed hidden builtin
  whose enable state is the agent's banned-tools list rather than its allowed
  tools.
- `agent-dashboard-ui`: the inherited policy value is inlined into the policy
  select's `Inherit` option, and ban-managed rows render without a per-tool policy
  select.

## Impact

- **Server** (`apps/server/domain/agents/`): `toolgroups/toolgroups.go` (new group +
  static mappings), `dto.go` (`sessionBuiltinToolNames` membership injection and a
  default-on notion in `groupEnabled`).
- **Gateway** (`apps/web-ui/gateway/`): `agent.go` (inline inherit label, ban-managed
  row + write-path reconciliation), `agent.templ` (Inherit option label, hint spans
  removed), regenerated `agent_templ.go`.
- **Docs** (`apps/web-ui/docs/spec/14-assistant-agent.md`): document the Session
  group and the inlined inheritance label.
- **No DB migration**: groups remain `@group:<id>` keys in the existing
  `tool_policies` jsonb.
- Depends on the shipped tool-group layer (`add-agent-tool-groups`).
