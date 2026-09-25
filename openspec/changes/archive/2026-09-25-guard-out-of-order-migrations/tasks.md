## 1. CI guard — merge-order enforcement (issue #750)

- [x] 1.1 Add `apps/server/internal/migrationguard/guard.go`: `VersionFromFilename`, `MaxVersion`, `ExemptReason` (escape hatch `-- out-of-order-migration-allowed: <reason>`), `AddedMigration`, `Violation`, `FindViolations` (version < base max, exempt skipped), `FormatViolationError` (states base max, offending files, renumber remediation with next free version, and the escape hatch). Verify: `go test ./internal/migrationguard/`.
- [x] 1.2 Add `apps/server/internal/migrationguard/guard_test.go` covering: no added migrations (no false positive), all added above base max, one below, below-but-exempt, baseMax 0/negative, sorted output, filename parsing, directive parsing (case-insensitive, empty reason not exempt), and failure-message contents. Verify: `go test ./internal/migrationguard/ -count=1 -v`.
- [x] 1.3 Add `apps/server/cmd/migration-order-guard/main.go`: resolve repo root via `git rev-parse --show-toplevel`, compute base max from `git ls-tree -r --name-only <base> -- <dir>`, added files from `git diff --name-only --diff-filter=A <base>...HEAD -- <dir>`, read added files for the exemption directive, exit 1 with the failure message on a violation, exit 0 otherwise, exit 2 on git/IO error. Verify: run against `origin/main` (no migration changes) → exit 0.
- [x] 1.4 Wire `migration-order-guard` job into `.github/workflows/server.yml` (PR-only, `fetch-depth: 0`, base ref `origin/${base_ref}`) and add it to the `ci` gate `needs`. Verify: YAML parses; job present.

## 2. Startup gap diagnostics

- [x] 2.1 Add `apps/server/internal/migrate/diagnose.go`: `MissingMigration`, `MissingMigrationsError` (`Error`/`Unwrap`), `DiagnoseError` parsing Goose's gap header + tab-indented entries and returning the explicit `-allow-missing` remediation plus raw error. Verify: `go test ./internal/migrate/`.
- [x] 2.2 Add `apps/server/internal/migrate/diagnose_test.go`: nil, non-gap passthrough, raw and wrapped gap parsing (exactly the two missing entries), remediation text, unwrap. Verify: `go test ./internal/migrate/ -run TestDiagnose -count=1 -v`.
- [x] 2.3 Wire `DiagnoseError` into `Migrator.Up`/`UpTo`. Verify: covered by package test.
- [x] 2.4 Add a `-allow-missing` flag to `apps/server/cmd/migrate` and use `goose.WithAllowMissing()` for `up`/`up-to`; print `migrate.DiagnoseError(err)` on failure. Verify: `go build ./...`; flag listed in `printUsage`.

## 3. Out-of-band write detection

- [x] 3.1 Add `apps/server/internal/migrate/postcondition.go`: `InvalidIndex`, `InvalidIndexQuery` (read-only `pg_index` scan for `kb`/`core` indexes not valid+ready), `FindInvalidIndexes`, `Migrator.VerifyPostConditions` (loud error log + descriptive error), `MarkAppliedBypassNotice`. Verify: `go test ./internal/migrate/`.
- [x] 3.2 Add `apps/server/internal/migrate/postcondition_test.go` (pure assertions; DB test skipped without `TEST_DATABASE_URL`). Verify: package test passes.
- [x] 3.3 Log `MarkAppliedBypassNotice` in `Migrator.MarkApplied`; print it from `cmd/migrate mark-applied`; add `-c verify` to run `FindInvalidIndexes` and exit non-zero on invalid indexes. Verify: `go build ./...`; `-c verify` in `printUsage`.
- [x] 3.4 Do NOT rewrite historical migrations (immutable; CI "Migration Immutability" job). The `indisvalid` guard convention of `00170`/`00175` is followed, not duplicated into old files.

## 4. Docs + spec

- [x] 4.1 Document the merge-order rule, escape hatch, gap remediation and `-c verify` in `apps/server/migrations/README.md`.
- [x] 4.2 Add this OpenSpec change (`server-migration-safety` capability) since CI/deploy behaviour changes. Verify: `openspec validate --strict`.

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server`.
- [x] 5.2 `go test` for every touched package plus the existing migration tests.
- [x] 5.3 `golangci-lint run` on the touched packages (ignore pre-existing repo-wide issues).
- [x] 5.4 Evidence: construct the failing case (a temp branch adding a low-numbered migration → exit 1) and the passing case (no migrations / forward migration / exempt gap-fill → exit 0); paste raw output.
