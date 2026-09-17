## Why

The `extract-shared-ui-components` change introduced `components/` and migrated the twelve highest-leverage patterns, but it explicitly deferred a set of remaining duplicates. Those deferrals are now visible as the last hand-rolled copies of patterns that already have a shared home:

- A local `emptyDash()` still exists in `ui.templ` (line ~1035), duplicating `components.EmptyDash` (added in `components/meta.templ`) and called from `ui.templ`, `sessions.templ`, and `documents.templ`.
- The destructive-confirm icon circle (`bg-error/10 text-error grid size-10 … rounded-full` + trash/alert icon) is copy-pasted six times across five files.
- The definition-list grid (`<dl class="grid grid-cols-1 gap-x-6 … sm:grid-cols-2 …">`) is hand-rolled four times with two drifted gap values (`gap-y-3` vs `gap-y-4`) and an ad-hoc `lg:grid-cols-3`.
- Three raw `<dialog class="modal">` shells (`blueprints.templ`, `schema.templ`) bypass `modalShell` and re-declare the backdrop form and box conventions.
- Two raw bordered table shells in `agent_mcp_endpoint.templ` hand-roll `rounded-box border-base-200 border` instead of `components.TableCard` (the extract change deliberately left these, noting they "sit inside the endpoint's own rounded-box container"; that container is in fact the same bordered-list shell the component already renders).
- One raw `@ui.Badge` "Revoked" status badge in `api_tokens.templ` bypasses `components.StatusBadge`.
- Two raw `<optgroup>` loops in `migrations.templ` bypass `components.SelectOptionGroups`.
- A stale comment in `toast.go` claims go-daisy's `alpine.ToastQueueState` marshals a nil slice as `null`; upstream fixed this at the vendored pin.

Each remaining copy is a drift generator. This change finishes the adoption so each pattern has a single compiler-checked definition.

## What Changes

Nine edits, all markup-preserving (no route, handler, schema, or user-visible behavior change):

1. Delete the local `emptyDash` and repoint its four callers at `components.EmptyDash`.
2. Add `components.ConfirmIcon(icon string)` (defaults to the trash glyph via `cmp.Or`) and replace the six inline icon circles.
3. Add `components.MetaGrid(opts ...MetaGridOpts)` and replace the four definition-list grids, preserving each grid's exact class order and gap.
4. Convert the three raw `<dialog class="modal">` shells to `ui.templ` `modalShell`, preserving ids and `aria-*` wiring.
5. Convert the two raw bordered table shells in `agent_mcp_endpoint.templ` to `components.TableCard`, preserving `data-testid` and the margin.
6. Adopt `components.ToggleField` where its shape fits — the four audited toggle rows are all bare checkboxes (form-submit, property-editor, or `data-*` toolbar toggles) and stay local (see tasks).
7. Convert the two raw `<optgroup>` loops in `migrations.templ` to `components.SelectOptionGroups` via a call-site adapter.
8. Convert the raw "Revoked" `@ui.Badge` in `api_tokens.templ` to `components.StatusBadge`.
9. Replace the stale `toast.go` comment with an accurate one.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-ui-components`: extends the shared component package with `ConfirmIcon` and `MetaGrid`, and finishes adoption of the already-shared `EmptyDash`, `TableCard`, `modalShell`, `StatusBadge`, and `SelectOptionGroups`. No capability behavior changes; the delta records only the newly single-sourced markup and the no-regression requirement.

## Impact

- `apps/web-ui/gateway/components/` — new `confirm.templ`; `meta.templ` gains `MetaGrid` + `MetaGridOpts` (and an `cmp` import).
- `apps/web-ui/gateway/*.templ` — call-site swaps in `ui.templ`, `sessions.templ`, `documents.templ`, `mcp_shares.templ`, `agent_mcp_endpoint.templ`, `mcp_nodes.templ`, `project_settings.templ`, `backups.templ`, `org_members_ui.templ`, `schema_packs.templ`, `blueprints.templ`, `schema.templ`, `migrations.templ`, `api_tokens.templ`.
- `apps/web-ui/gateway/toast.go` — comment-only correction.
- Generated `*_templ.go` are gitignored — regenerate via `templ generate`, never stage.
- Net effect: ~90 duplicated lines removed; no behavior change.
