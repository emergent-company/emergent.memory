## Why

Two independent problems, both invisible to a user but both blocking a goal the operator stated directly:
*"if I suddenly change the radius of buttons or paddings, I want to be able to do it by adjusting the
theme."*

**1. Build-input hygiene.** `vendor/` is untracked (`apps/web-ui/gateway/.gitignore:3`) and is a genuine CSS
build input: `webui/css/app.css` `@source`s the vendored go-daisy components and `@import`s their
`components/css/custom.css` (`Taskfile.yml:22-27`). Generation is keyed to directory *absence*
(`Taskfile.yml:27`), so a stale tree survives indefinitely — in the shared checkout `go.mod:9` pins go-daisy
`6d4696fc9cf9` while `vendor/modules.txt` records `d98f93ca60f9` (29 commits behind), so the CSS build can
silently compile the wrong component source with no other signal.

Note `go build -mod=vendor ./...` is **not** a viable check: verified it exits 1 (`inconsistent vendoring`)
even with a freshly regenerated tree, because the gateway module lives in the repo-root `go.work` workspace,
where workspace-mode vendoring requires `go work vendor`. The tree therefore matters only to the CSS build,
and the pin is guarded by a test rather than by that command. Separately, the pre-built go-daisy bundle is
not referenced by any page, while the `/static/*` mount that serves go-daisy's own static tree **stays**
(go-daisy's `layout`/`alpine`/`stimulus` components emit live `/static/js/*` URLs), and the `.row-actions`
rules are dead.

**2. Theme-owned values that do not follow the theme.** The theme block exists and is correct
(`webui/css/app.css:38-75`: `--radius-selector:1rem`, `--radius-field:0.25rem`, `--radius-box:0.5rem`,
`--size-selector/field:0.25rem`, `--border`, `--depth`, `--noise`, theme `dark`). Buttons, inputs, selects,
textareas, cards, dialogs, badges, and alerts already follow it — **but**:

- `rounded-lg` compiles to `var(--radius-lg)` from *Tailwind's* namespace, not daisyUI's `--radius-field`, so
  31 literal radius sites in our templates (20 `rounded-lg`, 7 `rounded-md`, 3 `rounded`, 1 `rounded-2xl`)
  plus 8 card-like radius literals in `app.css` never follow the theme. Changing the theme radius does not
  reach them. go-daisy carries 40+ of the same literals.
- **daisyUI has no padding theme token.** `--card-p`/`--btn-p` are per-size internal literals, and our
  components override `card-body` padding directly (`components/panel.templ:14` `p-5`,
  `components/table.templ:11` `p-0`, `components/secret.templ:109`, `blueprints.templ:308`, `ui.templ:308`,
  `share_manage.templ:225,416`, `approvals.templ:28,45`), so density cannot be changed centrally at all.
- The page background is hardcoded five ways: `app.css:137` plus three inline FOUC guards (`ui.templ:77`,
  `auth_ui.templ:59`, `share_page.templ:141`), plus `theme-color` metas and the web manifest.

Three verification lanes (our repo, the pinned go-daisy copy, and daisyUI 5.5.19 theming semantics) confirmed
all of this and re-verified the load-bearing claims independently: `rounded-lg` resolves from Tailwind's
namespace, `rounded-btn` is a dead class in daisyUI 5 (`form/palette.templ:125`, `form/combobox.templ:70` —
those elements render with **zero** radius today), and `@theme inline` is real Tailwind v4 syntax.

## What Changes

**Build-input hygiene (no rendered-output change).**

- Key `vendor/` generation to the pinned dependency version rather than directory absence, and add a check
  that fails when a present tree disagrees with `go.mod`.
- Confirm the unused go-daisy pre-compiled bundle is not referenced by any page. The `/static/*` mount
  itself **stays**: go-daisy's `layout`, `alpine`, and `stimulus` components emit live `/static/js/*` URLs
  and the mount is documented as intentional (`webui/webui.go:3-5`), so removing it would silently break any
  future adoption of them.
- Remove the dead `.row-actions` rules.
- Do **not** edit the vendored `custom.css` — it is generated, and its defects (unclosed braces at `:7`/`:17`,
  stray `*/` at `:51`, dead daisyUI-v4 variables) are upstream.

**CSS consolidation and theming.**

- Make radius theme-driven: migrate the 31 literal sites to `rounded-box`/`rounded-field`/`rounded-selector`
  (containers and tiles vs controls and chips), convert the 8 card-like `app.css` radius literals to
  `var(--radius-*)`, and add a temporary `@theme inline` alias so the literals still present in library
  markup follow the theme until upstream converts them.
- Make density one central edit: remove the component-local padding overrides on daisyUI roots and declare
  density in one documented block, noting that no daisyUI padding token exists and that the block uses
  component-internal variables and layered overrides.
- Give the page background one source, keeping the background/surface token pairs distinct
  (`background_color` vs `theme_color`).
- Collapse the JS-injected stylesheet duplication to a documented, tested subset; introduce the muted-text
  and icon-size scale; move page-local `<style>` blocks that duplicate shared idioms into the shared sheet.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-ui-css`: adds radius-derives-from-theme, density-has-one-central-source, and
  page-background-has-one-source requirements; scopes the injected-stylesheet requirement to a documented
  tested subset; and adds the CSS-build-input-matches-the-pin requirement.

## Scope

- **Unit 1** (`web-ui-component-conventions`, PR #874) landed the conventions these changes enforce.
- **Unit 3** (`web-ui-component-consolidation`) carries the component consolidation and the UX/IA/a11y/copy
  work, including the guard-test narrowing. This unit does not touch component structure.
- **Deferred to a fourth unit: the muted-text and icon-size scale.** An earlier draft of this change
  required muted emphasis and icon sizes to come from a small shared scale. That is a ~500-site sweep across
  essentially every template, and because the rule admits no opacity outside the scale, a partial migration
  would leave it unsatisfied. The requirement has been removed from this change's `web-ui-css` delta rather
  than shipped half-done; the scale's definition and the migration belong together in their own unit. This
  is recorded here so the deferral is visible, not silent.
- **go-daisy upstream** (§7 of the audit record, PR #854) stays a cross-repo dependency: the library's own
  radius literals, its padding overrides, the dead `rounded-btn` class, and its global `--text-*` redefinition
  are fixed there, then re-pinned here. The `@theme inline` bridge exists so this unit does not block on it.

## Impact

- **Gateway files:** `Taskfile.yml`, `webui/css/app.css`, `main.go` (unused bundle mount), ~20 templates for
  the radius migration, and `static/manifest.webmanifest` + three head partials for the background/metadata
  pairing.
- **No Go logic, API, schema, or migration change.**
- **Guard tests:** `css_consolidation_test.go` (stylesheet link, compiled-CSS contents, compile determinism —
  its compiled-CSS assertions **skip** unless `webui/static/css/app.css` exists, so `task css` must run first),
  `page_width_consistency_test.go` (unchanged), and `visual_delta_pinning_test.go` where radius class strings
  are pinned — those class-only assertions are narrowed, not re-pinned, per the conventions.
- **Verification:** `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`, `task lint`,
  `task css` twice for determinism, `task e2e:test`, and a manual check that changing `--radius-field` in the
  theme block moves buttons/fields/cards/dialogs/badges with no template edit.
