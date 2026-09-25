## 1. SDK project selector

- [x] 1.1 Add `projectID` + `NewClientWithProject` / `WithProject` / `setProject` to the A2A client, applied in `doJSON` and `StreamMessage`; verify a unit test asserts `X-Project-ID` is sent
- [x] 1.2 Omit the header when no project is configured; verify a unit test asserts no selector is sent

## 2. CLI threading

- [x] 2.1 Thread the selected project (global `--project` flag overrides config/env) into the A2A client; verify a unit test covers flag-over-config
- [x] 2.2 Confirm the streaming path (`memory a2a send --mode stream`) uses the same client construction

## 3. Server error contract

- [x] 3.1 `acpProjectID` returns 400 `INVALID_ARGUMENT` / `PROJECT_REQUIRED` naming `X-Project-ID`; verify a unit test asserts status and reason
- [x] 3.2 A project-scoped `emt_*` token binds the request to its own project; verify a unit test asserts the token project wins over a mismatched header
- [x] 3.3 `a2aErrorFromStatus` maps 400/404/405 to their own reasons and reserves `INVALID_AGENT_RESPONSE` for 5xx; verify unit tests assert the specific reason per status
- [x] 3.4 Streaming routes convert handler errors to the A2A envelope; verify a route-level test asserts 400 `PROJECT_REQUIRED` on `/message:stream`

## 4. Documentation

- [x] 4.1 Correct the `a2a_routes.go` route comment to describe header/token project selection

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./...`
- [x] 5.2 `cd apps/server && go test -count=1 ./pkg/sdk/a2a/... ./domain/agents/...`
- [x] 5.3 `cd apps/cli && go build ./... && go test -count=1 ./internal/cmd/...`
- [x] 5.4 `golangci-lint run ./pkg/sdk/a2a/... ./domain/agents/...` and `gofmt -l` on changed files
- [x] 5.5 `openspec validate --strict`
