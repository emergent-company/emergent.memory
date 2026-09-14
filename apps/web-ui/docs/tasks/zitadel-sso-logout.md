# Zitadel SSO logout (end_session)

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-add-auth-project-frame](../sessions/2026-09-04-add-auth-project-frame.md)

## What
Wire `POST /auth/logout` to also terminate the Zitadel session via the OIDC
`end_session` endpoint, instead of only clearing the local cookie.

## Why
Today logout clears the gateway's session cookie but leaves the Zitadel SSO session alive, so
a re-login would silently resume. The memory.web-ui client (`src/auth/oidc.ts`
`buildEndSessionUrl`) already shows the pattern: `{issuer}/oidc/v1/end_session` with
`id_token_hint` + `client_id` + optional `post_logout_redirect_uri`.

## Depends on
- `deploy-zitadel-auth` (needs live Zitadel to verify).

## Notes
- The id_token (needed for `id_token_hint`) is already decoded at callback but not retained;
  either retain it in the session claims or send `client_id` only.
- Keep local cookie clear even if end_session fails; decide whether to redirect to the
  `post_logout_redirect_uri` or back to `/`.

## Done (2026-09-10)
`POST /auth/logout` now terminates the Zitadel SSO session when possible, while local
sign-out stays unconditional.

- `sessionClaims` gained `IDToken` (retained at callback), and the inactive-account
  registry entries carry it too so a switched-to account still sends `id_token_hint`.
- `authLogout` reads the active session (best-effort), drops it from the registry,
  clears the session + oauth-state cookies **first**, then redirects to the discovered
  `end_session_endpoint`. If discovery/`end_session_endpoint` is unavailable it falls
  back to `/`. The install-id cookie and other cached accounts are untouched (design D5).
- URL: `end_session_endpoint?client_id=…[&id_token_hint=…][&post_logout_redirect_uri=…]`.
  `id_token_hint` is sent only when retained; `post_logout_redirect_uri` only when
  `PUBLIC_BASE_URL` is non-empty (`base + "/"`), so no unregistered redirect is sent.
- Cookie-size tradeoff: the session cookie already carries the access + refresh tokens,
  and browser cookies are capped at ~4 KB. Retaining a JWT-sized id_token on top can
  exceed that, in which case the browser silently drops the cookie and the user cannot
  stay signed in at all. `setSessionCookie` therefore sheds `IDToken`
  (`maxSessionCookieValueBytes = 3800`) when the signed value would exceed the cap and
  logout falls back to `client_id`-only. `id_token_hint` is a best-effort hint, so this
  is the safer trade.
- Tests: `oidc_test.go` covers the end_session redirect (id_token_hint/client_id/
  post_logout_redirect_uri rules), the `/` fallback when discovery lacks end_session,
  cookie clearing, install-id/cached-account preservation, and the cookie-size shed;
  `session_test.go` round-trips the new `IDToken` field.

Spec: no dedicated spec entry exists for this route; behavior is documented here.
