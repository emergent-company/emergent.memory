## 1. Auth error classification

- [x] 1.1 Classify token-validation failures in `validateToken`/`validateAPIToken` so authoritative rejections return `401 invalid_token` and infrastructure failures return `503 service_unavailable`, with an authoritative rejection taking precedence over any concurrent infrastructure error; verify `go build ./...`
- [x] 1.2 Add `apperror.NewServiceUnavailable` (503, code `service_unavailable`) that attaches the underlying infrastructure error for internal logging; verify `go vet ./...`
- [x] 1.3 Add unit tests pinning transport error → 503, DB cache unavailable → 503, API-token DB error → 503, inactive introspection → 401, and an authoritative introspection rejection that outranks a subsequent infrastructure fallback failure → 401; verify `go test ./pkg/auth/...`
- [x] 1.4 Add a unit test proving a warm introspection cache keeps validating during a transport blip; verify `go test ./pkg/auth/...`
- [x] 1.5 Mutation-check the classification: making a real rejection return 503 (or an infrastructure error return 401) MUST fail a test; verify `go test ./pkg/auth/...` fails under the mutation
- [x] 1.6 Short-circuit an authoritative introspection-inactive rejection so a later userinfo success cannot authenticate the token, and map a user-profile lookup failure to 401 (missing user) or 503 (database/pool unavailable); verify `go test ./pkg/auth/...`
- [x] 1.7 In standalone mode (no external IdP), reject unrecognised tokens with `401` instead of treating the absent provider's transport error as a retryable `503`; verify `go test ./pkg/auth/...`

## 2. Introspection cache and circuit breaker

- [x] 2.1 Consult the introspection cache before the circuit-breaker check and reduce the breaker cooldown 30s → 5s; verify `go test ./pkg/auth/...`
- [x] 2.2 Return a retryable infrastructure sentinel from the provider layer for transport/5xx/circuit-open failures and an authoritative sentinel for userinfo `401`; verify `go test ./pkg/auth/...`
- [x] 2.3 Document the `Introspect` return contract: `nil, nil` only for disabled/unconfigured introspection, infrastructure failures return an error, and an inactive token returns `Active=false`; verify `go vet ./pkg/auth/...`

## 3. Database pool defaults

- [x] 3.1 Raise `DB_MAX_OPEN_CONNS` 25 → 100 and `DB_MAX_IDLE_CONNS` 5 → 25, both env-overridable; verify `go test ./internal/config/...`
- [x] 3.2 Add a tag-level unit test pinning the `SHUTDOWN_TIMEOUT`, `DB_MAX_OPEN_CONNS`, and `DB_MAX_IDLE_CONNS` defaults independently of ambient environment; verify `go test ./internal/config/...`

## 4. Shutdown ordering

- [x] 4.1 Make the HTTP server's fx lifecycle hook depend on `*bun.DB` so the server's `OnStop` runs before the database and pool hooks, and raise `SHUTDOWN_TIMEOUT` 10s → 30s; verify `go build ./...`
- [x] 4.2 Set fx's stop timeout above `SHUTDOWN_TIMEOUT` (default +5s) so the configured drain window is reachable rather than capped by fx's 15s default; verify `go build ./...`
- [x] 4.3 Add a unit test pinning the fx reverse-registration stop order (server before database) and a structural test pinning that the server's start function declares the database dependency; verify `go test ./internal/server/...`
- [x] 4.4 Mutation-check the ordering pin: removing the database dependency MUST fail the structural test; verify `go test ./internal/server/...` fails under the mutation
