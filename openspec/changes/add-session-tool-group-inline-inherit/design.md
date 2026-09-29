## Context

The tool-group layer already exists: `toolgroups.Groups` is the ordered taxonomy,
`AgentDefinition.ToolGroupsWithCatalog` computes the per-agent `toolGroups` payload
(membership = catalog tools for the group ∪ the agent's allowed/banned tools, plus
the workspace names when a sandbox is configured), and the gateway renders it.

Two session-scoped tools are registered in the MCP catalog with no `RequiredScope`
(`session_todo_tools.go`), so `GroupForScope("", name)` falls through to the static
map and then `other`. The session-title tool is different: `set_session_title` is a
hidden builtin injected by `ToolPool.ResolveTools` **regardless of the Tools
whitelist**; the only opt-out is `BannedTools`.

The picker previously communicated inheritance with a separate hint span beside the
policy select.

## Goals / Non-Goals

**Goals**

- One Session group owning all three session tools, rendered by the existing
  machinery with no new DTO shape.
- The session-title builtin is enable/disable-able from the picker and its state
  round-trips through `BannedTools`.
- Inheritance is carried by the dropdown itself; no second element per row.

**Non-Goals**

- No change to the executor's injection of `set_session_title`.
- No new DB column or table.
- No change to the resolution order (explicit tool → `@group:<id>` → default).
- No group *policy* semantics for the session-title builtin (it bypasses the policy
  callback), so it gets an enable control only.

## Decisions

### D1 — Session group membership for the hidden builtin is injected only with a catalog

`set_session_title` is neither a catalog tool nor stored in `def.Tools`, so it must
be appended to the Session group explicitly (mirroring `workspaceToolNames`). The
injection is gated on a non-empty catalog so the nil-catalog fallback stays
strictly agent-referenced-tools — otherwise every bare definition would suddenly
report a Session group and the "toolGroups omitted when empty" contract would
break. Rejected alternative: include it unconditionally.

### D2 — A default-on notion in `groupEnabled`

A group reports enabled when at least one member is in `Tools` and not banned. For
the always-injected builtin that is wrong: it is neither in `Tools` nor banned, yet
active. `groupEnabled` therefore accepts a `defaultOn` set (built from
`sessionBuiltinToolNames`) and treats such a member as enabled when it is not
banned. Rejected alternative: special-case the `session` group id — the set-based
form stays generic and covers any future always-injected builtin.

### D3 — Ban-managed rows in the gateway

A hidden builtin's row checkbox maps to ban state (`checked = !banned`), the write
path reconciles `BannedTools` against the submitted `tool` membership when the
Session group was rendered (`groupWasEnabled.session` present), and the row omits
the per-tool policy select (`NoPolicy`). Rejected alternative: keep rendering it as
an ordinary whitelist row — unchecking then has no effect, a silent lie.

### D4 — The `Inherit` option carries the inherited value

`Inherit (<value>)` where `<value>` is the owning group's policy when set, else the
agent's default policy, else `Default`. The hint span, its helpers, and its two
responsive variants are deleted. Rejected alternative: keep the hint and only
shorten it — the whole point is to stop rendering two elements per row.

## Migration

None. Group policies remain `@group:<id>` entries in `tool_policies` (`@group:session`
is simply a new key). `BannedTools` already exists.
