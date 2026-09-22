## 1. Parser

- [x] 1.1 Parse SSE lines as `field: value` (`strings.Cut`) instead of an exact `data: ` prefix match
- [x] 1.2 Strip at most one leading space and/or one leading tab from the field value
- [x] 1.3 Buffer consecutive `data:` values and dispatch only on a terminating blank line, joining with `\n`
- [x] 1.4 Ignore `event:`, `id:`, `retry:`, and comment (`:`) lines without stalling the stream
- [x] 1.5 Discard an event not terminated by a blank line at end of stream
- [x] 1.6 Preserve `[DONE]` sentinel handling and Memory server `data: {...}\n\n` behaviour

## 2. Tests

- [x] 2.1 Table-driven cases: no space, single space, multiple spaces, tab
- [x] 2.2 Table-driven cases: multi-line data, multi-event chunk, bare `data:` empty payload
- [x] 2.3 Table-driven cases: comments/metadata/unknown fields ignored, unterminated event discarded
- [x] 2.4 `[DONE]` sentinel terminates the stream without dispatching later payloads

## 3. Verification

- [x] 3.1 `cd apps/server/pkg/sdk && go build ./...`
- [x] 3.2 `cd apps/server/pkg/sdk && go test ./...`
- [x] 3.3 `golangci-lint run ./...` in `apps/server/pkg/sdk`
