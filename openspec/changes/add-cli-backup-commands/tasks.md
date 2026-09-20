## 1. SDK backups package

- [x] 1.1 Add `apps/server/pkg/sdk/backups/types.go` with `Backup`, `Restore`, `CreateBackupRequest`, `ListBackupsOptions`, `ListBackupsResult`, `Cursor`, `ImportBackupInput`, `RestoreRequest`, and status constants. Verify: `go build ./...` in the SDK module compiles.
- [x] 1.2 Add `apps/server/pkg/sdk/backups/client.go` implementing `CreateBackup`, `ListBackups`, `GetBackup`, `DownloadBackup` (streamed, no client timeout, filename parsed from `Content-Disposition`), `DeleteBackup`, `ImportBackup` (multipart via `io.Pipe`), `CreateCloneRestore`, and `GetRestore`. Verify: `go build ./...` compiles.
- [x] 1.3 Register the client in `sdk.go` (import, `Backups` field, constructor in `initClients`, `SetContext` delegation). Verify: `go build ./...` compiles.
- [x] 1.4 Write `client_test.go` using `testutil.NewMockServer` covering routes, auth/org headers, query params, 202/204/201 decoding, multipart field names, 302→200 download streaming + filename fallback, and 4xx error parsing. Verify: `go test ./...` in the SDK module passes.

## 2. CLI backups and restores commands

- [x] 2.1 Add `apps/cli/internal/cmd/backups.go` with `memory backups create|list|get|download|delete|import` and `--wait`/`--timeout` polling on create. Verify: `go build ./...` compiles.
- [x] 2.2 Add `apps/cli/internal/cmd/restores.go` with `memory restores create|get` and `--wait`/`--timeout` polling on create. Verify: `go build ./...` compiles.
- [x] 2.3 Add `apps/cli/internal/cmd/backups_test.go` for pure logic: retention-days validation, status/terminal-state display helpers, and org-resolution fallback. Verify: `go test ./...` passes.

## 3. Docs, build, lint, verification

- [x] 3.1 Run `task cli:gen-docs` to regenerate the embedded CLI reference skill and commit the diff. Verify: `SKILL.md` contains `memory backups` and `memory restores`.
- [x] 3.2 Run `go build ./...`, `go test ./...`, `gofmt -l` (empty), `go vet`, and scoped `golangci-lint`; fix until clean. Verify: all pass.
- [x] 3.3 Run `openspec validate add-cli-backup-commands` from the repo root. Verify: passes.
