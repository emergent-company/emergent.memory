## Why

`apps/server/pkg/sdk/a2a/client.go` `SSEStream.Next()` matched `data: ` with `strings.HasPrefix(line, "data: ")`, which requires exactly one space after the colon. Per the Server-Sent Events spec a `data` field may be followed by zero or more spaces or a tab, and consecutive `data` lines belonging to one event must be concatenated with `\n` before dispatch, with the event terminated by a blank line.

Concretely, `data:{}` (no space) or `data:\t{...}` were silently skipped, and a multi-line event was dropped after its first line. The client works against the Memory server (which always emits `data: `), but breaks against a conformant third-party SSE emitter. Found during review of #713 (issue #720).

## What Changes

- Parse SSE lines as `field: value` instead of an exact-prefix match, so `data:`, `data: `, `data:   `, and `data:\t...` are all accepted as `data` fields.
- Strip at most a single leading space from the field value, per the spec; a leading tab is payload and is preserved rather than stripped.
- Accept CR, LF, and CRLF as line terminators (including a lone CR, which `bufio.ScanLines` did not handle).
- Buffer consecutive `data:` field values and dispatch the event only when a blank line terminates it, joining payload lines with `\n`.
- Ignore `event:`, `id:`, `retry:`, and comment (`:`) lines while keeping the stream advancing.
- Discard an event that is not terminated by a blank line at end of stream.
- Preserve the `[DONE]` sentinel and the existing behaviour for Memory server output (`data: {...}\n\n`).

## Capabilities

### Modified Capabilities

- `a2a-conformance`: Add client-side SSE event framing requirements covering single-space stripping with tab preservation, CR/LF/CRLF line termination, multi-line data concatenation, blank-line dispatch, and ignored non-data fields.

## Impact

- `apps/server/pkg/sdk/a2a/client.go` — `SSEStream.Next()` only.
- `apps/server/pkg/sdk/a2a/client_test.go` — new table-driven parser tests.
- No wire-format, server, or API changes; `data: {...}\n\n` behaviour is unchanged.
