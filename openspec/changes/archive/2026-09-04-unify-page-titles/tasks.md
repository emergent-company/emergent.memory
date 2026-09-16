## 1. Helper + unit tests (TDD)

- [x] 1.1 Add `appBrand = "Memory"` const and `pageTitle(...segments string) string` helper in gateway/ui.go — trims and drops blank segments, appends brand, joins with " — "
- [x] 1.2 Write `TestPageTitle` unit tests in gateway/ui_test.go covering: single segment, multiple segments (entity + section), blank/whitespace-only segment dropped, all-blank segments still yields brand, and order preservation — verify `go test ./...` passes
- [x] 1.3 Verify `go build ./...` compiles after the helper lands

## 2. Migrate app-shell call sites

- [x] 2.1 Replace every `s.page(c, "<label> — Memory", …)` literal in ui.go, agent.go, objects.go, sessions.go, migrations.go, schedules.go, backups.go, usage.go, org_members_ui.go, settings_handlers.go with `s.page(c, pageTitle(…), …)`
- [x] 2.2 Convert entity detail titles to two segments (entity + section), e.g. `agent.Name+" settings"` → `pageTitle(agent.Name, "Settings")`, `agent.Name+" memories"` → `pageTitle(agent.Name, "Memories")`
- [x] 2.3 Preserve generic fallback labels on load-error paths (e.g. `pageTitle("Agent")`, `pageTitle("Object")`, `pageTitle("Document")`) — verify no call site renders an empty segment
- [x] 2.4 grep for the literal `" — Memory"` and confirm zero remaining outside the helper — verify `go build ./...` passes

## 3. Login page + brand reuse

- [x] 3.1 Route the login page `<title>` through `pageTitle("Sign in")` (gateway/auth_ui.templ) — verify rendered title is `Sign in — Memory`
- [x] 3.2 Point `apple-mobile-web-app-title` and `layout.Navbar("Memory", …)` at the `appBrand` const — verify `templ generate` and `go build ./...` succeed

## 4. Verification

- [x] 4.1 Run `templ generate` and `go build ./...` from gateway/ — verify clean build
- [x] 4.2 Run `go test ./...` from gateway/ — verify all unit tests (incl. TestPageTitle) pass
- [x] 4.3 Manual smoke: load a list page, a detail page, and the login page — verify each tab title ends with "— Memory" and sectioned titles read "Entity — Section — Memory"
