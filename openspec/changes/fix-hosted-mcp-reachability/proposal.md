## Why

The Mac connector's **Hosted MCP Servers** page reports

> Can't load hosted MCP servers — Can't reach the connector's local management
> API (Could not connect to the server.).

after the connector engine has been restarted. The engine exposes a loopback
management API on `127.0.0.1:8931` that the page depends on, but
`EngineManager.restart()` calls `stop()` (SIGTERM — asynchronous) and `start()`
back-to-back. The successor engine is spawned while the previous child still
holds the port; its `ListenAndServe` fails with `address already in use`, and
because the relay only *logs* that failure and keeps running, the process stays
alive and connected to the hub with a **dead management API**. Every restart —
connecting a project, editing tools, toggling all tools, an account-scope swap —
can therefore silently kill the page.

Two further problems compound it:

1. The relay prints `management API listening on …` unconditionally, so a bind
   failure is never visible outside the log.
2. The page loads unconditionally and renders the raw transport error, leaking
   an internal "engine"/"management API" concept and giving the user no way to
   tell "not connected yet" from "broken".

## What Changes

- **Engine supervision (Mac app).** A restart waits for the previous child to
  fully exit before spawning its successor, so the new engine binds the
  management port cleanly. A bounded SIGKILL fallback prevents a hung child from
  wedging restarts.
- **Management API bind (connector).** The relay binds its loopback listener
  explicitly before serving, retries a transient bind failure for a bounded
  window (riding out a restart), and exits non-zero if it still cannot bind
  instead of running with a dead management API.
- **Page state (Mac app).** The Hosted MCP Servers page reflects the connector's
  general availability: it loads only when the connector is up, disables
  `Add`/`Browse registry` when it is not, shows a connection-oriented
  placeholder when no project is connected, and a generic unavailable error with
  a Retry action otherwise. The raw transport error is never shown.

## Capabilities

### Modified Capabilities

- `mac-connector-app`: a restart sequences on the previous child's exit; the
  hosted MCP servers page is availability-gated and never surfaces internal
  engine/API detail.
- `mcp-connector`: the management API binds before serving and fails fast (never
  runs with a dead API).

## Impact

- `apps/connector.mac/MemoryConnector/Sources/EngineManager.swift` — restart
  sequencing + SIGKILL fallback.
- `apps/connector.mac/MemoryConnector/Sources/MCPServersPage.swift`,
  `MCPServersStore.swift`, new `MCPServersAvailability.swift` — availability
  gating and generic messages.
- `apps/connector.mac/MemoryConnector/Tests/` — engine restart-decision, store
  message mapping, availability tests.
- `apps/connector.linux/cmd/memory-connector/relay.go` (+ test) — listener bind
  retry / fail fast.
