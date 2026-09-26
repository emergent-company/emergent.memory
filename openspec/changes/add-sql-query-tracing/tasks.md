## 1. OpenSpec artifacts

- [x] 1.1 Author `proposal.md`, `design.md`, `tasks.md`, and the new capability delta spec `specs/observability-tracing/spec.md` with the requirements below. Verify: `openspec validate` passes and the change appears in `openspec list`.

## 2. Dependency: bunotel

- [x] 2.1 From `apps/server`, `go get github.com/uptrace/bun/extra/bunotel@v1.2.16` (matching `bun v1.2.16`), then `go mod tidy`. Verify: `go build ./...` compiles and `go.mod`/`go.sum` record `bunotel v1.2.16`.

## 3. Implementation: register the tracing hook

- [x] 3.1 In `apps/server/internal/database/database.go`, register `bunotel.NewQueryHook(bunotel.WithDBName(cfg.Database.Database))` on the bun DB, keeping the existing `cfg.Database.QueryDebug` block untouched. Verify: `go build ./...` compiles.
- [x] 3.2 Gate the hook on `cfg.Features.Tracing && cfg.Otel.Enabled()`: the tracing module (which installs the OTLP `TracerProvider`) is only loaded when `FEATURE_TRACING` is set, so registering on `Otel.Enabled()` alone would create no-op spans on every query. Verify: `TestTracingHook_FeatureDisabled_NoSpans` passes.

## 4. Tests (TDD)

- [x] 4.1 Add `apps/server/internal/database/query_tracing_test.go` using an in-memory span recorder (`tracetest.NewSpanRecorder`) over a `*bun.DB` backed by `sqlmock` (no real Postgres). Verify: `go test ./internal/database/...` passes.
  - [x] 4.1.1 Hook registered only when tracing enabled: disabled → 0 spans. Verify: test passes.
  - [x] 4.1.2 A `SELECT` produces one span named `SELECT` with `db.system=postgresql` and a non-empty `db.statement`. Verify: test passes.
  - [x] 4.1.3 An erroring query sets span status `Error` and records the error. Verify: test passes.
  - [x] 4.1.4 Hook not registered when `FEATURE_TRACING` is off, even with an OTLP endpoint set. Verify: `TestTracingHook_FeatureDisabled_NoSpans` passes.

## 5. Build, test, lint

- [x] 5.1 `go build ./...`. Verify: clean.
- [x] 5.2 `go test ./internal/database/...`. Verify: all pass.
- [x] 5.3 `task lint` from the worktree root (or `task -d apps/server lint`). Verify: clean, or report pre-existing unrelated failures.

## 6. Commit and push

- [x] 6.1 Stage only authored files (change dir, `database.go`, new test, `go.mod`, `go.sum`), commit, and `git push`. Verify: push succeeds (no merge by the author).
