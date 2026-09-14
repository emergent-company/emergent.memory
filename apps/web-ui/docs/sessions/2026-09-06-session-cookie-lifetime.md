# 2026-09-06 — Decouple session cookie lifetime from access token

## Goal
Users were being logged out of the web UI after a few hours. Extend the automatic
sign-out window to ~30 days.

## Outcome
Done. Root cause was the session cookie's browser `MaxAge`, not a server bug: it was
set to `claims.ExpiresAt - now` (the short-lived Zitadel access-token lifetime), so the
browser dropped the whole cookie — refresh token included — once the access token
expired, forcing re-login. The fix decouples the browser cookie lifetime from the access
token via a new `SESSION_MAX_AGE` setting (default 30 days). Committed `ab794ed`, pushed.

## Decisions
- Decouple cookie retention from access-token expiry rather than extending the access
  token — the signed claims already enforce access-token expiry server-side and
  `ensureFreshSession` silently renews via the refresh token; only the browser lifetime
  was wrong.
- Make it configurable (`SESSION_MAX_AGE`, default 30 days) — matches the existing
  `durationOr` pattern; zero value falls back to legacy access-token lifetime (keeps
  existing tests that build `Config{}` by hand working).

## Changes
- `gateway/config.go` — added `SessionMaxAge time.Duration` field + `SESSION_MAX_AGE`
  env, default `30*24*time.Hour`.
- `gateway/auth.go` — `setSessionCookie` uses `SessionMaxAge` as the cookie `MaxAge`
  when > 0; otherwise falls back to access-token lifetime.
- `gateway/auth_session_test.go` — `TestSetSessionCookieMaxAgeDecoupled` (configured
  vs fallback).
- `gateway/config_test.go` — assert `SESSION_MAX_AGE` default + env parsing.
- `docs/spec/09-security.md`, `docs/spec/10-api-contracts.md`,
  `openspec/changes/add-auth-project-frame/design.md` — documented the decoupled
  lifetime + `SESSION_MAX_AGE`.

## Verification
- `go build ./...` — clean.
- `go test ./...` — all packages pass.
- `golangci-lint run ./...` — 0 issues on changed files; one pre-existing `handlers.go:16`
  gofmt finding (committed, not from this change).

## Open questions / follow-ups
- **Zitadel refresh-token lifetime is the true ceiling.** The 30-day cookie keeps the
  refresh token retained, but if Zitadel's refresh token expires sooner (defaults are
  short — often hours), users still get logged out when the refresh grant fails. To
  actually reach 30 days, set the refresh-token lifetime in the Zitadel app settings
  to ≥ 30 days. Tracked in `deploy-zitadel-auth` (note added).

## Tasks
- [deploy-zitadel-auth](../tasks/deploy-zitadel-auth.md) — note added: configure
  Zitadel refresh-token lifetime ≥ 30 days.
