# auth-error-semantics Specification

## Purpose
Define how the API classifies token-validation failures into authoritative `401` rejections versus retryable `503` infrastructure failures, so a transient provider or database outage never causes a client to discard a valid credential, while genuine invalid credentials still fail closed.

## Requirements

### Requirement: Authoritative token rejections return 401

The system SHALL return `401 invalid_token` when a validation path authoritatively rejects the credential: token introspection reports the token inactive (`active=false`), the OIDC userinfo endpoint returns `401`, an API-token lookup finds no matching, unexpired, unrevoked row, or an API token's owning user profile no longer exists. An authoritative rejection SHALL take precedence over any concurrent infrastructure failure, so a genuine rejection is never reported as retryable.

#### Scenario: Introspection reports the token inactive

- **WHEN** introspection reports `active=false` for a token and the userinfo fallback then fails for an infrastructure reason
- **THEN** the middleware returns `401 invalid_token` and authenticates no user

#### Scenario: Userinfo rejects the token

- **WHEN** the userinfo endpoint returns `401` for a token that introspection did not accept
- **THEN** the middleware returns `401 invalid_token` and authenticates no user

#### Scenario: An inactive introspection verdict is never laundered by userinfo

- **WHEN** introspection authoritatively reports `active=false` for a token, even if the userinfo endpoint would return a subject for the same token
- **THEN** the middleware returns `401 invalid_token` and authenticates no user, without falling through to the userinfo path

#### Scenario: API token is unknown or expired

- **WHEN** the API-token lookup for an `emt_*` token finds no matching, unexpired, unrevoked row
- **THEN** the middleware returns `401 invalid_token` and authenticates no user

#### Scenario: API token's owning user profile is missing

- **WHEN** an `emt_*` token row resolves to a `user_id` whose profile no longer exists and the follow-up profile lookup fails with a `missing-row` (`sql.ErrNoRows`) error
- **THEN** the middleware returns `401 invalid_token` and authenticates no user

#### Scenario: Standalone deployment has no external identity provider

- **WHEN** standalone mode is enabled and a token matches neither an `emt_*` API token nor a recognised development test token
- **THEN** the middleware returns `401 invalid_token` without attempting external introspection or userinfo, because the absent provider is not an infrastructure failure and a permanently unrecognised token must not be reported as retryable

### Requirement: Infrastructure token-validation failures return 503

The system SHALL return `503 service_unavailable` when every configured validation path fails for infrastructure reasons — an introspection or userinfo transport error, a non-`401` (including 5xx) userinfo response, an unavailable database or connection pool (including a failure of an API token's follow-up user-profile lookup), or an open circuit breaker — and no path authoritatively rejected the credential. A `503` response SHALL NOT authenticate the request; it signals the client to retry.

#### Scenario: Introspection and userinfo are both unreachable

- **WHEN** introspection and the userinfo fallback both fail with transport errors for a non-`emt_*` token
- **THEN** the middleware returns `503 service_unavailable` and authenticates no user

#### Scenario: Introspection cache is unavailable while the provider is down

- **WHEN** the introspection cache read fails for a reason other than a normal cache miss and the provider cannot be reached
- **THEN** the middleware returns `503 service_unavailable` and authenticates no user

#### Scenario: API-token lookup fails because the database is unavailable

- **WHEN** the API-token lookup fails with a non-`missing-row` database error
- **THEN** the middleware returns `503 service_unavailable` and authenticates no user

#### Scenario: API-token profile lookup fails because the database is unavailable

- **WHEN** the profile lookup for a valid `emt_*` token (whose user row exists) fails with a non-`missing-row` database or connection-pool error
- **THEN** the middleware returns `503 service_unavailable` and authenticates no user

### Requirement: Introspection cache is consulted before the circuit breaker

The system SHALL consult the last-known-good introspection cache before skipping introspection due to an open circuit breaker, so a transient provider blip does not lock out a token whose claims are still cached. Cached-claim exposure SHALL be bounded by the configured introspection cache time-to-live.

#### Scenario: Warm cache survives a provider transport blip

- **WHEN** introspection cannot be reached but a non-expired cached entry exists for the token
- **THEN** validation succeeds from the cached claims and the request is authenticated

### Requirement: Server drains before the database pool closes

The system SHALL order its shutdown hooks so the HTTP server stops accepting connections and drains in-flight requests before the database and connection-pool shutdown hooks run, bounded by the configured shutdown timeout, so no live handler is served from a closed pool.

#### Scenario: Graceful shutdown ordering

- **WHEN** the application shuts down
- **THEN** the HTTP server drains first and only afterwards do the database and connection pool close
