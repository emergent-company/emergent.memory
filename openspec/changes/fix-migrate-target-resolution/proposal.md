# Fix `emergent-migrate` target resolution and fail closed on implicit localhost

## Why

`apps/server/cmd/migrate` (the `emergent-migrate` binary embedded in the server image and wrapped by `task migrate:*`) resolved its PostgreSQL target from `DB_HOST`/`DB_PORT` only. It did not understand `POSTGRES_HOST`/`POSTGRES_PORT` — the names used by compose services, `.env` files, `apps/server/internal/config`, and the container entrypoint (`deploy/self-hosted/entrypoint.sh` maps `DB_HOST="${POSTGRES_HOST:-db}"` itself). When neither was set it silently fell through to `localhost:5432`, and nothing in the output stated the resolved target.

That combination produced the issue **#754** incidents: a lane intending its own scratch database applied migrations `00156`–`00176` to the shared local `memtest-db` (`localhost:5432`), which another session had deliberately pinned at `155`. The substitution was invisible at runtime and there was no safe undo (rolling back the `00174` FTS backfill and the HNSW index swaps was riskier than leaving it forward). Refs #734, #750.

## What Changes

- **Accept `POSTGRES_HOST`/`POSTGRES_PORT` as first-class aliases** in the migrate command, together with `MEMORY_PG_*` as a lowest-precedence alias, so the command understands the same names as the rest of the stack. Additional name aliases are accepted for user (`MEMORY_PG_USER`), database (`POSTGRES_DB`, `MEMORY_PG_DB`) and sslmode (`POSTGRES_SSL_MODE`) to remove the same class of host/port mismatch on the other target components.
- **Log the resolved target prominently before connecting.** A single line printed to stdout before `sql.Open` states `host`, `port`, `user`, `database`, `sslmode` and which env pair (or built-in default) supplied the host. The password is never logged.
- **Fail closed on ambiguous loopback targets.** A mutating command (`up`, `up-to`, `down`, `mark-applied`) refuses to run when the resolved host is loopback (`localhost`, `127.0.0.1`, `::1`, `0.0.0.0`) and either (a) another host env var (`DB_HOST`/`POSTGRES_HOST`/`MEMORY_PG_HOST`) is set to a non-loopback value that differs from the resolved host, or (b) no host env var was configured at all and the loopback target is the built-in default. A new `-allow-localhost` flag opts back in; it is required for all loopback mutations except an explicitly-and-unambiguously configured loopback target (which is allowed with a warning). Read-only commands (`status`, `version`, `create`) are never refused so the target can still be inspected.
- **Documented precedence:** `DATABASE_URL` (complete override) > `DB_HOST`/`DB_PORT` > `POSTGRES_HOST`/`POSTGRES_PORT` > `MEMORY_PG_HOST`/`MEMORY_PG_PORT` > built-in `localhost:5432`. `DB_*` wins because it is the migrate tool's own explicit override and the `task migrate:*` wrapper already derives it from `POSTGRES_*`.
- **Update the migration-drift runbook** to document the new local-run requirement (explicit host, explicit `DATABASE_URL`, or `-allow-localhost`).

### Audit of the same defect elsewhere

- `apps/server/internal/migrate` (`Migrator`) receives an already-constructed `*bun.DB` via fx and performs **no** env resolution, so it does not share the silent-localhost default. It already logs `running database migrations`; broader startup diagnostics are owned by #750, so this change leaves it untouched.
- The `memory` CLI (`apps/cli`) has **no** local goose migrate path: `memory schemas migrate` drives server-side migration jobs over the API and never resolves a Postgres target. No duplicate.
- `apps/cli/internal/cmd/db_diagnose.go` builds a DSN with a hard-coded `localhost` host from `~/.memory/config/.env.local` for the hidden `memory db diagnose --verbose`-only dev utility. It is the same *shape* of hard-coded-localhost default but is not a migration path and is intentionally local-only; it is reported rather than fixed here to avoid scope creep.

## Capabilities

### Added Capabilities

- `database-migrations`: the `emergent-migrate` command's target resolution precedence, startup target logging, and the fail-closed policy for mutating commands against loopback targets.

## Impact

### Code Changes

- `apps/server/cmd/migrate/target.go` (new): `Target`, `resolveTarget`, `parseDatabaseURL`, `detectConflict`, `decideTarget`, `isMutatingCommand`, `firstNonEmpty`.
- `apps/server/cmd/migrate/target_test.go` (new): precedence, alias, conflict, fail-closed, logging and DSN unit tests.
- `apps/server/cmd/migrate/main.go`: `-allow-localhost` flag; `resolveTarget` replaces the inline component resolution; target line + fail-closed decision printed before connecting; usage text documents aliases, precedence and the refusal rule.

### Configuration

- Env vars newly understood by `emergent-migrate`: `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_SSL_MODE`, `MEMORY_PG_HOST`, `MEMORY_PG_PORT`, `MEMORY_PG_USER`, `MEMORY_PG_DB`.
- New flag: `-allow-localhost`.

### Operational Impact

- `task migrate:up` / `task migrate:down` and a bare `POSTGRES_PASSWORD=… go run ./cmd/migrate -c up` now **refuse** when `POSTGRES_HOST`/`DB_HOST` are unset and the target would default to localhost. Local operators pass `-allow-localhost` (or set `POSTGRES_HOST=localhost` / an explicit `DATABASE_URL`).
- Container deployments are unaffected: the entrypoint exports a non-loopback `DB_HOST` (`db` by default) and compose services set `POSTGRES_HOST`.
- The resolved target is visible on every invocation, including refusals.

### Testing Requirements

- Unit tests cover: built-in defaults, `POSTGRES_*` honored, `DB_*` precedence over `POSTGRES_*`, `MEMORY_PG_*` fallback, `DATABASE_URL` full override, database-name/ssl aliases, missing-password and malformed-`DATABASE_URL` errors, conflict detection, each `decideTarget` branch, mutating-command classification, and that `LogLine` never contains the password.
- Manual evidence: refusal on the incident path, conflict refusal, `POSTGRES_*` resolution logging, and `-allow-localhost` opt-in (see tasks).
