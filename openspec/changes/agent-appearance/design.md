## Context

Object types already support a user-chosen icon and color: `kb.project_object_schema_registry` carries a `ui_config jsonb` blob holding `{"icon":"<bare-kebab-lucide-name>","color":"<#RRGGBB>"}`, the object-type editor uses go-daisy's `ui.IconPicker` / `ui.ColorPicker` fed by `supportedIconPickerOptions()` and `schemaColorPresets`, and every object surface renders the pair through the `typeGlyph` / `typeIconTile` / `typeNameChip` / `typeColorStyle` primitives (with `lucide--box` as the object-type default). Agents have none of this: every agent renders the same neutral `lucide--bot` tile, so agents are visually indistinguishable. This change mirrors the object-type appearance machinery onto agent definitions. See proposal.md for motivation; specs/agent-appearance/spec.md for requirements.

## Goals / Non-Goals

**Goals:**

- Store a user-chosen icon + color per agent definition in a `ui_config` JSONB column, shaped exactly like object types.
- Expose it through the agent DTOs (full + summary + create + update) as camelCase `uiConfig`.
- Let the Web UI edit agent appearance with the exact object-type pickers, and render it on every agent-visible surface with the existing glyph primitives, falling back to `lucide--bot` when unset.
- Support `ui: {icon, color}` in blueprint agent manifests; the CLI flags that would also consume it are deferred until the next SDK module tag (D6).

**Non-Goals:**

- iOS app rendering of agent icon/color (follow-up).
- CLI terminal rendering of icon/color (follow-up).
- Changing the object-type appearance machinery itself — agents reuse it verbatim.

## Decisions

### D1 — Mirror object types: a `ui_config` JSONB blob, not dedicated columns

Store appearance as a single `ui_config jsonb NOT NULL DEFAULT '{}'` column on `kb.agent_definitions`, holding `{"icon":"<bare-kebab-lucide-name>","color":"<#RRGGBB>"}`. Both keys optional; `{}` = defaults. This is the exact approach `kb.project_object_schema_registry.ui_config` uses, so the server model, DTO mapping, and UI pickers can share conventions and no per-field column/validation machinery is needed.

*Alternatives considered:* dedicated `icon text`/`color text` columns — rejected, breaks parity with object types and complicates the "both optional, `{}` = defaults" semantics the UI already understands.

### D2 — `uiConfig json.RawMessage` on all four agent DTOs, camelCase

`AgentDefinitionDTO`, `AgentDefinitionSummaryDTO`, `CreateAgentDefinitionDTO`, and `UpdateAgentDefinitionDTO` each gain `uiConfig json.RawMessage json:"uiConfig,omitempty"`, matching the existing agents-domain tag convention (camelCase, e.g. `systemPrompt`, `toolPolicies`). The summary DTO MUST carry it because the list/picker surfaces (dashboard, schedules, ⌘K, blueprint rows) render from summaries; without it those surfaces could not show appearance.

*Alternatives considered:* raw `map[string]any` or a typed `{Icon,Color}` struct — rejected; `json.RawMessage` matches how `ProjectObjectSchemaRegistry.UIConfig` is modeled and round-trips the arbitrary `{icon,color}` blob losslessly.

### D3 — Reuse the object-type pickers and glyph primitives unchanged

The agent editor uses `ui.IconPicker` + `ui.ColorPicker` with the same `supportedIconPickerOptions()` catalog and `schemaColorPresets` palette. The icon catalog gains `lucide--bot`, and the agent default/fallback glyph is `lucide--bot` (object types fall back to `lucide--box`). Rendering uses `typeGlyph`/`typeIconTile`/`typeNameChip`/`typeColorStyle`, so icon + color are tinted identically to object types with no new rendering code.

*Alternatives considered:* a separate agent-specific picker/primitive set — rejected, duplicates the object-type code and risks visual drift.

### D4 — Fallback is a neutral `lucide--bot` tile, not an error or hide

Where icon/color are unset, surfaces render today's neutral `lucide--bot` tile. No visual regression, no conditional rendering branches — the primitive already falls back cleanly when given an empty icon.

### D5 — Blueprint `ui` parity via a typed `AgentManifest.UI`

`AgentManifest` gains a typed `UI *AgentUIManifest` (`{Icon, Color string}` with `json`+`yaml` tags, `omitempty`) mirroring `ObjectTypeDef.UI`; apply marshals it into `UIConfig` on create/update (only when at least one value is non-empty, so create keeps the DB default `{}` and update preserves an existing appearance). The gateway's `BundledAgent`/`blueprintAgent` reuse the same `{icon,color}` shape, and the CLI blueprint `AgentFile` gets it in the follow-up slice.

*Alternatives considered:* raw `map[string]any` — rejected in favour of a typed block so the manifest contract is explicit and both the server and gateway can share the shape.

### D6 — CLI flags deferred to the next SDK release

`memory agent-definitions create/update --icon/--color` (and the CLI blueprint-applier `ui` passthrough) are **not** in this change. They consume the new `uiConfig` field on the SDK's `agentdefinitions` types, but `apps/cli` is intentionally built against the *published* SDK module: `.github/workflows/cli.yml` runs with `GOWORK: off` and `apps/cli/go.mod` pins `sdk v0.82.0`. SDK module tags are only created on release tags (`server-sdk.yml` → `apps/server/pkg/sdk/<tag>`), so a CLI slice that references an unreleased SDK field cannot pass CI in the same PR. The CLI work ships as a follow-up once the next SDK tag exists and `apps/cli/go.mod` is bumped (no relative `replace` added to `apps/cli/go.mod` — the pinned-SDK check is deliberate release hygiene).

## Risks / Trade-offs

- [Icon name validation] → icon is a bare kebab-case Lucide name; the UI catalog constrains choices, but the API/CLI accept arbitrary strings. Mitigation mirrors object types (the primitive renders an unknown class as nothing/neutral), so an invalid icon degrades to the neutral tile rather than erroring.
- [Color parsing] → color is a hex string; malformed values are persisted but render as no tint (the primitive only applies valid hex). Acceptable, same as object types.
- [Summary DTO size] → adding `uiConfig` to the summary is a small payload increase on list endpoints; justified because lists/pickers must render appearance without a full fetch.
- [Backfill] → existing rows default to `{}` via `DEFAULT '{}'`; no backfill needed, no visual change for existing agents.

## Migration Plan

Additive: one Goose migration `00155_add_ui_config_to_agent_definitions.sql` adding `ui_config jsonb NOT NULL DEFAULT '{}'` to `kb.agent_definitions` (default ensures existing rows are `{}`), mirrored into the integration-test fixture schema (`apps/server/internal/testutil/schema.sql`). Server entity/DTO changes, blueprint manifest `ui`, and Web UI picker/rendering changes are all additive. Rollback = revert the change; the column is dropped by a down migration.

## Open Questions

- Whether iOS rendering (and the deferred CLI slice) of agent icon/color should be tracked as separate capability deltas or folded into this change's archive follow-ups.
