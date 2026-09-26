## Context

Goose orders migrations by the numeric version parsed from `NNNNN_name.sql`. When it finds a migration file whose version is lower than a version already recorded in `goose_db_version`, it refuses to run `up` unless `-allow-missing` is passed. On 2026-09-22 this hard-blocked dev startup: `00172` was recorded before `00170`/`00171` (issue #750). Each PR was individually valid; only the merge order produced the gap. The existing "Migration Immutability" CI job only rejects *changes to existing* migration files, not additions that land below an already-reachable version.

## Decisions

### D1 — Enforce the rule in CI by comparing added files against the base branch maximum

The guard computes the maximum migration version on the base branch (`origin/main`) and fails if any migration file **added by the PR** has a lower version. This is the correct discriminator: a PR that adds `00177` when the base is at `00176` is fine; a PR that adds `00170` when the base is at `00176` is not, because `00176` may already be applied anywhere.

Alternatives rejected:

- **"Version must be unique" / "must equal max+1"** — too strict (a single PR commonly adds several consecutive migrations, and gaps in the base are tolerated today) and misses the actual incident, where every version was unique.
- **Comparing `HEAD` against the base's max at merge time via a bot** — cannot fail the PR; the check must be a PR status check so the merge is blocked.

Only `--diff-filter=A` additions are considered. Renames/reformats/deletions are the Immutability job's concern, so the guard does not double-report them and cannot false-positive on them. A PR that adds no migrations is always green.

### D2 — Escape hatch is an explicit in-file directive, not a label

Some additions below the base max are legitimate: deliberately filling a known gap whose lower version was never applied anywhere. The guard exempts an added migration that carries:

```
-- out-of-order-migration-allowed: <reason>
```

The reason must be non-empty (a bare directive does not exempt). Rejected alternatives: a PR **label** would need `pull_request` trigger types `[labeled, unlabeled]` added to `server.yml` (otherwise the check does not re-run when the label is applied) and is outside the diff; a separate marker **file** leaves a permanent artifact on `main` and needs an added/modified-in-diff rule to stay inert. The in-file directive is visible in the diff, travels with the migration it justifies, cannot be forgotten after merge, and requires a written reason.

### D3 — Gap diagnostics live in `internal/migrate`; the CLI only bridges

`DiagnoseError` parses Goose's error text (header + tab-indented `version N: file` entries) and returns a `*MissingMigrationsError` carrying the explicit remediation. Parsing the message is the only option: Goose exposes no typed gap error, and the CLI is the process that must boot or exit. The diagnostic lives in the migration package (testable without a DB); `cmd/migrate` merely prints it and gains the `-allow-missing` flag that the remediation names. Env/target resolution in `cmd/migrate` is untouched (owned by another lane).

### D4 — Out-of-band detection uses a read-only catalog check, not per-migration post-conditions

`mark-applied` and manual `INSERT INTO goose_db_version` intentionally skip the migration. A recorded-but-not-applied version is only detectable by checking the effects the migration should have produced. Re-deriving arbitrary per-migration expectations would require parsing migration SQL; instead the check targets the concrete, recurring signature from #734/00164: indexes left **not** (`indisvalid AND indisready`) by a killed/never-completed `CREATE INDEX CONCURRENTLY`. This is one cheap read-only query over `pg_index`, deterministic, and directly names the failing objects. It is exposed three ways: a loud warning printed by `mark-applied`, a `Migrator.VerifyPostConditions` hook for the programmatic API, and `-c verify` for operators. Historical migrations are immutable, so the fix is detection + forward repair, matching the `00170`/`00175` `indisvalid` guard convention.
