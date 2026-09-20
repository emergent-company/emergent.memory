## Purpose

Defines the gateway-side public surface for a shareable agent page: the `/share/` BFF page, the sealed-cookie key exchange and per-request HMAC ref signing, header stripping and log redaction, edge rate limiting, anti-indexing/privacy headers (with Sentry deliberately absent), and the session-list and approve/deny surface.

## ADDED Requirements

### Requirement: Public page lives in the gateway as the BFF
The public share page SHALL live in the web gateway and be reachable under `/share/agent`, which the gateway SHALL allowlist in its public auth path. The server SHALL keep zero unauthenticated routes; the gateway SHALL forward the share key as a Bearer credential plus the signed end-user ref headers to the authenticated server endpoints.

#### Scenario: Public gate allows only /share/
- **WHEN** an unauthenticated visitor requests a path under `/share/`
- **THEN** the gateway serves it without requiring login, and no other path is widened

#### Scenario: Server routes stay authenticated
- **WHEN** the gateway forwards a share request upstream
- **THEN** the request reaches a server endpoint as an authenticated Bearer call with the signed ref headers, never as an unauthenticated server route

#### Scenario: Other public paths remain gated
- **WHEN** an unauthenticated visitor requests a path outside `/share/` and outside the existing public set
- **THEN** the existing auth gate still applies

### Requirement: Fragment key is exchanged once for a sealed cookie
The share key SHALL travel in the URL fragment and SHALL be exchanged once for an AES-256-GCM sealed HttpOnly cookie named `memory_share` that carries the key (encrypted) and the anonymous end-user ref. The gateway SHALL then sign the end-user ref per-request as `X-End-User-Ref` + `X-End-User-Ref-Sig` = base64url-nopad HMAC-SHA256(`SHARE_REF_SECRET`, ref), failing closed when the ref secret is unset.

#### Scenario: Key stays in the fragment and is sealed
- **WHEN** a share link is loaded
- **THEN** the key is read from the URL fragment only, exchanged once, and sealed (AES-256-GCM) into the `memory_share` cookie so the raw key is never recoverable by the client or logged

#### Scenario: Ref is signed per request
- **WHEN** the gateway calls a share endpoint upstream
- **THEN** it sends `X-End-User-Ref` (UUID v4) and `X-End-User-Ref-Sig`, and never sends an unsigned ref when `SHARE_REF_SECRET` is unset (fails closed as 503)

### Requirement: Cookie-gated config rehydration
The gateway SHALL expose `GET /share/api/config`, a cookie-gated route that returns the sanitized public config for the bound link. It SHALL return only the sanitized config — never the key/token and never project/org identifiers — so a plain refresh or return visit without a URL fragment can rehydrate from the cookie alone, without re-running the key exchange.

#### Scenario: Refresh rehydrates without re-exchanging
- **WHEN** an end user refreshes or returns to the share page with a valid `memory_share` cookie but no URL fragment
- **THEN** `GET /share/api/config` returns the sanitized public config for the bound link

#### Scenario: Config never leaks the key
- **WHEN** `GET /share/api/config` responds
- **THEN** the body carries no key, token, project id, or org id

#### Scenario: Config requires a valid cookie
- **WHEN** `GET /share/api/config` is called without a valid sealed cookie
- **THEN** the gateway returns 401 (or 503 when the share surface is unconfigured) and no config is served

### Requirement: Header strip and log redaction
The gateway SHALL NOT send client `X-Project-ID` or `X-Org-ID` headers on share requests, and SHALL redact query parameters named `key` or `token` from its access logs.

#### Scenario: Project and org headers are never sent
- **WHEN** the gateway makes a share request upstream
- **THEN** it sends no `X-Project-ID` or `X-Org-ID` header, so the server always resolves project/org from the bound link

#### Scenario: Key/token is redacted from logs
- **WHEN** the gateway logs a request whose query carries a `key` or `token` parameter
- **THEN** the parameter value is masked before the URI is written

### Requirement: Edge rate limiting and hashed-IP audit
The gateway SHALL rate-limit the public share surface at the edge: per direct socket peer (the trusted reverse proxy, e.g. traefik) always, and per share link (SHA-256 of the token) when the request carries a valid share cookie. Rate state SHALL be in-memory and therefore per-instance, with a bounded number of keys. The first exchange SHALL be per-IP only (no cookie yet). Upstream audit SHALL record only a SHA-256 hashed IP, never a raw IP.

#### Scenario: Per-IP rate limit keys on the socket peer
- **WHEN** the gateway rate-limits by IP
- **THEN** it keys on the direct socket peer address (the reverse proxy), not the client-supplied `X-Forwarded-For` chain, so the dimension is non-spoofable but does not distinguish end users behind the proxy

#### Scenario: Per-IP rate limit always applies
- **WHEN** the socket peer exceeds its per-minute limit (default 30, burst 10)
- **THEN** the gateway returns 429 with the `rate-limited` code

#### Scenario: Per-link rate limit is the authoritative dimension
- **WHEN** a request carries a valid share cookie and its link (SHA-256 of the token) exceeds the per-link limit (default 60/min, burst 20)
- **THEN** the gateway returns 429 with the `rate-limited` code

#### Scenario: Exchange is per-IP only
- **WHEN** the key exchange runs (no cookie yet)
- **THEN** only the per-IP limiter applies, and the per-link limiter is skipped

#### Scenario: Rate state is per-instance and bounded
- **WHEN** the gateway rate-limits
- **THEN** buckets are held in memory per instance, capped at a fixed key count, and idle buckets are evicted

#### Scenario: Audit records a hashed IP
- **WHEN** the gateway forwards a share access event upstream
- **THEN** the server records a SHA-256 hex digest of the client IP, never the raw IP

### Requirement: Anti-indexing and privacy headers scoped to /share/
The gateway SHALL serve the `/share/*` surface (and only that surface) with `Referrer-Policy: no-referrer`, `X-Robots-Tag: noindex, nofollow`, `X-Content-Type-Options: nosniff`, and a Content-Security-Policy that includes `frame-ancestors 'none'` and a deliberate `'unsafe-inline'` allowance for the page's inline bootstrap.

#### Scenario: Headers apply only to /share/
- **WHEN** a request hits the `/share/*` group
- **THEN** the four security headers are set, and the authenticated app routes are unaffected

#### Scenario: Noindex and privacy headers present
- **WHEN** the public share page is served
- **THEN** the response carries `Referrer-Policy: no-referrer`, `X-Robots-Tag: noindex, nofollow`, and `X-Content-Type-Options: nosniff`

#### Scenario: Frame embedding is blocked
- **WHEN** the share page's CSP is applied
- **THEN** it includes `frame-ancestors 'none'`, so the page cannot be framed

### Requirement: Sentry is absent from the public share page
The public share page SHALL NOT initialize Sentry or load any third-party script. This is a deliberate consequence of the strict share CSP, which disallows the Sentry CDN and outbound reporting on the anonymous surface.

#### Scenario: No third-party scripts on the share page
- **WHEN** the share page is rendered
- **THEN** it does not wire Sentry or any third-party script, because the CSP would block them

### Requirement: Session list and approve/deny surface
The public page SHALL render the end user's own sessions (respecting `show_session_list` and the `filter` parameter) and SHALL render an approve/deny surface for pending approvals and `ask_user` questions. The surface SHALL support approve/deny only; no richer approval UI is in scope.

#### Scenario: Session list shows only own sessions
- **WHEN** an end user loads the public page and `show_session_list` is true
- **THEN** the page lists only that end user's own sessions for the link, filtered by `filter` (default active)

#### Scenario: Approve/deny surface
- **WHEN** a pending approval or question exists for the end user's session
- **THEN** the page renders approve and deny actions; no richer approval controls are offered

### Requirement: Session detail with transcript is surfaced end-to-end
The gateway SHALL carry the session detail — including its `messages` transcript — through from the server, so resuming a session renders its history client-side. `GET /share/api/sessions/:id` SHALL return the session's transcript (`messages`) for the caller's session.

#### Scenario: Resuming a session renders its history
- **WHEN** an end user opens one of their own sessions
- **THEN** the gateway returns the session detail including its `messages` transcript, and the client renders the prior user/assistant history

#### Scenario: Transcript is scoped to the caller's session
- **WHEN** an end user requests a session id they do not own on the link
- **THEN** no transcript is returned and no history leaks
