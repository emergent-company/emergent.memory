## Why

`apps/server/domain/health/routes.go` registers `GET /api/diagnostics` (and the sibling `GET /debug`) with **no guard at all**. Both are reachable by any caller — authenticated or not — and expose platform-tier internals:

- `/api/diagnostics` — live DB pool statistics, `pg_stat_activity` connection states, long-running query reports (with raw SQL text, up to 100 chars), Postgres settings, and the top-10 table sizes.
- `/debug` — the DB connection target (host/port/database name), pool stats, and Go runtime memory stats (dev-only; 404 in production).

This is platform-tier internal diagnostics, not project or org data, so neither a project token scope nor a bare admin scope is the correct authority (an `admin:all` token is mintable by any `org_admin`). The audit that found this cites the `routes.go` registration and the `Diagnose` handler.

## What Changes

- Gate `GET /api/diagnostics` and `GET /debug` behind `RequireAuth()` + `RequireSuperadminFull()` — the shared role-derived seam in `pkg/auth/superadmin.go` (the single `core.superadmins` authority from #947/#958). A scope gate is deliberately **not** used.
- Redact the long-running-query report: replace the raw `query` text (`left(query, 100)`) with `query_length` (`length(query)`), so an in-flight SQL body (which can embed secrets, user data, or connection targets) never leaves the server even to a superadmin.
- Leave the liveness/readiness probes (`/health`, `/healthz`, `/ready`, `/api/health`) deliberately public (load balancer/Kubernetes probes), and `/api/health/scope-authority` + `/api/metrics/*` at their existing `RequireAuth()` posture.

## Capabilities

### New Capabilities

- `platform-diagnostics`: documents the authorization posture of the platform-tier diagnostic surfaces (`/api/diagnostics`, `/debug`).

### Modified Capabilities

<!-- none -->

## Impact

- `apps/server/domain/health/routes.go` — a new platform-tier group gates `/debug` and `/api/diagnostics`.
- `apps/server/domain/health/handler.go` — `Diagnose` redacts raw query text; swagger annotations gain `@Security bearerAuth`.
- `apps/server/domain/health/diagnostics_authz_test.go` — caller-class matrix (401/403/403/200).
- `apps/server/domain/health/diagnostics_redaction_test.go` — guard against reintroducing raw query text.
- `docs/site/developer-guide/health-ops.md` — endpoint auth table + redaction note.
- **No migration, no response-shape change** (the `long_queries` items swap a `query` field for a `query_length` field).
- **Consumer impact:** on-call engineers reach diagnostics with a `superadmin_full` credential instead of unauthenticated. Probes are unaffected.
