## Why

A full audit of the gateway web UI (`apps/web-ui/gateway`, Go templ + HTMX + daisyUI 5) found four
classes of problem. None of them is a missing feature; all of them are accumulated drift, and each one
gets more expensive the longer it sits.

**1. Duplicated logic that safety and correctness depend on.** The app grew a `components` package and
adopted most of go-daisy, but several patterns still have two or three live implementations:

| Pattern | Copies | Sites |
|---|---|---|
| Copy-to-clipboard (server-rendered) | 2 | canonical `apiTokenCopyScript` (`api_tokens.templ:473`) used 4×; re-implemented `shareManageScript`/`copyText` (`share_manage.templ:618`, raw buttons at `:589`, `:605`) |
| Toast dispatch | 3 | canonical `MemoryApp.toast` (`webui/static/js/app.js:619`; the `toast` function is declared at `:40`); `flashToasts` (`ui.templ:1012`); a third `toast()` (`mcp_servers.templ:565`) |
| Raw `<table class="table table-sm">` beside the shared shell | 6 tables | `migrations.templ:344,441` and `blueprints.templ:759` are **unwrapped**; `api_tokens.templ:126` and `agent_mcp_endpoint.templ:237,478` already sit inside `components.TableCard` |
| Raw form controls bypassing `form.FormControl` | ≥24 | `project_settings.templ:174…366` (13), `schema.templ:383…520` (7), `sidepanel.templ:116`, `share_page.templ:303,512,574` |

That is not cosmetic. The copy helpers are the only path by which a user retrieves a one-time secret,
and the two implementations have already diverged (one supports reveal-fetch, one does not). Three toast
entry points mean three places a failure can be silently swallowed. (Chat-message copy in
`chat-host.js:85` / `chat-components.js:281` is a separate, page-local concern and out of scope here.)

**2. A locally-generated `vendor/` tree that can silently misrepresent the build.** `vendor/` is
untracked (`apps/web-ui/gateway/.gitignore:3`) — it is a *build input* that `task css` generates because
`webui/css/app.css` `@source`s the vendored go-daisy components and `@import`s the vendored
`components/css/custom.css` from that stable path (`Taskfile.yml:22-27`). For Go compilation the
authoritative source is the module cache at the pinned version; for the CSS build it is this tree, so a
stale tree means the stylesheet is compiled from library source that disagrees with `go.mod`. Generation
is not keyed to the dependency version: `Taskfile.yml:27` runs `go mod vendor` only when the directory is
**absent**, so a stale tree survives indefinitely. In the shared checkout that has already happened —
`go.mod:9` pins go-daisy `6d4696fc9cf9` while `vendor/modules.txt` records `d98f93ca60f9` (29 commits
behind, missing `ui.Dialog`, `ui.ConfirmDialog`, `ui.Disclosure`), and `go build -mod=vendor ./...` fails
with `inconsistent vendoring`. A fresh worktree has no tree at all.

**3. Dead, malformed, or duplicated CSS.** A 477 KB go-daisy bundle is mounted and served at `/static/*`
with zero referencing pages. The go-daisy stylesheet that this app `@import`s ships unclosed braces at
`custom.css:7` and `:17`, so its global reduced-motion and forced-colors `@media` blocks are mis-scoped
inside a sidebar selector, plus a stray `*/` with no opener at `:51`; it also carries daisyUI **v4**
variables (`--bc`, `--p`, `--s`) that v5 never defines — an **upstream library defect**, since the file is
generated from go-daisy (see Scope Boundary). `webui/static/js/chat-components.js:172,473` re-declares
~200 lines of `app.css` at runtime (a second copy of rules such as `.chat-bubble-neutral` at
`app.css:298` and `.memory-md pre` at `:970`). Twelve muted-text opacities and an icon-size spread
(`size-4` ×72, `size-3.5` ×20, `size-4.5` ×3) give the app no typography scale. The dark background
`#0E1017` is hardcoded in four places.

**4. Cross-page inconsistency a user notices immediately.**

- Settings pages disagree with their own sidebar group: `settingsSubNav` (`project_settings.templ:33-60`)
  lists seven destinations while the sidebar Settings group (`ui.go:100-108`) has a different five
  (Project, API Tokens, MCP Servers, Blueprints, Skills), and `/settings/tokens`, `/settings/approvals`,
  `/settings/mcp-servers`, `/settings/mcp-servers/shares` render **no rail at all**. Approvals is
  additionally a top-level, ungrouped nav item (`ui.go:75-82`), so "the rail lists the group" and "these
  pages get a rail" cannot both hold until the IA is settled — this change settles it. The existing
  `settings-navigation` capability currently scopes the rail to the six project-setting sections, so the
  change modifies that capability rather than adding a rival contract.
- Two page-header idioms and a kicker taxonomy matching no sidebar group: `/schema` uses the list header,
  its own `/schema/packs` uses the detail header; Schedules sits in sidebar group "Agents" but is kickered
  "Memory".
- Destructive actions use three conventions plus seven sites with **no confirmation**, the worst being
  Blueprints "Remove" (`blueprints.templ:103`), which silently destroys a schema install.
- Settings autosave is invisible: `project_settings.templ:174-187` posts `hx-swap="none"` with no
  in-flight or failure signal, so a failed save is indistinguishable from an untouched field.
- Mobile table→card fallback exists only for agents (`ui.templ:224-229`) and blueprints
  (`blueprints.templ:742`, `:758`); backups, skills, schedules, members, API tokens, and usage are
  horizontally-scrolling tables.
- ⌘K finds 5 of the app's ~15 destinations (`spotlight.templ:24-30`).

## What Changes

**Phase 1 — build-input hygiene (no rendered-output change).**
- Key `vendor/` generation to the pinned dependency version, so a version change regenerates the tree
  instead of reusing a stale one, and add a check that fails when a present tree disagrees with `go.mod`.
- Stop serving the unused 477 KB go-daisy bundle at `/static/*`.
- Remove the dead `.row-actions` / `tr[data-row]:hover` rules.
- (Does **not** edit the vendored `custom.css`: it is generated, and its defects are upstream — §7.9.)

**Phase 2 — single-source the duplicated patterns (component extraction).**
- `components.CopyButton` + one `CopyScript`, replacing both server-rendered clipboard implementations;
  delete the raw `share_manage.templ` buttons.
- One client toast dispatch over `MemoryApp.toast`, replacing `flashToasts` and the `mcp_servers.templ`
  copy — while preserving the server-rendered PRG flash path, which must not depend on the client bundle
  because the app shell renders content before it loads the scripts (`ui.templ:133` vs `:168`).
- One table story: wrap the **two genuinely unwrapped** tables (`migrations.templ:344,441`) and
  `blueprints.templ:759` in the shared shell, or adopt the go-daisy table primitive — re-examining the
  earlier deferral of that adoption explicitly rather than leaving both stories standing.
- Route the ≥24 raw form controls through `form.FormControl`.
- Collapse the surface sprawl the audit exposed: **five** card constructors to one card component plus a
  card list (density variant), **four** form-field shapes to one field component, **two** page-header
  components to one with optional kicker/breadcrumbs/actions, and **five** tone spellings to one shared
  tone type with the domain mapping pushed out of the shared layer.
- Remove layout props (`Margin`, `CardClass`, `BodyClass`, generic class escape hatches) in favour of
  density variants, so callers own external spacing.
- New primitives for the remaining repeats: stat grid, inline code, soft info panel, eyebrow adoption,
  `MetaRow` adoption, and `ui.Button` migration for the 19 raw `btn btn-*` occurrences across 8 templates.

**Phase 3 — CSS consolidation and theming.**
- Reduce the JS-injected stylesheet duplication to a documented, tested subset (it cannot simply be
  deleted: another active change depends on it staying in sync).
- Introduce a muted-text and icon-size scale and token-ize the hardcoded colours, including the four
  `#0E1017` FOUC guards — declaring the resulting class-string normalizations explicitly.
- Move the page-local `<style>` blocks that duplicate shared idioms into the shared stylesheet.

**Phase 4 — UX/IA consistency.**
- Make the settings rail the navigator for the Settings area, agreeing with the sidebar group, and render
  it on every Settings-area destination (a MODIFIED delta on `settings-navigation`).
- One page-header idiom, with a kicker taxonomy that equals the sidebar groups.
- One destructive-confirmation convention, applied to every destructive route — including the two flows
  whose existing specs currently delete on plain activation.
- Visible saving/failed feedback for settings autosave that stays compatible with the toast-based
  `inline-form-saving` contract, and in-context failures for form submissions.
- One rule for table-versus-card-list, and mobile card fallbacks for the tables that lack them.
- Complete the command palette from the sidebar groups.
- Accessibility: keyboard-reachable row targets, ≥44 px touch targets, per-field validation errors,
  non-colour-only status.
- Copy pass: canonical empty-state and error-heading forms, removal of internal-detail phrasing.

## Capabilities

### New Capabilities

- `web-ui-navigation`: page-level navigation cues for the gateway — the page-header idiom each page
  class must use, the kicker vocabulary, and command-palette coverage of the nav. (The settings rail is
  not defined here; it stays with `settings-navigation`.)
- `web-ui-accessibility`: keyboard reachability of row targets, minimum touch-target size for row
  actions, per-field validation feedback, and the non-colour-only status rule.
- `web-ui-copy`: the voice rules for user-facing strings — empty states, error headings, and the ban on
  internal implementation detail in user copy.

### Modified Capabilities

- `web-ui-components`: adds requirements for copy affordances, one client toast dispatch, one table
  story, form-control adoption, one card component, one form-field component, one page-header component,
  one shared tone type, and the remaining repeated primitives (stat grid, inline code, info box, eyebrow,
  button, empty state); records the table-versus-card-list rendering rule; reconciles **both** the
  "adoption preserves rendered output" and "extraction preserves rendered output" requirements with the
  normalizations this change introduces, including the shift to narrowing class-only assertions rather than
  re-pinning them; and retires the standalone `ConfirmIcon` contract now that `ui.ConfirmDialog` owns the
  icon circle.
- `web-ui-css`: scopes the injected-stylesheet requirement to a documented, tested subset (the existing
  sync contract stays authoritative); adds the muted-text/icon scale, token-derived colours, and a CSS
  build input that matches the pinned dependency version; extends the monolithic-bundle requirement from
  "not linked" to "not served".
- `settings-navigation`: extends the rail from the six project-setting sections into the navigator for
  the whole Settings area, so every settings destination renders it and its entries agree with the
  sidebar Settings group; keeps section save behaviour intact.
- `project-settings-ui`: adds saving/failed feedback for autosave and in-context failure for submissions,
  reconciled with the existing "Edit the project info" scenarios.
- `inline-form-saving`: clarifies that the settings autosave indicator is additive to — not a replacement
  for — the existing toast feedback.
- `blueprint-gallery`: routes pack removal through the shared destructive confirmation.

## Scope Boundary — go-daisy upstream work is **not** in this change

The audit also identified work that belongs upstream in `emergent-company/go-daisy`, not in this repo:
repairing the stylesheet defects (`custom.css` brace nesting and its dead v4 variables), back-porting the
secret-reveal surfaces (`ui.SecretRevealModal` from `components/secret.templ`'s `SecretRevealModal`) and
the meta rows (`ui.MetaRow`/`ui.MetaGrid` from `components/meta.templ`), extending `form.ToggleInput`
with its label/tip row and `form.Select` with option groups, and extending `ui.CodeBlock` with a copy
affordance.

go-daisy has **no OpenSpec root**, so that work cannot be specified or implemented here. It is recorded in
`tasks.md` §7 as a dependency list only: each item must be done in the go-daisy repo on its own branch
(one `.templ` + generated `_templ.go` + mandatory `boundary.go` `*WithBoundary` wrapper + gallery
route/seed + unit test), merged there, and then the app re-pins. This change deliberately keeps the local
components — and leaves the vendored stylesheet untouched — until those land, so nothing here blocks on a
cross-repo release. The one exception is `ConfirmIcon`: `ui.ConfirmDialog` **already** renders that icon
circle internally, so the local component is retired in this change rather than deferred.

## Impact

- **Web UI gateway only.** Primary files: `apps/web-ui/gateway/components/*` (new components),
  the ~30 page templates listed in `tasks.md`, `webui/css/app.css`, `webui/static/js/chat-components.js`,
  `webui/static/js/app.js`, `Taskfile.yml`, and the untracked `vendor/` build input.
- **Server, CLI, connectors, iOS: untouched.** No API, schema, or migration change.
- **Assertions are the contract, but not the class list.** `refactor_exact_test.go`,
  `visual_delta_pinning_test.go`, `adoption_contract_test.go` and `api_tokens_ui_test.go:60` currently pin
  class strings and class order alongside real contract attributes. This change keeps the contract
  assertions (ids, `data-testid`, `aria-*`, `hx-*`, client markers) and **narrows or removes** the
  class-only ones rather than re-pinning them to new class strings, because class composition is not part
  of a component's contract and a cosmetic change must not fail a unit test. Visual and interaction
  verification moves to the gallery and `task e2e:test`. `page_width_consistency_test.go` (the page-width
  rule, with the chat `max-w-[100rem]` exception) and `css_consolidation_test.go` (stylesheet link,
  compiled-CSS contents, compile determinism) keep their current role — the latter's compiled-CSS
  assertions still **skip** unless `webui/static/css/app.css` exists, so they only bite after `task css`.
- **Verification gate for every task group:** `templ generate ./...`, `go build ./...`, `go vet ./...`,
  `go test ./...`, and `task lint` from `apps/web-ui/gateway`, plus `task css` before any CSS assertion,
  the Playwright e2e suite (`task e2e:test`), and a manual browser pass for the UI-visible phases.
