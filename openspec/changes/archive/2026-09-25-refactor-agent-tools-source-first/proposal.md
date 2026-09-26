## Why

The agent Tools picker (`/agents/:id/settings/tools`) groups by capability first and nests the *source* inside each capability group. Memory's native tools all come from one `builtin` server (~124 tools), so unfolding a capability such as "Search" reveals a redundant nested "builtin" group, then the tool — three levels to reach one checkbox. Meanwhile every user-added external MCP server and connector/relay node is buried under the display-only "Other" capability group instead of standing as a first-class source.

Source is the stronger mental model: a user reasons about "memory's own tools" versus "the MCP servers and connectors I added". Flipping the top-level axis to source removes the redundant `builtin` layer and surfaces external servers as peers of Built-in.

## What Changes

- Make **source** the top-level picker dimension.
- Add a top-level collapsible **"Built-in"** section holding the capability groups (Search, Graph · Read/Write, Schema · …, Web, Other, …). A capability group's member tools render as **direct rows** — the nested `builtin` server sub-group disappears.
- Render each **external MCP server** (stdio/sse/http) and each **relay/connector node** as a top-level sibling of Built-in, listing its own tools directly. Relay nodes keep the remote badge and stay enable-only; external MCP servers keep per-tool policy selects.
- Preserve behavior: capability groups keep their enable switch (`groupEnabled.<id>`) and tri-state policy select (`groupPolicy.<id>`); delegation tools stay owned by the Delegation subpage and excluded here; the uncovered fallback stays reachable so a taxonomy gap never drops a tool on save; the no-groups (older server) path falls back to the previous source-only grouping unchanged.
- Drop a capability group that would render an empty body (all members external) so the panel never shows an empty header.

No API, schema, or write-path change: `toolGroups` stays server-computed and read-only, group policies stay under `@group:<id>` in `toolPolicies`, and the same form fields are submitted.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `agent-dashboard-ui`: the Tools panel is grouped by source first (Built-in section + external/relay siblings) instead of by capability first with nested sources.

## Impact

- `apps/web-ui/gateway/agent.go` — source-first picker view-model builder (`buildToolPickerView`) replaces `buildToolGroupViews`/`groupedOtherRows`.
- `apps/web-ui/gateway/agent.templ` — Built-in section + capability-group/row templates; source blocks as siblings.
- `apps/web-ui/gateway/agent_ui_test.go` — grouped-picker tests updated; source-sibling coverage added.
- No server, API, schema, or migration change.
