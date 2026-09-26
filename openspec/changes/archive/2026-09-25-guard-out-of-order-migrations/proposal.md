## Why

On 2026-09-22 dev crash-looped for hours: migration `00172` merged and was applied **before** `00170`/`00171`, which carry lower numbers. Once `172` was recorded in `public.goose_db_version`, Goose refused to start the server with `found 2 missing migrations before current version 172` and the container restart-looped. Nothing in CI or the merge flow prevented a PR that inserts a lower-numbered migration below an already-reachable version, because each migration was individually valid; only the **merge order** produced the gap. Issue #750.

Two adjacent gaps made the incident harder to operate and to prevent:

1. The runner surfaced only Goose's raw error — the on-call had to know that `-allow-missing` is the out-of-order remedy, and the CLI had no such flag.
2. Out-of-band version records (`goose ... mark-applied`, manual `INSERT INTO goose_db_version`) bypass any post-condition check. Migration `00164` demonstrated this: its Goose row was written while the `CREATE INDEX CONCURRENTLY` never took effect (issue #734), so goose permanently de-duplicated the version and only a new forward migration could repair it.

## What Changes

- **CI guard (new required check).** `apps/server/cmd/migration-order-guard` fails a PR that adds a migration file whose version is lower than the maximum migration version already on the base branch (`origin/main`). It compares the PR's *added* `.sql` files (diff-filter `A`) against the base's max version. Renames/reformats are not additions, so they are not flagged (the existing Migration Immutability job owns those). The failure message states the exact remediation (renumber above the current max, with the next free number) and documents a deliberate escape hatch: a `-- out-of-order-migration-allowed: <reason>` line in the added migration, for the legitimate case of filling a gap. Pure logic + tests live in `apps/server/internal/migrationguard`; the thin CLI does the git plumbing; a `migration-order-guard` job is added to `.github/workflows/server.yml` and wired into the `ci` gate.
- **Startup diagnostics.** `internal/migrate.DiagnoseError` recognises Goose's `missing migrations before current version` failure and returns a rich error naming every missing `version N: file`, the current version, and the explicit remediation (`-allow-missing`, with the `renumber above the current max` alternative). `cmd/migrate` gains a `-allow-missing` flag so that remedy is actually available, and prints the rich diagnostic instead of the bare error.
- **Out-of-band write detection.** `internal/migrate` gains a loud `MarkAppliedBypassNotice` warning (logged by `Migrator.MarkApplied` and printed by the CLI's `mark-applied`), plus a read-only `FindInvalidIndexes` / `Migrator.VerifyPostConditions` check and a `-c verify` CLI command that surface indexes left invalid by a killed `CREATE INDEX CONCURRENTLY` — the exact #734/00164 signature. Historical migrations are not rewritten.
- **Docs.** `apps/server/migrations/README.md` documents the merge-order rule, the escape hatch, the gap remediation, and the verification command.

## Capabilities

### New Capabilities

- `server-migration-safety`: the merge-order rule for migrations, its CI enforcement, the deliberate gap-fill escape hatch, the startup gap diagnostic, and the out-of-band version-record verification path.

### Modified Capabilities

<!-- None. -->

## Impact

- `.github/workflows/server.yml` — new `migration-order-guard` job; added to the `ci` gate `needs`.
- `apps/server/internal/migrationguard/` — new pure guard logic + tests.
- `apps/server/cmd/migration-order-guard/` — new guard CLI (git plumbing only).
- `apps/server/internal/migrate/` — `DiagnoseError`, `MissingMigrationsError`; `FindInvalidIndexes`, `VerifyPostConditions`, `MarkAppliedBypassNotice`; `Up`/`UpTo`/`MarkApplied` wired.
- `apps/server/cmd/migrate/main.go` — `-allow-missing` flag, rich gap diagnostic, `mark-applied` warning, `-c verify` command. Target/env resolution is unchanged.
- `apps/server/migrations/README.md` — workflow documentation.
- No migration SQL is added, changed, or removed. This change adds no schema behaviour; it changes CI/deploy and operator-facing diagnostics.
