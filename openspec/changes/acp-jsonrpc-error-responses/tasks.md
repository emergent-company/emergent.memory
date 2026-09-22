## 1. Wire error typing (TDD)

- [x] 1.1 Add `-32700`/`-32600` codes and a typed `wireError` (code, message, recoverable id) in `apps/cli/internal/acp/wire.go`. Verify: `go build ./...` compiles.
- [x] 1.2 Make `stream.next()` return `-32700` for malformed JSON and `-32600` for valid JSON that is not a Request object, echoing a recoverable `id`. Verify: `go test ./internal/acp/...` passes.
- [x] 1.3 Write the error response from the existing `stream.send` path in `apps/cli/internal/acp/run.go`, keeping the stderr log. Verify: `go test ./internal/acp/...` passes.

## 2. Tests

- [x] 2.1 Unit test malformed JSON → `-32700` with `id: null`, and that the line is still logged to stderr. Verify: `go test ./internal/acp/...` passes.
- [x] 2.2 Unit test table for `-32600` (missing method, empty object, unsupported version, non-string method, non-object literal, array) asserting the echoed/null `id`. Verify: `go test ./internal/acp/...` passes.
- [x] 2.3 Unit test that a malformed line does not stop the loop and a following valid request is still served. Verify: `go test ./internal/acp/...` passes.

## 3. Verification

- [x] 3.1 `go build ./...` (apps/cli) compiles. Verify: clean.
- [x] 3.2 `go test ./...` (apps/cli) passes. Verify: pass.
- [x] 3.3 `golangci-lint run ./...` in `apps/cli` reports 0 issues. Verify: clean.
