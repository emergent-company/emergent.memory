## Context

Three tool-list notions already exist and must not be conflated:

1. `sandbox.ValidToolNames` (`mcp/sandbox_config.go:22`) — config keys
   (`bash`, `read`, …) for the sandbox allowlist. **Not** LLM-facing names.
2. Agent-facing workspace tool names — `workspace_bash`, `workspace_read`,
   `workspace_glob`, … (`agents/workspace_tools.go:107`).
3. MCP/registry tool names — `entity-search`, `schema-migrate-execute`, `remember`,
   relay names `<instance>_<tool>`, external prefixed names.
   `sandbox.ValidToolNames` appears in group code must use (2), not (1).

Existing taxonomies:

- `mcp/share_instance.go:toolCategoryDerived` — maps `RequiredScope` to a display
  string (Graph / Schema / Branches / Journal / Skills / Documents / Agents / Data /
  Projects / Chat / Admin / Other), falling back to the tool-name prefix before the
  first `-` for unscoped tools. Written for the share-instance catalog; ordering is by
  `(Category, Name)`.
- `mcp/service.go:toolRequiredScope` — package-private `map[toolName]scope`.

## Goals / Non-Goals

**Goals**

- One server-owned capability taxonomy, reused for policy resolution and UI rendering.
- Group policy without a schema migration, resolved at the single existing authorization
  point.
- Group enable/disable consistent with existing per-tool semantics.
- Deterministic, unit-testable resolution; no new network dependency for the UI.

**Non-Goals**

- New DB columns or tables.
- Group policy on external MCP-server / relay-node sections (deferred; see Non-goal in
  proposal).
- Changing the approval-pause/conversation mechanics — groups only choose *which*
  policy applies.
- Grouping skills or delegation targets; those have their own subpages and their own
  pickers.

## Decisions

### D1 — Primary axis: capability domain, source as sub-group

Chosen over source-only and effect-class-only. Rationale: policy statements are
capability statements ("all schema migrations need approval"), and the server already
derives the capability per tool from `RequiredScope` — so membership is automatic and
new tools inherit their group's policy without a fan-out write. Source (MCP server,
relay node) is retained as a collapsible sub-group so nothing regresses.

Effect class is used only to seed each group's suggested default policy, not as the
grouping key: it is too abstract to browse.

### D2 — Storage: reserved `@group:<id>` keys in the existing jsonb

`tool_policies` stays `map[string]ToolPolicy`. Group entries use the reserved prefix
`@group:`:

```json
{
  "@group:graph-write": { "confirm": true },
  "@group:schema-migrate": { "confirm": true },
  "entity-delete": { "confirm": false }
}
```

Tool names are validated identifiers that cannot begin with `@`, so the prefix cannot
collide. Alternatives rejected:

- **Fan-out on save** (write a policy per member tool): stale — a newly registered MCP
  tool is not covered until the agent is re-saved. This is the failure mode the group
  layer exists to fix.
- **New column `tool_group_policies`**: requires a Goose migration plus a second
  resolution field, for no expressive gain.

### D3 — Resolution order

`AgentDefinition.effectiveToolPolicy(tool)` (`agents/entity.go:388`) becomes:

1. `tool_policies[tool]` — explicit per-tool override (unchanged, wins).
2. `tool_policies["@group:"+GroupForTool(tool)]` — group policy.
3. `default_tool_policy` — fallback (unchanged).

`effectiveToolPolicy` returns `(ToolPolicy, bool)` where the bool is false for the
"allow, non-disabled" result. The group branch preserves that contract so
`executor.go:1943` needs no change.

Group ids are stable slugs, never display labels. Resolving a tool that belongs to no
known group falls through to (3) — `other` exists for display, not to force a policy.

### D4 — Enable/disable is membership, not policy

A group policy is an approval requirement; it does **not** remove tools. Enabling and
disabling are membership operations, mirroring the existing per-tool form semantics in
`gateway/agent.go:392` (`applyAgentToolsSection`):

- **enable group** → every member added to `def.Tools`, removed from `def.BannedTools`.
- **disable group** → every member removed from `def.Tools`, added to
  `def.BannedTools`.

`BannedTools` is already enforced as a hard filter after tool resolution
(`executor.go:1674`), so a disabled group stays disabled even if a later tool-list
change reintroduces a member. `disabled: true` in a group policy is a separate,
weaker thing (the executor returns an error to the model instead of removing the tool
from its context) and is offered as the "Deny" value of the group policy control.

### D5 — Server owns taxonomy, gateway owns writes

The gateway never hardcodes the capability taxonomy. The agent definition read DTO
gains a computed `toolGroups` array; the gateway renders what it receives and writes
group form fields back. This keeps membership derivation in one place and lets the
gateway lane compile against the frozen JSON shape while the server lane lands
independently (the gateway's `memory.go` DTOs are local mirrors, so there is no
compile-time coupling).

Fallback: if the server returns no `toolGroups` (older server), the panel renders
today's source-only grouping unchanged.

### D6 — Frozen contract

Group ids (slugs) and the JSON shape both lanes code against:

```
search, graph-read, graph-write, schema-read, schema-write, schema-migrate,
branches, journal, documents, skills, agents, projects, chat, admin,
workspace-read, workspace-exec, web, other
```

The `web` group covers the web MCP tools (`web-search-brave`, `web-fetch`,
`web-search-reddit`) — "Web search and fetch." The Google-native tools
(`google_search`, `url_context`, `code_execution`) are **out of scope** for
group policy: they are configured under `Model.NativeTools` and injected
directly into `genConfig.Tools` in the executor, bypassing the tool-policy
callback, so they belong to no group. `other` is display-only — it never carries
a group policy, and external/relay tools fall through to the default.

```json
"toolGroups": [
  { "id": "graph-write", "label": "Graph · Write",
    "description": "Create, update, or delete graph objects.",
    "policy": "ask", "enabled": true,
    "tools": ["entity-create", "entity-delete", "remember"] }
]
```

`policy` is `""` (inherit default), `"allow"`, `"ask"`, or `"deny"`. `enabled` means
at least one member is in `Tools` and not banned.

Form field names:

- `groupPolicy.<groupId>` → `allow` | `ask` | `deny` | `inherit`
- `groupEnabled.<groupId>` → `on` (present) / absent

## Risks / Trade-offs

- **Taxonomy drift from `toolCategoryDerived`.** The share-instance catalog and the
  agent group registry would otherwise grow two divergent maps. Mitigation: the new
  `toolgroups` package exports the scope→group mapping and `mcp`'s
  `toolCategoryDerived` is left as-is for the catalog (its output is a display string
  for a different surface); a unit test asserts every scope in `toolRequiredScope` maps
  to a known group, so a new scope cannot silently fall into `other`.
- **Silent group vanishes on tool rename.** Covered by the same test: unknown names fall
  to `other` and are still rendered and controllable.
- **Panel size.** Group headers add chrome to a page that can already list hundreds of
  tools. Mitigation: groups collapse; a group with no members in the project's catalog
  is not rendered; active groups default open (same rule as `mcpServerInUse` today).
- **Group policy vs `default_tool_policy` confusion.** Resolution order is documented in
  the panel itself: each group header shows the effective value with an explicit
  "Inherit" option rather than a blank state.
