## Context

The gateway is one `package main` at `apps/web-ui/gateway` (module `github.com/emergent-company/emergent.memory/apps/web-ui`). It already has two subpackages (`webui/` for embedded assets, `blueprints/` for the bundled blueprint dirs), so adding a third is an established pattern. `templ generate ./...`, `go build ./...`, `go vet ./...` and golangci-lint all already recurse into subpackages, and Tailwind's `@source "../**/*.templ"` in `webui/css/app.css` already globs any new templ under `gateway/`.

## Goals / Non-Goals

**Goals**

- One importable home for the shared generic markup patterns, so a pattern change is a compiler-checked single edit.
- Extract the twelve measured patterns with preserved rendered output.
- Establish the test pattern for the package (render-and-assert, matching the existing gateway convention).

**Non-Goals**

- Porting components upstream to `go-daisy` (separate change; see audit §B).
- Replacing the twelve gateway locals that already duplicate go-daisy (`toastQueue`, `statRow`, `pageHeader`, `detailHeader`, `appSidebar`, `spotlight*`, …).
- Moving the entire `ui.templ` shared kit, or splitting mega-templs.
- Adopting `form.FormInput`/`FormSelect`/`Textarea` at the raw-input sites.
- Any route, handler, API, or user-visible behavior change.

## Decisions

### D1 — Package location and name

`apps/web-ui/gateway/components`, `package components`, import path `github.com/emergent-company/emergent.memory/apps/web-ui/components`.

Chosen over extending `ui.templ` (the current problem: a page file doubling as the shared kit) and over a nested `gateway/internal/...` (this module has no `internal/` convention today). Call sites read `@components.MetaRow(...)`, which is disambiguated from go-daisy's `@ui.*` by the import name.

### D2 — Domain-free by construction; output preserved except the named drifts

The package MUST NOT import gateway domain types, reference gateway route paths, or read gateway config. Anything that needs a domain type stays in `package main` as a thin adapter (see D4).

Extraction preserves rendered markup, with a small set of intentional normalizations, each a fix for a real drift already in `main`:

1. `<dt>` label class unifies on `text-base-content/45 text-[11px] font-medium tracking-wide uppercase` (the `text-xs` dialect at `org_members_ui.templ:441,511`, `project_settings.templ:748`, `schema_packs.templ:151,163` normalizes to it), and the row's `<dd>` spacing unifies on `mt-1` (the `mt-0.5` dialect normalizes to it).
2. Snippet `<pre>` class order unifies on the `agent_mcp_endpoint.templ:653` order (`rounded-box bg-base-100 overflow-x-auto whitespace-pre-wrap …`).
3. Panel cards unify on `card-border` + `p-5`; the one-off `card-border border-primary/20` variant (`api_tokens.templ:428`) becomes `PanelCard` with an appended `ExtraClass`.
4. Tailwind class *order* differences that produce identical CSS (for example the secret element's `border`/`rounded-box` ordering between the two reveal dialogs) are dropped — only the set of classes matters.
5. Insignificant inter-element whitespace inside a `<select>` is dropped by rendering the option groups from a component (`TestRefactorOutputExact` pins the old space between the default option and the first `<optgroup>`; the guard is updated). No CSS, DOM structure, or parse result changes.
6. The reveal panel's heading becomes an `<h3>` (it was a `<p>`) so both reveal variants share one heading treatment.

Anything else that renders differently — or any class that changes the rendered CSS — is a bug in the extraction, not an accepted drift.

### D3 — Dialog opening: one JS helper, attribute-driven auto-open

`window.MemoryApp` already exists (`webui/static/js/app.js:500`) and is the documented page-JS surface (`ui.templ:197-204`). Add `MemoryApp.openDialog(id)` there — a no-op for a missing id, a non-dialog element, or an already-open dialog — and delete the eight per-page `openX()` scripts.

For the ~11 inline auto-open IIFEs (`agent_mcp_endpoint.templ:626-631`, `mcp_shares.templ:471-476`, `project_settings.templ:1330-1335`, `auth_ui.templ:254-266`, …) which exist to re-open a dialog after an htmx swap, the trigger becomes a markup attribute: `components.DialogAutoOpen()` returns `data-dialog-autoopen`, scanned by `app.js` on `DOMContentLoaded` and `htmx:afterSwap`. That keeps the htmx re-open semantics while deleting the per-page scripts.

For the row-dialog scripts whose id is computed at render time (`openBackupDelete(id)`, `openMCPShareDialog(kind, id)`, …), `app.js` exposes a `window.openDialogByID(id)` global that delegates to `openDialog`; templ call sites emit inline handlers via `templ.JSFuncCall("openDialogByID", …)` and stop declaring their own opener per dialog.

### D4 — Domain vocabularies stay in `package main`

`StatusBadge` takes `(label string, intent ui.BadgeIntent, icon string)`. The ten per-domain wrappers (`documentStatusBadge`, `runStatusBadge`, `toolStatusBadge`, `releaseBadge`, `mcpNodeStateBadge`, `mcpShareStatusBadge`, `scheduleEnabledBadge`, `roleBadge`, `ownerBadge`, `provenanceBadge`) collapse into one-line adapters in `package main` that map their domain status to `(label, intent, icon)`. This keeps "which intent does `draft` mean" a domain decision while the markup itself is shared.

Same reasoning for `SelectOptionGroups`: the component takes `[]SelectOptionGroup{Label string; Options []SelectOption{Value, Label string; Selected bool}}`; the existing `modelGroups(models)` adapter in `package main` keeps producing the model-domain grouping.

### D5 — `MetaRow` replaces four implementations, not three

`backupDetailField` (`backups.templ:409`), `memberDetailRow` (`org_members_ui.templ:509`), `schemaPackMetaRow` (`schema_packs.templ:161`) and `voiceSecretRow` (`project_settings.templ:746`) are the same component with different empty-value handling, so one `MetaRow(label, value string, opts ...MetaRowOpts)` covers all four. `MetaRowOpts` carries `Mono bool` and `NoEmptyDash bool` — `voiceSecretRow` renders status copy rather than an em-dash and is the `NoEmptyDash` caller.

### D6 — Slots over domain data for syntax-highlighted content

`SnippetCard(label, copyTargetID string, code templ.Component)` takes the code as a slot rather than a string. Syntax highlighting (`highlightJSONSnippet`, `highlight.go:56`) is generic code but is shared with non-snippet JSON surfaces in `package main`; moving it would force call-site edits outside this phase's scope. Passing the already-rendered code as a component preserves the highlighted output exactly and keeps the component domain-free.

By the same rule, `SecretRevealProps.Snippets` is a slot: the modal renders the "Client setup" heading and guidance, and the caller supplies the list, so the MCP snippet type stays in `package main`.

### D7 — Test convention

Each component gets a render test in `components/*_test.go` using the same `renderHTML(t, component)` idiom as gateway tests (`agent_ui_test.go:14`). Tests assert the contract, not the full markup: required/scoped attributes, slot content present, empty-value fallback, and the class that carries the pattern's identity (for example that `MetaRow` emits exactly one `<dt>` label class).

Extraction-only call-site edits are covered by the existing gateway `*_ui_test.go` render tests plus a targeted assertion added where a page had no coverage of the extracted region.

### D8 — Sequencing

Call-site swaps touch the same ~28 files repeatedly, so the component definitions land first (phase 1), then swaps proceed in **file-disjoint** groups (phases 2-4) to allow parallel lanes with no overlapping write scopes.

## Risks / Trade-offs

- **Hidden markup dependence in tests.** Existing `*_ui_test.go` assertions may pin markup the unified component changes (D2 drifts). Mitigation: phases 2-4 each end with the full `go test ./...` for the gateway, and any assertion that pins a normalized drift is updated as part of the same task.
- **htmx re-open regression.** Replacing auto-open IIFEs with `data-dialog-autoopen` scanning can silently break re-open-after-swap if `htmx:afterSwap` is not wired. Mitigation: the auto-open task includes a test that the scanner runs on `htmx:afterSwap`, and the browser smoke step exercises reveal-after-mutation.
- **`package main` remains a god-package.** This change creates the boundary but does not move the whole kit — the audit's phase 2 remains open. Trade-off accepted: a smaller, verifiable change beats a mass move.
- **Visual drift beyond the three named normalizations.** Mitigation: per-page diff of rendered HTML before/after for the affected routes during browser smoke.

## Migration Plan

1. Phase 1 — package skeleton, helpers, and the twelve components with unit render tests. No call sites touched; gateway still compiles green.
2. Phases 2-4 — call-site swaps in file-disjoint groups; duplicates deleted as each group completes.
3. Phase 5 — verification sweep: `templ generate`, `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`, Playwright smoke on the affected pages.
4. No data migration, no rollback concern: the branch is a pure refactor and reverting it restores the duplicated markup.

## Deferred by measurement

Two audit items turned out not to be output-preserving and are therefore not shipped in this change:

- **Raw table markup → go-daisy table primitives.** `table.TableWithProps` always emits its own `overflow-x-auto` wrapper, and every gateway raw table already lives inside one, so adopting the primitive would add a nesting level. This needs an upstream `TableWithProps` option (or an accepted visual change) — tracked in `tasks.md` Deferred.
- **Hand-rolled rows → `components.ListRow`.** Re-inspection showed none of the remaining rows is the `ListRow` shape (link, leading slot, grow column with title + meta, trailing children). The three genuine callers were migrated in phase 2; the rest would visually change.

## Open Questions

- None blocking. Whether `TableCard` should be built on go-daisy `table.TableCardWrapper` was checked and rejected: that component's shell is `card bg-base-100 shadow-sm border border-base-200`, which is not the gateway shell (`card-border overflow-hidden shadow-sm`); unifying them would change rendered output, so `TableCard` stays a gateway-local component for now.
