# 2026-09-04 — Rebrand + unify page titles

## Goal

Two UI/branding tasks: (1) rebrand the web UI logo from monogram "A" to "M" and remove the "voice · chat agents" subtitle, and (2) design and implement a single, consistent page-title algorithm for every web page.

## Outcome

Done.

- Logo monogram changed `A` → `M`; subtitle "voice · chat agents" removed from both the sidebar header and the login footer.
- Page-title unification shipped end-to-end via the OpenSpec workflow: proposal → spec → design → tasks → implement → archive, with a new main spec `openspec/specs/web-page-titles/spec.md`.
- All ~78 hardcoded `"<label> — Memory"` titles across 14 handler files replaced by a single `pageTitle(...)` helper.

## Decisions

- `pageTitle(segments ...string) string` — variadic keeps call sites terse and expresses ordering naturally.
- Separator ` — ` + trailing brand, hardcoded in the helper — matches the pre-existing `"X — Memory"` convention, so the change is behavior-preserving (no title churn).
- Blank/whitespace segments dropped in the helper; load-error call sites still pass a generic fallback noun (`"Agent"`, `"Object"`, `"Document"`) so no title renders a bare `" — Memory"`.
- Entity + section are separate segments (`pageTitle(agent.Name, "Settings")`) — fixes the old `"Ada settings"` vs `"Ada memories"` inconsistency into a uniform `Entity — Section — Memory` shape.
- `appBrand = "Memory"` const — single source reused by `pageTitle`, `apple-mobile-web-app-title`, and the navbar brand label.
- Logo `A` → `M` and subtitle removal — direct user request; no design judgment applied.

## Changes

- `gateway/ui.templ` — monogram `A`→`M`, dropped subtitle `<span>`, `apple-mobile-web-app-title` and `layout.Navbar` now use `appBrand`.
- `gateway/auth_ui.templ` — removed `login-foot` "Memory — voice · chat agents" line; login `<title>` → `pageTitle("Sign in")`.
- `gateway/ui.go` — added `appBrand` const + `pageTitle` helper; migrated 15 `s.page(c, "… — Memory", …)` call sites.
- `gateway/{agent,objects,sessions,migrations,schedules,backups,usage,org_members_ui,settings_handlers,runs,schema,approvals_handlers,blueprints_handlers}.go` — migrated remaining `s.page` titles to `pageTitle(…)`.
- `gateway/ui_test.go` — new `TestPageTitle` (7 cases).
- `gateway/auth_ui_test.go` — added `<title>Sign in — Memory</title>` assertion.
- `openspec/changes/unify-page-titles/` — proposal/spec/design/tasks, then archived to `openspec/changes/archive/2026-09-04-unify-page-titles/`.
- `openspec/specs/web-page-titles/spec.md` — new main spec (6 requirements).

## Verification

- `templ generate` — success.
- `go build ./...` — success (clean).
- `go test ./...` — ok (all packages pass).
- `go test -run TestPageTitle -v` — 7/7 subtests pass.
- `go test -run TestRenderLoginPage` — ok.
- `go vet ./...` — clean; `gofmt -l ui.go ui_test.go` — no output.
- `golangci-lint run ./...` — 0 issues.
- `openspec validate unify-page-titles` — valid.
- Grep for `" — Memory"` literal after migration — zero remaining outside the helper.

## Open questions / follow-ups

- Rebrand is incomplete at the asset level: `gateway/webui/static/manifest.webmanifest` still has `"description": "Voice · chat agents"`, and the PWA icons (`icon-192.png`, `icon-512.png`, `apple-touch-icon.png`) predate the rename and likely still show the old monogram. Tracked as task `rebrand-app-assets`.

## Tasks

- [rebrand-app-assets](../tasks/rebrand-app-assets.md) — remove the manifest subtitle + regenerate PWA icons to the "M" brand.
