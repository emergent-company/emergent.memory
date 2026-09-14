# 2026-09-05 — Fix "missing oauth state" sign-in failure

## Goal
Diagnose and fix the browser sign-in failure *"Sign-in failed — missing oauth state"* on the `/auth/callback` round trip.

## Outcome
Done. Root cause found, fixed, tested, committed (`e32844a`), pushed to `master`.

The `memory_oauth` flow cookie was being dropped by the browser, so `/auth/callback`
found no cookie and returned "missing oauth state". Cause: `cookieSecure()` returned
`true` whenever `PUBLIC_BASE_URL` was unset (or `https://…`), because
`strings.HasPrefix("", "http://") == false`. Over plain HTTP the browser silently drops
`Secure` cookies, so the state cookie set by `/auth/start` never survived to the
callback. The documented intent ("empty = derive from request Host", which is `http:`)
was not honored.

## Decisions
- **Derive `Secure` from the request scheme when `PUBLIC_BASE_URL` is empty** (`c.Scheme()` → `X-Forwarded-Proto`/TLS) — consistent with `publicBaseURL()`, fixes plain-HTTP dev hosts, and keeps TLS-terminated proxies working.
- **Keep explicit `PUBLIC_BASE_URL` as a hard pin** — `http://…` forces insecure (dev override), any other scheme forces `Secure`.
- **Unit test the flag, not the whole flow** — `TestCookieSecure` covers all five cases (dev mode, http pin, https pin, empty+HTTP, empty+HTTPS proxy).

## Changes
- `gateway/auth.go` — `cookieSecure()` signature changed to `cookieSecure(c echo.Context)`; empty `PublicBaseURL` now derives from `c.Scheme() == "https"`. All 5 internal call sites (`setSessionCookie`, `clearSessionCookie`, `clearOAuthStateCookie`, `rememberRecentProject`, `ensureInstallID`) pass `c`.
- `gateway/oidc.go` — `authStartWithPrompt` passes `c` to `cookieSecure(c)`.
- `gateway/auth_test.go` — added `TestCookieSecure`.

## Verification
- `go build ./...` (in `gateway/`) — pass (after the unrelated parallel `objects.go` WIP was fixed by another session).
- `go test ./...` — pass; `TestCookieSecure` green.
- `go vet ./...` — pass.
- `golangci-lint run ./...` — 0 issues.
- `gofmt -l gateway/auth.go gateway/oidc.go gateway/auth_test.go` — clean.
- `git push origin master` — `01ac858..e32844a`.

## Open questions / follow-ups
- **End-to-end browser verification deferred** — the unit test proves the flag logic, but a live sign-in over plain HTTP (and over the TLS proxy) still needs a manual check to confirm "missing oauth state" is gone.
- **`deploy-zitadel-auth` remains the gating task** — flipping `AUTH_MODE=session` + Zitadel config is still tracked separately and will exercise this fix in production.

## Tasks
- [verify-signin-over-http](../tasks/verify-signin-over-http.md) — manual browser check that sign-in completes over HTTP and HTTPS proxy.
