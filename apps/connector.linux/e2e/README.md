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

The test is Linux-only: it asserts the Linux built-in tool set
(`linux-host-info`) and is skipped on other hosts.

## Notes

- The fake hub reuses the production frame types from
  `internal/relay` (`RegisterFrame`, `RequestFrame`, `ResponseFrame`, ...), so
  the test cannot silently drift from the wire protocol.
- The reconnect assertion allows up to 45s because the daemon has no backoff
  override; the relay client reconnects after `DefaultBackoffBase` (30s).
  Keeping this test out of the default fast path (`-short`) keeps normal test
  runs quick.
