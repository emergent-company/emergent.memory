## 1. Target resolution

- [x] 1.1 Add `apps/server/cmd/migrate/target.go` with `Target`, `resolveTarget(getenv)`, `parseDatabaseURL`, `detectConflict`, `decideTarget`, `isMutatingCommand`, `firstNonEmpty`
- [x] 1.2 Precedence: `DATABASE_URL` > `DB_HOST`/`DB_PORT` > `POSTGRES_HOST`/`POSTGRES_PORT` > `MEMORY_PG_HOST`/`MEMORY_PG_PORT` > built-in `localhost:5432`
- [x] 1.3 Accept alias names for the other components: `MEMORY_PG_USER`, `POSTGRES_DB`, `MEMORY_PG_DB`, `POSTGRES_SSL_MODE`

## 2. Logging

- [x] 2.1 `Target.LogLine()` prints `host`, `port`, `user`, `database`, `sslmode` and the host source, and never the password
- [x] 2.2 `main.go` prints the target line before opening the DB connection, for every command

## 3. Fail closed

- [x] 3.1 `decideTarget(target, mutating, allowLocalhost)` refuses loopback targets for `up`/`up-to`/`down`/`mark-applied` when a non-loopback host env conflicts, or when the host is the built-in default
- [x] 3.2 Add the `-allow-localhost` flag to opt back in; read-only commands (`status`/`version`/`create`) are never refused
- [x] 3.3 Print a warning (stderr) when running a mutation against an explicitly-configured loopback target or under `-allow-localhost`

## 4. Audit other migrate paths

- [x] 4.1 Confirm `apps/server/internal/migrate` performs no env resolution (fx-injected `*bun.DB`) — untouched to stay disjoint from #750
- [x] 4.2 Confirm the `memory` CLI has no local goose migrate path (`schemas migrate` is server-side over the API)
- [x] 4.3 Record `memory db diagnose`'s hard-coded-localhost dev DSN as an out-of-scope report, not a fix

## 5. Spec + docs

- [x] 5.1 Add this OpenSpec change with a new `database-migrations` capability delta
- [x] 5.2 Update the migrate usage text in `main.go` (aliases, precedence, refusal rule, example)
- [x] 5.3 Update `docs/deployment/migration-drift-runbook.md` §7 for the new local-run requirement

## 6. Verification

- [x] 6.1 `cd apps/server && go build ./...`
- [x] 6.2 `cd apps/server && go test ./cmd/migrate/...`
- [x] 6.3 `cd apps/server && golangci-lint run ./cmd/migrate/...`
- [x] 6.4 `gofmt -l cmd/migrate` (clean)
- [x] 6.5 Manual smoke: refusal on the no-host path, conflict refusal, `POSTGRES_*` target logging, `-allow-localhost` opt-in (unreachable/bogus targets only; no real database touched)
- [x] 6.6 `openspec validate --strict`
