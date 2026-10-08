## Why

`memory journal list` has no `--cursor` flag. The server endpoint
`GET /api/graph/journal` already supports keyset cursor pagination (shipped in
`315ca5785`, PR #1632 — it returns `next_cursor` and `has_more`), but the CLI
never sends a cursor and prints only a bare "more entries available" hint with
no way to act on it. `memory objects list`, `sessions list`, and `backups list`
all expose `--cursor`; the journal command is the odd one out.

Issue: emergent-company/emergent.memory#1660.

## What Changes

- Add a `--cursor` flag to `memory journal list` that passes the token through
  to the server's existing `cursor` query parameter, resuming the feed after the
  last entry of the previous page.
- Surface the actual `next_cursor` token after a non-final page instead of the
  current token-less hint, so it can be copied into `--cursor`.
- `--output json` already carries `next_cursor`/`has_more`; unchanged.
- Default behavior with no `--cursor` is unchanged.

The server-side cursor contract (`cursor` query param, `next_cursor` /
`has_more` response fields, `(created_at, id)` keyset, 400 on invalid cursor)
already exists on `main` and is **not** modified by this change.

## Capabilities

### New Capabilities
- `cli-journal`: the `memory journal` CLI surface — listing entries with
  `--since`, `--limit`, `--branch`, `--include-branches`, and `--cursor` keyset
  pagination, plus adding notes.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/cli/internal/cmd/journal.go`: `--cursor` flag, cursor query param,
  next-page output, help text.
- Tests: `apps/cli/internal/cmd/journal_test.go`.
- No server changes. Backwards compatible: no existing flag changes meaning.
