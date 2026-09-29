## 1. Reproduce

- [x] 1.1 Fail-first DB-backed worker test: a `project-invitation` job with a missing template must not be sent and must not be marked `sent`
- [x] 1.2 Fail-first test: an invite with a missing accept URL must not be sent
- [x] 1.3 Fail-first test: an invite with a relative accept URL must not be sent

## 2. Fix

- [x] 2.1 `processJob` fails on a missing template instead of using the generic fallback
- [x] 2.2 `processJob` fails on a render error instead of using the generic fallback
- [x] 2.3 Validate required template content (`acceptUrl` absolute for `project-invitation`; `mcpUrl`/`apiKey` for `mcp-invite`) and fail the job when missing

## 3. Regression + Verification

- [x] 3.1 Happy-path test: a renderable invite with an absolute accept URL is sent, contains the link, and is marked `sent`
- [x] 3.2 `go build ./...` (apps/server)
- [x] 3.3 Targeted DB-backed tests for `domain/email` (`REQUIRE_DB=1`)
- [x] 3.4 `task lint`, `gofmt -l`, `openspec validate --all --strict`
