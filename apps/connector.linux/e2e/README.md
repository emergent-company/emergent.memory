# connector end-to-end test

Hermetic end-to-end test for the `memory-connector` daemon.

It builds the **real** `memory-connector` binary once and runs it as a
subprocess against an **in-process fake relay hub** (an `httptest` server using
`gorilla/websocket`). The full path is exercised:

1. config YAML → daemon start
2. WebSocket dial → `register` frame (instance id + tool list)
3. hub-initiated `tools/call` for `linux-host-info` → `response` frame
4. `status --config <cfg> --json` → `schema_version == 1`, matching
   `instance_id`, `linux-host-info` in `tools`, `hub_state == "connected"`
5. second `daemon` on the same config fails with "already running" (flock)
6. `SIGTERM` → clean exit 0
7. hub drops the connection → daemon reconnects and re-registers

Everything is local (`127.0.0.1`); there is no external network and no secret.
Every wait is bounded by a timeout, so it is CI-safe.

## Run

From `connector/`:

```sh
go test -count=1 ./e2e/...
```

Run it twice to shake out flakes:

```sh
go test -count=1 ./e2e/... && go test -count=1 ./e2e/...
```

The whole package is skipped under `-short` (and the binary build is skipped
with it):

```sh
go test -short ./e2e/...
```

The Linux test asserts the Linux built-in tool set (`linux-host-info`) and is
skipped on other hosts.

## Darwin variant

A second test (`TestDarwinDaemonRegisterAndStatus` in
`mac_daemon_e2e_test.go`) covers the darwin engine↔hub handshake. It builds the
real `memory-connector` binary (the same `TestMain`/`buildConnectorBinary` build,
no duplication) and runs it against the same in-process fake hub, asserting:

1. a `register` frame arrives whose tool list **contains** the Apple tool ids the
   engine registers on darwin (`notes_search`, `notes_create`, `reminders_list`,
   `reminders_lists`, `reminders_add`, `reminders_update`, `reminders_delete` —
   sourced from `internal/appletools` and cross-checked against
   `MemoryConnector/Sources/ToolCatalog.swift`);
2. `status --config <cfg> --json` reports the matching `instance_id`,
   `hub_state == "connected"`, and `schema_version == 1`.

It is gated on `runtime.GOOS == "darwin"` and skips under `-short`, so on Linux
it skips (the Linux test runs) and the Linux test is unchanged. It is run on a
macOS host the same way as the Linux test:

```sh
cd apps/connector.linux
go test -count=1 ./e2e/...
```

On macOS the native `go build` produces a darwin binary, so the harness exercises
the real darwin engine path end to end.

The tool-*call* round-trip stays **Linux-only**: the permission-free probe tool
(`linux-host-info`) has no darwin analogue, and invoking the Apple tools would
require live TCC/EventKit/AppleScript grants a hermetic test must not assume.
Darwin asserts the handshake and registration only (registration does not
execute the tool).

## Notes

- The fake hub reuses the production frame types from
  `internal/relay` (`RegisterFrame`, `RequestFrame`, `ResponseFrame`, ...), so
  the test cannot silently drift from the wire protocol.
- The reconnect assertion allows up to 45s because the daemon has no backoff
  override; the relay client reconnects after `DefaultBackoffBase` (30s).
  Keeping this test out of the default fast path (`-short`) keeps normal test
  runs quick.
