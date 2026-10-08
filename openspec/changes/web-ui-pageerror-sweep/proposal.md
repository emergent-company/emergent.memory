## Why

#1645/#1666 redacted the tenancy/unavailable surfaces through `userFacingMemoryError` / `userFacingError`
(`apps/web-ui/gateway/memory.go`), but scoped the fix to the org/project tenancy loads. Every other gateway
handler that renders a load failure through `pageError` still passes the **raw** backend error, so a memory
outage on those pages leaks internal detail straight into the HTML — transport strings such as
`dial tcp 172.17.0.1:8080: connection refused`, the `memory 503 service_unavailable:` prefix, and
`Token validation temporarily unavailable`.

The `web-ui-degraded-backend-state` capability already requires that unavailable surfaces never expose
internal error detail, but its language ("When a tenancy load fails") is scoped to org/project loads. This
change extends that guarantee to the remaining non-tenancy `pageError` surfaces (#1671).

## What Changes

- Sweep **every** raw-error `pageError` call site in `apps/web-ui/gateway` and route the displayed value
  through `userFacingError(err)` (or the sanitizer), preserving control flow, branching, and HTTP status
  codes. Never swallow an error, never alter which branch runs, never change a status code.
- Sanitize the two raw leaks that bypass `pageError` by rendering `data.LoadErr.Error()` directly: the MCP
  connected-devices relay banner (`project_settings.templ` ← `mcp_nodes.go`) and the API-token reveal branch
  (`api_tokens.templ` ← `api_tokens_handlers.go`). The MCP nodes loader sanitizes its assembled
  `errors.Join` result once, so a nil part never becomes a non-nil empty error.
- Preserve locally-authored, already-safe messages instead of blanketing them: the backups no-org sentinel
  (`errBackupsNoOrg`) and the "backup not found" copy keep their specific, non-technical text, while a
  genuine backend/transport failure on the same page is sanitized.
- Where a raw error is passed unconditionally on the success path (e.g. documents list, skills list,
  datasources list/versions, embeddings progress, schedules list), wrap behind an `if err != nil` guard so
  the nil/empty-state gating (`loadErr == nil`) is unchanged.
- Add a table-driven gateway regression test (`TestPageErrorSweepRedactsRawBackendErrors`) that drives
  representative swept pages through their real handlers with a raw transport failure and a raw 503
  `service_unavailable` error, asserting each renders `memoryUnavailableMessage` and none of the leak
  tokens (`dial tcp`, the IP:port, `memory 5`, `service_unavailable`, `Token validation`). It fails if any
  swept site is reverted to the raw error (mutation-checked).
- Update the pre-existing tests that asserted the pre-redaction raw text to assert the sanitized message
  instead (sessions, usage, backups-unreachable).

## Capabilities

### Modified Capabilities

- `web-ui-degraded-backend-state` — extend "Unavailable surfaces never expose internal error detail" from
  org/project tenancy loads to every non-tenancy page that renders its load error through `pageError`.

## Impact

- `apps/web-ui/gateway/*.go` — the swept handlers across agent, api_tokens, approvals, backups, blueprints,
  board, datasources, documents, embeddings, inbox, mcp (`nodes`/`registry`/`servers`/`shares`), objects,
  org_members, runs, schedules, schema, schema_packs, sessions, share_owner, share_session_transcript,
  ui, and usage.
- `apps/web-ui/gateway/pageerror_sweep_test.go` — new regression test (issue #1671).
- `apps/web-ui/gateway/{backups,sessions,usage}_test.go` — expectation updates to the sanitized message.
- `apps/web-ui/gateway/memory.go` — the existing `userFacingError` / `userFacingMemoryError` helpers are
  reused; no new sanitizer is introduced.
