## 1. Module migration (apps/server)

- [x] 1.1 Rewrite every `google.golang.org/adk/...` import to
  `google.golang.org/adk/v2/...` under `apps/server`. Verify: `go build ./...`
  resolves (no `adk` (non-v2) import remains: `grep -rc '"google.golang.org/adk/' --include=*.go apps/server` shows only `/v2` paths).
- [x] 1.2 `apps/server/go.mod`: require `google.golang.org/adk/v2 v2.5.0`, raise
  the Go directive to `1.26.6`; refresh `go.sum`. Verify: `GOFLAGS=-mod=mod go get google.golang.org/adk/v2@v2.5.0`.
- [x] 1.3 Update `go.work` to Go `1.26.6` and refresh `go.work.sum`.
  Verify: `go work sync` exits 0.
- [x] 1.4 Fix `session.NewEvent(id)` → `session.NewEvent(ctx, id)` at every call
  site (`domain/extraction/agents/{pipeline,pipeline_two_phase,quality_checker}.go`,
  `domain/agents/executor.go`). Verify: `go build ./...`.
- [x] 1.5 Unify `tool.Context` / `agent.CallbackContext` → `agent.Context`
  (~30 sites in `domain/agents`, `domain/skills`, `domain/agentcompat`,
  `pkg/adk`) and add the `google.golang.org/adk/v2/agent` import where missing.
  Verify: `go build ./...` green.

## 2. e2e isolation

- [x] 2.1 Add `GOWORK: "off"` to the `e2e` jobs in
  `.github/workflows/e2e.yml` (CLI Install, API, Mailgun) so runlog's pinned
  `adk v0.6.0` is not compiled against adk/v2's newer `otel`.
  Verify: `cd e2e && GOWORK=off go build ./...` exits 0.

## 3. Verification

- [x] 3.1 `cd apps/server && go build ./...` and `go vet ./...` both green.
- [x] 3.2 `gofmt -l` clean on every changed Go file.
- [x] 3.3 `go test ./domain/extraction/... ./pkg/adk/...` and
  `go test -short ./domain/{agents,skills,chat,discoveryjobs,provider,agentcompat}/...`
  pass — behaviour parity of the agent runtime across the module move.
- [x] 3.4 `task lint` (root lefthook) passes for the server tree. (CI-owned; green on
  PR #1740 — `server-ci`, `Lint`, `cli-ci`, `Unit Tests`, `Test`, `API Tests` all pass.)
- [x] 3.5 `openspec validate adk-v2-migration --strict` passes.
