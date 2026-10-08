## 1. CLI: `--cursor` flag on `memory journal list` (TDD)

- [x] 1.1 Add a `--cursor` string flag and send it as the `cursor` query param when set (the server already supports keyset pagination).
- [x] 1.2 Surface the `next_cursor` token after a non-final page (`Next cursor: <token>` + `Use --cursor <token> to fetch the next page.`), replacing the token-less hint; JSON output already emits `next_cursor`/`has_more`.
- [x] 1.3 Update the command help text to document `--cursor`.
- [x] 1.4 CLI tests: `--cursor` (and existing `--since`/`--limit`/`--branch`/`--include-branches`) flag present; command structure. Verify: `go test ./internal/cmd/... -count=1`.

## 2. Verify

- [x] 2.1 `cd apps/cli && go build ./... && go vet ./... && go test ./... -count=1`.
- [x] 2.2 `cd apps/server && go build ./... && go test ./domain/journal/... -count=1` (server cursor already shipped; sanity check).
- [x] 2.3 `openspec validate cli-journal-cursor-pagination --strict`.
