# Verify sign-in completes over HTTP and HTTPS proxy

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-fix-missing-oauth-state](../sessions/2026-09-05-fix-missing-oauth-state.md)

## What
Manually verify the full browser sign-in flow no longer fails with *"missing oauth state"*:
1. With `AUTH_MODE=session` and `PUBLIC_BASE_URL` **unset**, sign in over plain HTTP (e.g. `http://localhost:8095`) — expect success, no "missing oauth state".
2. Sign in over the TLS-terminated proxy (traefik) with `PUBLIC_BASE_URL` unset — expect the cookie stays `Secure` and sign-in succeeds.

## Why
The fix derives the cookie `Secure` flag from the request scheme when `PUBLIC_BASE_URL` is empty. `TestCookieSecure` proves the flag logic; this task confirms the browser end-to-end path (`/auth/start` → Zitadel → `/auth/callback`) under both schemes.

## Depends on
- [deploy-zitadel-auth](deploy-zitadel-auth.md) (`AUTH_MODE=session` + Zitadel config)

## Notes
- Check the `memory_oauth` cookie is present in the browser after `/auth/start` (devtools → Application → Cookies) before the Zitadel redirect.
- Over HTTP the cookie must NOT have `Secure` set; over the HTTPS proxy it must.
