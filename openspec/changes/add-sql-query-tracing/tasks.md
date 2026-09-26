## 1. OpenSpec artifacts

- [ ] 1.1 Author `proposal.md`, `design.md`, `tasks.md`, and the new capability delta spec `specs/observability-tracing/spec.md` with the requirements below. Verify: `openspec validate` passes and the change appears in `openspec list`.

## 2. Dependency: bunotel

- [ ] 2.1 From `apps/server`, `go get github.com/uptrace/bun/extra/bunotel@v1.2.16` (matching `bun v1.2.16`), then `go mod tidy`. Verify: `go build ./...` compiles and `go.mod`/`go.sum` record `bunotel v1.2.16`.

## 3. Implementation: register the tracing hook

- [ ] 3.1 In `apps/server/internal/database/database.go`, register `bunotel.NewQueryHook(bunotel.WithDBName(cfg.Database.Database))` on the bun DB, gated on `cfg.Otel.Enabled()`, keeping the existing `cfg.Database.QueryDebug` block untouched. Verify: `go build ./...` compiles.

## 4. Tests (TDD)

- [ ] 4.1 Add `apps/server/internal/database/query_tracing_test.go` using an in-memory span recorder (`tracetest.NewSpanRecorder`) over a `*bun.DB` backed by `sqlmock` (no real Postgres). Verify: `go test ./internal/database/...` passes.
  - [ ] 4.1.1 Hook registered only when OTel enabled: disabled → 0 spans. Verify: test passes.
  - [ ] 4.1.2 A `SELECT` produces one span named `SELECT` with `db.system=postgresql` and a non-empty `db.statement`. Verify: test passes.
  - [ ] 4.1.3 An erroring query sets span status `Error` and records the error. Verify: test passes.

## 5. Build, test, lint

- [ ] 5.1 `go build ./...`. Verify: clean.
- [ ] 5.2 `go test ./internal/database/...`. Verify: all pass.
- [ ] 5.3 `task lint` from the worktree root (or `task -d apps/server lint`). Verify: clean, or report pre-existing unrelated failures.

## 6. Commit and push

- [ ] 6.1 Stage only authored files (change dir, `database.go`, new test, `go.mod`, `go.sum`), commit as `feat(observability): emit OTel spans for bun SQL queries`, and `git push -u origin feat/sql-query-tracing`. Verify: push succeeds (no PR, no merge).
