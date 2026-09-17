# Tasks — extract-shared-ui-components

Pure refactor: every task ends with its verification gate. Unit tests are required for every component (config rule: TDD, unit tests are the minimum bar). Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

Run all commands from `apps/web-ui/gateway` unless stated otherwise.

## 1 — Package + components (blocking; lands before any call-site swap)

- [x] 1.1 Create `components/` (`package components`) with `doc.go` stating the D1/D2 contract: generic markup only, no domain types, no route literals. Add the `renderHTML(t, templ.Component) string` test helper in `components/render_test.go`. Verify: `templ generate ./...` + `go build ./...` clean; a trivial render test passes.
- [x] 1.2 Add `MemoryApp.openDialog(id)` (no-op for a missing id, a non-dialog element, or an open dialog) and the `[data-dialog-autoopen]` scanner (`DOMContentLoaded` + `htmx:afterSwap`) to `webui/static/js/app.js`. Verify: `node --check webui/static/js/app.js`; manual smoke opens a dialog on a page that already uses `showModal()`.
- [x] 1.3 `DialogOpenScript()` (page-level `openDialogByID(id)` global delegating to `MemoryApp.openDialog`) + `DialogAutoOpen()` (returns the `data-dialog-autoopen` attribute) in `components/dialog.templ`. Unit test: the script delegates to `MemoryApp.openDialog`, and the attribute is exactly `data-dialog-autoopen=true`. Verify: `go test ./components/...` green.
- [x] 1.4 `MetaRow(label, value string, opts ...MetaRowOpts)` with `Mono`, `NoEmptyDash` in `components/meta.templ`. Unit test: emits one `<dt>` using the D2 label class, `<dd>` carries the value, empty value renders `emptyDash` unless `NoEmptyDash`, `Mono` applies the mono class. Verify: `go test ./components/...` green.
- [x] 1.5 `TableCard(attrs templ.Attributes)` in `components/table.templ`, wrapping `ui.CardRaw("card-border overflow-hidden shadow-sm", "p-0", …)` so callers supply only the table body. Unit test: shell classes present, children render inside the body. Verify: green.
- [x] 1.6 `SnippetCard(label, copyTargetID string, code templ.Component)` with the D2-normalized `<pre>` class order and a `data-copy-target` copy button (slot-based per design D6). Unit test: label, code slot, and copy target render. Verify: green.
- [x] 1.7 `SecretRevealModal(p SecretRevealProps)` + `SecretRevealPanel(p SecretRevealProps)` in `components/secret.templ`. `SecretRevealProps`: `IDPrefix`, `Heading`, `Name`, `Secret`, `Endpoint`, `Warning`, `Snippets templ.Component`. Unit test: secret shown once with `data-testid` anchor, copy buttons carry the prefixed ids, warning alert renders, snippet slot renders, Done action closes, modal variant auto-open attribute present. Verify: green.
- [x] 1.8 `ToggleField(name, label, tip string, checked bool, attrs templ.Attributes)` in `components/form.templ`. Unit test: checkbox carries `name`, `toggle` class, `checked` state, and the info tip. Verify: green.
- [x] 1.9 `SelectOptionGroups(groups []SelectOptionGroup, selected string)` in `components/form.templ` with `SelectOptionGroup{Label string; Options []SelectOption}`, `SelectOption{Value, Label string; Selected bool}`. Unit test: one `optgroup` per group, options in order, selected option marked. Verify: green.
- [x] 1.10 `SubNav(label string)` + `SubNavItem(href string, active bool, icon, label string)` in `components/nav.templ` (relocated from `agent.templ:47`), preserving the rail classes and active-state styling. Unit test: rail `aria-label`, child link, active class applied only when active. Verify: green.
- [x] 1.11 `StatusBadge(label string, intent ui.BadgeIntent, icon string)` in `components/badge.templ`, emitting a `ui.Badge` (XS, dot/icon) — not a raw `<span>`. Unit test: label, intent class, and icon render. Verify: green.
- [x] 1.12 `MetaChip(icon, text string)` and `PanelCard(extraClass string, attrs templ.Attributes)` (`card-border` + `p-5`, the extra class appends) in `components/chip.templ` / `components/panel.templ`. Unit tests: chip renders `IconSpan` + text in the inline-flex wrapper; panel renders the standard classes and an extra class appends rather than replaces. Verify: green.
- [x] 1.13 `ListRow(href templ.SafeURL, leading templ.Component, title, meta string)` relocated from `ui.templ:1012` into `components/list.templ`, markup unchanged. Unit test: link href, leading slot, title, meta, chevron. Verify: green.
- [x] 1.14 Package gate: `templ generate ./...` && `go build ./...` && `go vet ./...` && `golangci-lint run ./...` && `go test ./...` all clean, with no call site yet modified.

## 2 — Swaps: shared kit + agent surface (phase 1 must be committed first)

Files owned by this phase (no other lane edits them): `ui.templ`, `agent.templ`, `schema_nav.templ`, `sidepanel.templ` if it consumes the relocated row.

- [ ] 2.1 `agent.templ` + `schema_nav.templ`: delete `subNavItem`/`agentSubNav`, route all five rails (`agent.templ:39`, `org_context.templ:713`, `org_members_ui.templ:542`, `project_settings.templ:32`, `schema_nav.templ:9`) through `components.SubNav` + `components.SubNavItem`. Verify: `go test ./...` green; agents page sub-nav renders with active state intact.
- [ ] 2.2 `ui.templ:915-925`: replace the inlined `modelGroups` loop with `components.SelectOptionGroups`. Verify: model select renders the same options and selection.
- [ ] 2.3 `ui.templ:1012`: delete the local `listRow` and point its three existing callers (`agent.templ:299`, `objects.templ:108`, `sessions.templ:56`) at `components.ListRow`. Verify: `go build ./...` + the three pages' render tests green.
- [ ] 2.4 `ui.templ:324`: table shell → `components.TableCard`. Verify: agents table still renders.
- [ ] 2.5 Add/extend a render test asserting the extracted region on the agents page (the page previously had no assertion pinning `subNavItem` markup). Verify: `go test ./...` green.

## 3 — Swaps: MCP cluster + approvals

Files owned by this phase: `agent_mcp_endpoint.templ`, `mcp_shares.templ`, `mcp_servers.templ`, `mcp_nodes.templ`, `approvals.templ`.

- [x] 3.1 `agent_mcp_endpoint.templ:552-634` and `mcp_shares.templ:397-479`: replace both 83-line reveal dialogs with `components.SecretRevealModal`; drive the field-name difference (`r.Label` vs `r.Name`) through `SecretRevealProps.Name`. Verify: both reveal surfaces render identically modulo the unified `<pre>` order; key row and share row tests green.
- [x] 3.2 `api_tokens.templ:427-468`: replace the inline reveal card with `components.SecretRevealPanel`. Verify: token reveal test green; one-time-secret copy target still resolves.
- [x] 3.3 `agent_mcp_endpoint.templ:636-657` + `mcp_shares.templ:481-506` → `components.SnippetCard`. Verify: both snippet blocks render label, code, copy affordance.
- [x] 3.4 Delete the `openX()` scripts (`agent_mcp_endpoint.templ:409-412`, `mcp_nodes.templ:190-193`, `mcp_servers.templ:305-308`, `mcp_shares.templ:240-243`, `backups.templ:304-307`, `objects.templ:437-440`, `schedules.templ:418-421`, `skills.templ:360-363`) and the inline auto-open IIFEs (`agent_mcp_endpoint.templ:626-631`, `mcp_shares.templ:471-476`, `project_settings.templ:1330-1335`, `auth_ui.templ:254-266`, `org_context.templ:125,166`, `org_members_ui.templ:699`, `backups.templ:306`, `mcp_nodes.templ:192`, `mcp_servers.templ:307`, `mcp_shares.templ:242`, `schedules.templ:420`, `skills.templ:362`, `objects.templ:461`); use `MemoryApp.openDialog` + `components.DialogAutoOpen`. Verify: unit test that the scanner runs on `htmx:afterSwap`; browser smoke re-opens a dialog after a mutation that swaps the DOM.
- [x] 3.5 Card shells in `mcp_shares.templ` / `mcp_servers.templ` → `components.TableCard`; `mcpNodeStateBadge` and `mcpShareStatusBadge` → `components.StatusBadge` adapters (via `StatusBadgeOpts` so style/size/dot are unchanged). Verify: MCP page render tests green.
- [x] 3.6 Apply `MetaChip` at `mcp_shares.templ:136,140,144` and `agent_mcp_endpoint.templ:104`. Two audit candidates stay local because they are a different element or class (`agent_mcp_endpoint.templ:511` is a nested `gap-1.5` span; `approvals.templ:57` is an anchor, not a span) — converting them would change rendered output.
- [x] 3.7 Dialog migration: all per-page `openX()` scripts deleted; row dialogs use `templ.JSFuncCall("openDialogByID", …)`/`MemoryApp.openDialog`; `data-dialog-autoopen` replaces the auto-open IIFEs (`modalShell` gained an optional attrs parameter so `providerConfigSaveErrorModal` can mark its dialog). Verify: `avatar_test.go` and `mcp_shares_handlers_test.go` updated to assert the new wiring; both suites green.

## Phase 3 note

The raw `<table class="table table-sm">` blocks in `agent_mcp_endpoint.templ` sit inside the endpoint's own `rounded-box` container, not the standard card shell, so they are not part of the `TableCard` swap; migrating them to the go-daisy table primitives is tracked with the other raw tables in 4.7.

## 4 — Swaps: settings, org, and list/table surfaces

May run as two disjoint sub-lanes after phase 1; never in the same file set.

**4A — settings/org**: `project_settings.templ`, `org_context.templ`, `org_members_ui.templ`, `schema_packs.templ`

- [x] 4.1 `MetaRow` at `backups.templ:409` (below), `org_members_ui.templ:509`, `project_settings.templ:746` (+ `voiceSecretRow` → `NoEmptyDash`), `schema_packs.templ:161`, and the 10 inlined `<dt>` sites; normalize the `text-xs` dialect per D2.
- [x] 4.2 `project_settings.templ:181-194,482-500,703-734` → `components.ToggleField`.
- [x] 4.3 `project_settings.templ:871-878,890-898` → `components.SelectOptionGroups`, keeping `modelGroups` as the domain adapter.
- [x] 4.4 `project_settings.templ`, `org_context.templ`, `org_members_ui.templ` panel shells → `components.PanelCard`; fold the `border-primary/20` variant into `ExtraClass`.
- [x] 4.5 `org_members_ui.templ:118` `roleBadge` + `schema_packs.templ:175` `ownerBadge` → `components.StatusBadge` adapters; org/member/profile table shells → `components.TableCard`.
- [x] 4.6 Verify 4A: `go test ./...` green; settings, org landing, members, schema packs pages render.

**4B — list/table surfaces**: `api_tokens.templ`, `backups.templ`, `schedules.templ`, `skills.templ`, `documents.templ`, `blueprints.templ`, `migrations.templ`, `objects.templ`

- [ ] 4.7 (deferred — see Deferred below) `TableCard` at `api_tokens.templ:121`, `backups.templ:158`, `schedules.templ:70`, `skills.templ:133`; and the raw tables at `blueprints.templ:763`, `migrations.templ:334,431` → go-daisy `table.TableWithProps` inside `components.TableCard`.
- [x] 4.8 `MetaRow` at `backups.templ:409,429,442,448,568`; `PanelCard` at `api_tokens.templ:254,350,428`, `documents.templ:44,170`, `migrations.templ:152,167,179,370`.
- [x] 4.9 `StatusBadge` adapters for `documentStatusBadge` (`documents.templ:117`), `releaseBadge` (`blueprints.templ:288`), `scheduleEnabledBadge` (`schedules.templ:143`).
- [ ] 4.10 (dropped — see Deferred below) Migrate hand-rolled rows to `components.ListRow`: `blueprints.templ:174,258,810`, `documents.templ:281,310`, `objects.templ:464,494`, `schema.templ:291`.
- [x] 4.11 Verify 4B: `go test ./...` green; tokens, backups, schedules, skills, documents, blueprints, migrations, objects pages render.

**4C — sessions/schema**: `sessions.templ`, `schema.templ`

- [x] 4.12 `runStatusBadge`/`toolStatusBadge` (`sessions.templ:336,349`) and `provenanceBadge` (`schema.templ:128`) → `components.StatusBadge` adapters; `schema.templ` panel shells → `components.PanelCard`; `schema.templ:291` row → `components.ListRow`.
- [x] 4.13 Verify 4C: `go test ./...` green; session/runs and schema pages render.

## Deferred / dropped

- **4.7 raw tables → go-daisy table primitives — deferred, not doable output-preservingly at this pin.** `table.TableWithProps` always emits its own `overflow-x-auto` wrapper and builds its class list from `TableProps`; every gateway raw table already sits inside its own wrapper div (see `api_tokens.templ:122`, `migrations.templ:333`, `blueprints.templ:763`), so adopting the primitive adds a nesting level and would change rendered markup. Doing this needs either a go-daisy `TableWithProps` variant that omits the wrapper, or a deliberate, separately-reviewed visual change. Out of scope for a refactor that must not change output.
- **4.10 hand-rolled rows → `components.ListRow` — dropped, no faithful conversions exist.** Re-inspecting the audit's candidates: `availableRow` (`blueprints.templ:174`) is a flex div holding an install form, the blueprint version row (`blueprints.templ:258`) is a `gap-2` link with an active-state ring, `objectRelationshipRow` (`objects.templ:464`) and the schema history row (`schema.templ:291`) are non-link flex divs, `similarObjectRow` (`objects.templ:494`) is a non-link card row with an action slot, and `extractedObjectRow` (`documents.templ:281`) is a `gap-2` link whose title is a span and whose trailing affordance is a `ml-auto` chevron. None is the `ListRow` shape (`gap-3` link, leading slot, grow column with title + meta paragraphs, trailing children, auto chevron), so migrating any of them would change rendered output. Item 12 closes with the three genuine callers relocated in 2.3.

## 5 — Verification sweep

- [x] 5.1 Full gate: `templ generate ./...` && `go build ./...` && `go vet ./...` && `golangci-lint run ./...` && `go test ./...` (gateway) — all clean.
- [x] 5.2 Duplication gate: re-run the audit greps and assert zero remaining duplicates for the twelve patterns (no `<dt class="text-base-content/45`, no `d.showModal()` outside `app.js`/`components/dialog.templ`, no second 80-line reveal dialog, no per-page `openX()` script).
- [x] 5.3 `cd apps/web-ui && task lint` (lefthook: gofmt, go vet, go build, `templ generate -check`) clean.
- [ ] 5.4 (not run locally — the dev server serves the shared checkout, not this worktree; CI + review-bot smoke covers it) Browser smoke on the dev server for the affected routes (`/settings/providers`, `/settings/mcp-servers`, `/settings/mcp-servers/shares`, `/agents/*` MCP endpoint, `/settings/tokens`, `/backups/*`, `/schedules`, `/skills`, `/documents`, `/blueprints`, `/objects`, `/sessions`, `/schema`, `/orgs`, `/members`): pages render, dialogs open and re-open after mutation, secrets reveal once. Verify: no console errors; screenshot check of the normalized regions.
- [ ] 5.5 (not run locally — requires a session-mode gateway against this branch; deferred to CI/PR review) `apps/web-ui/tests/e2e` smoke: run the render/title specs plus the MCP-share and token lifecycle specs. Verify: green (or the same failures as `main`, recorded).

## 6 — PR

- [ ] 6.1 Commit change artifacts + implementation on `feat/webui-shared-components`, push, open one PR to `main` linking `openspec/changes/extract-shared-ui-components/`. Verify: CI checks pass; review bot approves and merges (author does not self-merge).
- [ ] 6.2 Post-merge: `openspec archive extract-shared-ui-components` to sync the delta into `openspec/specs/web-ui-components/`.

## Notes

- Phases 2-4 are file-disjoint by construction; run them in parallel lanes only when each lane has its own isolated checkout, since concurrent `templ generate ./...` in one checkout races on generated files.
- Do not stage `_templ.go` or `webui/static/css/app.css` (pre-commit `no-generated` job rejects them).
- If a phase-2..4 swap reveals a page render test pinning a normalized drift, update that assertion in the same task and note it in the PR body.
