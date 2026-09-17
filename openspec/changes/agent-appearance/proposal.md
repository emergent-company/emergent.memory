## Why

Agent definitions are visually indistinguishable today: every agent renders the same neutral `lucide--bot` tile wherever it appears in the Web UI — list cards, the dashboard header, session/chat author bubbles, schedules, ⌘K spotlight, blueprint rows. Object types already let the owner choose an icon and color (`ui_config` on `kb.project_object_schema_registry`), so a project's objects have a distinct visual identity while its agents all look identical. This change gives agent definitions the same user-chosen icon + color, stored and rendered through the exact same primitives, so agents are as recognizable as object types.

## What Changes

- Agent definitions gain a `ui_config jsonb` column (`{"icon":"<bare-kebab-lucide-name>","color":"<#RRGGBB>"}`), mirroring the JSONB-blob approach object types use. Icon is a bare kebab-case Lucide name (e.g. `bot`, `file-text`); color is a hex string. Both optional; `{}` = defaults.
- Server DTOs (`AgentDefinitionDTO`, `AgentDefinitionSummaryDTO`, `CreateAgentDefinitionDTO`, `UpdateAgentDefinitionDTO`) gain `uiConfig json.RawMessage` (camelCase `uiConfig`, matching the agents-domain tag convention). The summary DTO carries it so lists/pickers render appearance without a full fetch.
- The Web UI editor (settings form + create/edit modal) reuses the object-type pickers — `ui.IconPicker` / `ui.ColorPicker`, `supportedIconPickerOptions()`, `schemaColorPresets` — so the agent icon/color editing experience is identical to object types. The icon catalog gains `lucide--bot`; the agent default/fallback glyph is `lucide--bot`.
- Every agent surface — list cards/rows, dashboard header + summary card, session/chat author bubbles, session row + detail header, schedules rows/detail, ⌘K spotlight, blueprint agent rows, and client-side chat-stream bubbles — renders the agent's icon + color via the existing `typeGlyph` / `typeIconTile` / `typeNameChip` / `typeColorStyle` primitives. Where icon/color are unset, they render today's neutral `lucide--bot` tile (no visual regression).
- Blueprint agent manifests may declare `ui: {icon, color}` and it is applied on create/update (parity with object types declaring `ui` in `object_type_schemas`).
- The CLI `memory agent-definitions create`/`update` gain `--icon` / `--color`.

## Capabilities

### New Capabilities

- `agent-appearance`: the ability to give an agent definition a user-chosen icon and color, persist it, and render that appearance everywhere the agent is visible in the Web UI.

## Impact

- Database: new migration adding `ui_config jsonb NOT NULL DEFAULT '{}'` to `kb.agent_definitions` (matching `kb.project_object_schema_registry.ui_config`).
- Server (`apps/server/domain/agents/`): entity gains `UIConfig`; DTOs + `ToDTO`/`ToSummaryDTO` map it; create/update handlers accept and persist it.
- Blueprints (`apps/server/domain/blueprints/`): `AgentManifest` gains `ui`, and apply maps it onto create/update (parity with `ObjectTypeDef.UI`).
- CLI (`apps/cli/internal/cmd/agent_definitions.go`): `--icon` / `--color` flags on create/update.
- Web UI (`apps/web-ui/gateway/`): agent editor uses `ui.IconPicker`/`ui.ColorPicker`; icon catalog gains `lucide--bot`; agent rendering surfaces switch to the type-glyph primitives with a `lucide--bot` default.
- Follow-up (out of scope for this change): iOS app rendering and CLI terminal rendering of icon/color.
