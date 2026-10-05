## Why

During dev brownouts (#1481) a live, valid project token transiently returned `401 invalid_token`, causing clients to discard a working credential, and in-flight requests hit `sql: database is closed` because the HTTP server did not always drain before the connection pool closed. A `401` is a credential verdict; it must never be emitted for a transient infrastructure failure.

## What Changes

- Classify token-validation failures by authority, not by which path happened to run:
  - **Authoritative rejection → `401 invalid_token`**: introspection reports `active=false`, userinfo returns `401`, or an `emt_*` API-token lookup finds no unexpired/unrevoked row.
  - **Infrastructure failure → `503 service_unavailable` (retryable)**: introspection/userinfo transport error, Zitadel 5xx (or any non-401 userinfo response), database/pool unavailable, or an open circuit breaker.
  - An authoritative rejection takes precedence over a concurrent infrastructure failure; a `503` never authenticates a request.
- Consult the introspection cache **before** the circuit breaker (last-known-good claims survive a Zitadel blip) and reduce the breaker cooldown from 30s to 5s.
- Raise the database pool defaults `DB_MAX_OPEN_CONNS` 25→100 and `DB_MAX_IDLE_CONNS` 5→25 (both remain env-overridable).
- Force the HTTP server's fx shutdown hook to run before the database/pool hooks by making the server depend on `*bun.DB`; raise `SHUTDOWN_TIMEOUT` 10s→30s and raise fx's own stop timeout above it (fx's 15s default otherwise caps the drain) so in-flight requests actually get the configured window.

## Capabilities

### New Capabilities

- `auth-error-semantics`: how the API classifies token-validation failures into authoritative `401` rejections versus retryable `503` infrastructure failures, the persistence of cached introspection claims, and the server-drains-before-pool-closes lifecycle invariant.

### Modified Capabilities

(none)

## Impact

- `apps/server/pkg/auth/middleware.go` — `validateToken`/`validateAPIToken` classification, introspection cache read order.
- `apps/server/pkg/auth/zitadel.go` — introspection cache/breaker ordering, `ErrIntrospectionUnavailable`/`errUserInfoInvalidToken` sentinels, userinfo status classification.
- `apps/server/pkg/apperror/error.go` — `NewServiceUnavailable` constructor.
- `apps/server/internal/config/config.go` — `SHUTDOWN_TIMEOUT`, `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS` defaults.
- `apps/server/internal/server/server.go`, `apps/server/internal/database/database.go`, `apps/server/cmd/server/main.go` — fx shutdown ordering.
- Client-visible API contract: 401 vs 503 for failed token validation. No schema or migration change.
