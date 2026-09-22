## Why

The ACP stdio loop (`apps/cli/internal/acp/run.go`) reaches the client with only a stderr log when `stream.next()` rejects an inbound line: invalid JSON, an unsupported `jsonrpc` version, or a missing method. The server side is conformant-compatible with JSON-RPC 2.0 clients (Paseo, Claude Code, Gemini CLI) that expect `-32700` (Parse error) or `-32600` (Invalid Request) echoed back, so those clients hang or misbehave waiting for a reply that never arrives (#719).

## What Changes

- Emit a JSON-RPC 2.0 error response for any inbound line that cannot be decoded into a valid Request object.
- Malformed JSON → `-32700` Parse error; structurally valid JSON that is not a valid Request (bad/missing `jsonrpc`, missing/invalid `method`, non-object value) → `-32600` Invalid Request.
- Echo the request `id` when it was recoverable from the input; use `id: null` otherwise.
- Keep the existing stderr diagnostic for operator visibility and the existing single response-writing path (`stream.send`); stop swallowing the failure silently.
- Leave the terminal-read-error path (over-long line / read failure) unchanged: no response is possible there and the loop still returns.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `cli-acp`: malformed inbound messages now get a JSON-RPC error frame instead of silence.

## Impact

- `apps/cli/internal/acp/wire.go`: `-32700`/`-32600` codes, a `wireError` carrying the recoverable id, and `next()` returning typed errors.
- `apps/cli/internal/acp/run.go`: the decode-error branch logs and writes the error response.
- `apps/cli/internal/acp/acp_test.go`: unit tests per error class.
