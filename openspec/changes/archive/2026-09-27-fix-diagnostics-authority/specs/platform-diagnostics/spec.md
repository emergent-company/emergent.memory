## Purpose

Document the authorization posture and secret-redaction behaviour of the platform-tier diagnostic surfaces served by the health domain: `GET /api/diagnostics` and `GET /debug`.

## ADDED Requirements

### Requirement: Gate platform-tier diagnostics on superadmin_full

The platform-tier diagnostic endpoints SHALL be gated on an active `superadmin_full` grant — a `core.superadmins` row with `role = 'superadmin_full'` and `revoked_at IS NULL`, enforced by the shared `RequireSuperadminFull` middleware behind `RequireAuth`.

- `GET /api/diagnostics` SHALL require `RequireAuth()` + `RequireSuperadminFull()`.
- `GET /debug` SHALL require `RequireAuth()` + `RequireSuperadminFull()`.

The diagnostic surfaces SHALL NOT be satisfied by a scope check: an `admin:all` token is mintable by any `org_admin`, so a scope gate (`admin:read`/`admin:write`/`admin`) would admit a non-platform-admin caller. The admitted set is `superadmin_full` only — `superadmin_readonly`, `org_admin`, and any `admin`/`admin:all` token SHALL be refused.

The liveness/readiness probes (`/health`, `/healthz`, `/ready`, `/api/health`) SHALL remain deliberately public, and `/api/health/scope-authority` and `/api/metrics/*` SHALL remain at their existing `RequireAuth()` posture.

#### Scenario: Unauthenticated caller denied

- **WHEN** an unauthenticated caller requests `/api/diagnostics` or `/debug`
- **THEN** the server responds 401

#### Scenario: Authenticated non-superadmin denied

- **WHEN** an authenticated caller without a `superadmin_full` grant requests `/api/diagnostics` or `/debug`
- **THEN** the server responds 403

#### Scenario: org_admin's admin:all token denied

- **WHEN** an `org_admin` carrying an `admin:all` token requests `/api/diagnostics`
- **THEN** the server responds 403 (the platform-admin authority is the role, not the scope)

#### Scenario: superadmin_full admitted

- **WHEN** an active `superadmin_full` principal requests `/api/diagnostics` or `/debug`
- **THEN** the request reaches the handler (200, or 404 for `/debug` in production)

### Requirement: Redact raw SQL text from diagnostics

The `/api/diagnostics` long-running-query report SHALL NOT expose raw SQL text. It SHALL report only the query length (`query_length`), `pid`, `duration`, and `state`, so an in-flight query's literals — which may embed secrets, user data, or connection targets — never leave the server, even to a `superadmin_full` caller.

#### Scenario: Long-running query report redacts query body

- **WHEN** a `superadmin_full` caller requests `/api/diagnostics` while a long-running query is active
- **THEN** the `long_queries` items contain `query_length` but never a raw `query` field
