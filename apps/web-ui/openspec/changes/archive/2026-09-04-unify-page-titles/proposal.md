## Why

Every web page in the control plane builds its browser `<title>` by hand-concatenating a page label with the brand (`"Agents — Memory"`), repeated across ~40 handler call sites with no shared helper. Titles are observably inconsistent — the em-dash separator and trailing brand are duplicated as string literals, entity-vs-section ordering varies (`"Ada settings — Memory"` vs `"Ada memories — Memory"`), and the login page hardcodes its own title outside the shared shell. Consolidating title construction into one function makes the browser-tab title deterministic and trivially consistent across all pages.

## What Changes

- Add a single `pageTitle(...segments)` helper that composes the browser `<title>`: drops blank segments, joins non-empty segments with an em dash, and always appends the brand `"Memory"` last.
- Migrate every `s.page(c, "<label> — Memory", …)` call site to `s.page(c, pageTitle("<label>"), …)`.
- Express entity + section as separate segments (e.g. `pageTitle(agent.Name, "Settings")`) so detail-page ordering is uniform.
- Give empty/unknown entities (load-error paths) a stable fallback label so no title ever renders a bare `" — Memory"`.
- Route the login page title through the same helper/format (result `"Sign in — Memory"`).
- Promote the brand string to a single `appBrand` constant reused by the title helper, `apple-mobile-web-app-title`, the web manifest, and the navbar brand label.

## Capabilities

### New Capabilities

- `web-page-titles`: consistent construction of the browser page title across every web page.

### Modified Capabilities

<!-- none: no existing spec covers page titles -->

## Impact

- `gateway/ui.go` — add `appBrand` const + `pageTitle()` helper; migrate all `s.page` title args in `ui.go`, `agent.go`, `objects.go`, `sessions.go`, `migrations.go`, `schedules.go`, `backups.go`, `usage.go`, `org_members_ui.go`, `settings_handlers.go`.
- `gateway/ui.templ` — reuse `appBrand` in `apple-mobile-web-app-title` and navbar label.
- `gateway/auth_ui.templ` — login `<title>` uses the same helper/format.
- `gateway/*_test.go` — unit tests for `pageTitle` (segment joining, blank handling, brand suffix, empty-entity fallback).
- No API, dependency, or data-model changes.
