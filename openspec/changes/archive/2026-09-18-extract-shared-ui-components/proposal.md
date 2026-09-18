## Why

`apps/web-ui/gateway` holds 429 templ definitions across 34 files and 14,699 lines, all in a single `package main`. There is no component package: the de-facto shared kit (`pageHeader`, `modalShell`, `countBadge`, `listRow`, `emptyDash`, `monoMeta`) lives inside a *page* file (`ui.templ`), and `subNavItem` lives in `agent.templ` while five unrelated files import it. Concretely measured duplication:

- The MCP secret-reveal dialog is **copy-pasted verbatim** (83 lines) between `agent_mcp_endpoint.templ:552-634` and `mcp_shares.templ:397-479`; `api_tokens.templ:427-468` is a third inline variant of the same anatomy.
- The key/value `<dt>/<dd>` detail row is implemented four times (`backups.templ:409`, `org_members_ui.templ:509`, `project_settings.templ:746`, `schema_packs.templ:161`) plus inlined ten more times — and the label class has **already drifted** into two dialects (`text-[11px] font-medium tracking-wide uppercase` vs `text-xs`).
- The table card shell (`card-border overflow-hidden shadow-sm` + `p-0`) is repeated 11 times; inner tables are inconsistent (go-daisy `table.TableWithProps` in 4 files, raw `<table class="table table-sm">` in 4 others).
- The `showModal()` helper script is duplicated in 8 files, plus ~11 inline auto-open IIFEs.
- Ten per-domain status badges re-derive the same `label → Badge intent` mapping; two render raw `<span>` instead of `ui.Badge`.

Every one of these is a drift generator: the same intent maintained in parallel copies, with no compiler check that they stay in sync. The cost is already visible in the drifted label classes, the `pre` class-order divergence, and the `card-border border-primary/20` variant at `api_tokens.templ:428`.

## What Changes

Create a real, importable component package for the gateway and extract the twelve highest-leverage repeated patterns into it:

- **Package**: `apps/web-ui/gateway/components` (`package components`, import path `github.com/emergent-company/emergent.memory/apps/web-ui/components`) — generic markup only: no gateway domain types, no route literals, no app config.
- **Extractions** (each replaces its duplicates at every call site):
  1. `SecretRevealModal` / `SecretRevealPanel` — one-time secret reveal (MCP keys, MCP shares, API tokens).
  2. `MetaRow` — key/value detail row, one label-style convention.
  3. `TableCard` — table shell wrapper.
  4. `MemoryApp.openDialog(id)` + `window.openDialogByID(id)` — one dialog-opening helper replacing 8 scripts and ~11 auto-open IIFEs.
  5. `SnippetCard` — client config snippet block with copy affordance.
  6. `ToggleField` — settings toggle row with info tip.
  7. `SelectOptionGroups` — provider/model grouped `<optgroup>` renderer.
  8. `SubNav` + `SubNavItem` — sub-navigation rail, relocated out of `agent.templ`.
  9. `StatusBadge` — status badge with explicit intent/icon.
  10. `MetaChip` — icon + text inline chip.
  11. `PanelCard` — the standard `card-border` panel.
  12. `ListRow` — the existing shared list row relocates into the package; hand-rolled equivalents migrate to it.

Rendered output is preserved. The only intentional markup changes are the three drifts normalized as part of unifying each pattern (dt label class, snippet `pre` class order, panel-card variant class), and those are called out in `design.md` D2.

Out of scope (separate changes): replacing the twelve gateway components that already duplicate go-daisy, porting `Modal`/`ConfirmDialog`/`DescriptionList`/`Chip` upstream, moving the whole `ui.templ` kit, adopting `form.FormInput`/`FormSelect`/`Textarea` at the 120 raw-input sites, and splitting the four mega-templs.

## Capabilities

### New Capabilities

- `web-ui-components`: the gateway's shared generic component package — which patterns MUST be rendered by a single shared component, the contract each component exposes, and the no-regression requirement that extraction preserves rendered output.

### Modified Capabilities

None. No route, handler, API, schema, or user-visible behavior changes.

## Impact

- `apps/web-ui/gateway/components/**` — new package (templ + Go helpers + render tests + `renderHTML` test helper).
- `apps/web-ui/gateway/*.templ` — call-site swaps across ~28 files; the twelve new components replace their duplicates; `ui.templ` and `agent.templ` shed the relocated definitions.
- `apps/web-ui/gateway/webui/static/js/app.js` — add `MemoryApp.openDialog(id)` and auto-open scanning for `[data-dialog-autoopen]`.
- `apps/web-ui/gateway/webui/css/app.css` — no change required: `@source "../**/*.templ"` already covers `components/`.
- `apps/web-ui/gateway/Taskfile.yml` — no change required: `templ generate ./...` and `go build ./...` already recurse.
- Generated `*_templ.go` are gitignored (pre-commit `no-generated` job) — regenerate via `task build`, never stage.
- Net effect: roughly 400 duplicated lines removed, no API or behavior change.
