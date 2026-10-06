## Context

Today the MCP relay (`apps/server/domain/mcprelay`) is invocation-only: a device registers with an
`instance_id` and its tool list, and the only server→device work is a JSON-RPC `tools/call`. Device
MCP servers are configured locally (connector YAML + loopback management API) and the server never
sees their command/env/url/headers. The previous product (Diane) kept this configuration in the
Memory graph — `MCPProxyConfig` (`config` JSON + `scope` + `version`) and `MCPSecret` — merged by
scope, pushed to the node in-band, and re-pushed on SSE entity changes via `mcp_config_watch`.

We restore the server-authoritative behaviour on the current architecture.

## Goals / Non-Goals

- **Goal:** a project's assistant can create, inspect, and edit MCP server configuration for its
  connected devices, and the device applies it live without hand-editing files.
- **Goal:** reconnect is self-healing — a device receives its configuration on connect.
- **Non-Goal:** replacing the connector's local management API or local YAML for locally-configured
  servers. Those stay; server-managed and local servers coexist.
- **Non-Goal:** per-device tool enable/deny lists in v1 (see D7).
- **Non-Goal:** a project-member-reachable HTTP push endpoint (see D6).

## Decisions

### D1: Store device config in `kb.mcp_servers`, not graph objects

Diane used generic graph objects because no structured registry existed. The current server already
has `kb.mcp_servers` with structured transports, a tool catalog (`kb.mcp_server_tools`), and
AES-GCM secret columns (`secret_env`, `secret_headers`, migration `00151`). Extending that table is
less code, reuses encryption and the agent-tool surface, and avoids a second config store.

- `target` `text NOT NULL DEFAULT 'server'` CHECK IN (`'server'`, `'device'`).
- `target_instance_id` `text NULL`; null means "all connected devices of the project" (subject to
  D6 authorization).
- `builtin` type is rejected for device targets (the API already rejects builtin on create).

### D2: Store the device's owner and an opt-in flag at register

A configuration push can define a process the device executes, so a project member must not be able
to target an arbitrary device. The `register` frame gains:

- `accepts_managed_config bool` — the connector advertises this only when its operator explicitly
  opts in (connector setting / flag, **default false**). The server SHALL NOT push configuration to
  a device that does not advertise it.
- the registering subject is captured from the authenticated request (`auth`) and recorded on the
  session as `OwnerSubject`.

Authorization for device-targeted writes: only the device's `OwnerSubject` (matched by instance) or
a project admin may create/update/delete a device-targeted row. A broadcast row (`instance_id` null)
carries no owner identity and is hydrated to every opted-in device of the project on (re)connect, so
it is **admin-only**; a non-admin must name a specific device they own. This closes the cross-member
RCE hole (a member's command executing on another member's device). The existing `TrustedInternalFromContext`
(`apps/server/domain/mcp/entity.go:81-90`) is an **in-process agent-tool marker, not HTTP
middleware**, so it must not be relied on to authorize an HTTP route.

### D3: Push, don't poll — and push after the register ack, asynchronously

The server pushes a `config` frame to the targeted opted-in session(s). Push is chosen over Diane's
SSE-watch because the relay connection already exists and is always open.

- The `config` frame is distinct from `request` (D4 below).
- **Ordering:** `Service.Register` fires `OnChange` synchronously *before* `handler.Connect` writes
  the `registered` ack (`handler.go:78-86`), and the connector's `waitForAck` drops non-ack frames
  (`client.go:540-567`). Hydration therefore MUST NOT use `OnChange`. `handler.Connect` writes the
  ack first, then triggers hydration through a post-ack hook (`Service.NotifyRegistered`).
- **Non-blocking:** every push runs on its own goroutine with a bounded context so a connected-but-
  slow device cannot stall registry writes or the register handler. Pushes are best-effort; an
  offline device is reconciled at its next register (no durable queue in v1).
- The device acknowledges with a `response` (ok/error + non-secret status).

### D4: `config` frame is distinct from `request`

A JSON-RPC-shaped `request` frame is `tools/call`-only on the device. Reusing it for config would
mean overloading `method`. A dedicated `FrameConfig` keeps the two paths unambiguous and lets the
device reply with the normal `response` ack envelope.

### D5: Secret handling — encrypted at rest, never logged, never echoed, decrypted in mcpregistry

Pushed env/headers may contain secret values. They:

- are stored AES-GCM encrypted at rest server-side (`secret_env`/`secret_headers`) and returned by
  the API only as key names (`SecretEnvKeys`/`SecretHeadersKeys`);
- are decrypted **inside `mcpregistry.Service`** (the only holder of the encryptor) when building a
  push — never in the relay adapter;
- travel only over the authenticated TLS relay for the device's own project;
- are stored locally on the device via its secret store;
- are **never** included in ack, registration, or status payloads;
- are **never logged** at any layer (resolved frames and decrypted values SHALL NOT be passed to
  `slog`).

`DeviceServerConfig` is a distinct resolved type (defined in `mcpregistry`); decrypted values MUST
NOT flow through the existing `ToDTO`/`stripSecretKeys`/tool-pool cache paths. This is a deliberate,
documented revision of the "secrets never leave the device" statement **scoped to opted-in,
server-managed servers**; locally-configured servers are unchanged.

### D6: No member-reachable HTTP push route

There is no new Echo route that pushes configuration. The stack has only auth + project-member
middleware on `/api/mcp-relay/*` (`routes.go`); exposing a push route there would let any project
member execute a process on another member's device. Pushes originate **in-process** from the
registry service via the `DeviceConfigPusher` interface, driven by the assistant's opt-in,
confirmation-gated agent tool. This removes the HTTP RCE surface entirely.

### D7: No per-server `disabled_tools` in v1

`kb.mcp_servers` has no per-server deny list, and per-tool enablement lives in
`kb.mcp_server_tools`, which is only populated by server-side tool-sync — which device rows are
excluded from. So a device server has no tool rows to derive a deny list from. v1 omits
`disabled_tools` from the pushed config (the connector's own local deny list still applies to
locally-configured servers). Tool selection for server-managed servers is deferred.

### D8: Device-targeted rows never enter the server's outbound paths

Excluding `target='device'` rows from `FindAllEnabledTools` is necessary but not sufficient. The
filter must also cover the slug-mode resolver (`FindAllServers` path in `proxy.go`), and the
`DiscoverAndSyncTools`, `InspectServer`, and `CallToolOnServer` service paths, so a device row can
never be connected, tool-synced, or called outbound by the server.

### D9: Config version guard

`ConfigFrame` carries a monotonic `version` (per instance). The connector records the last applied
version and rejects an older-or-equal frame as stale (ack `stale`), so a replayed/out-of-order frame
cannot revert a device's configuration. The connector persists the last applied version, so the
server's version must be monotonic **across server restarts** too: it is derived from wall-clock
nanoseconds (bumped past the previous value for strict monotonicity), not a process-local counter
that would reset to zero and make every post-restart push look stale.

### D10: Apply order and isolation on the connector

Validate → **apply via reload** → **persist only on success** → re-register → ack. Persisting before
a reload that then fails would leave a bad entry that breaks the next restart. The apply runs on a
**goroutine with a panic recover** (like `dispatch`/`invoke`), so a slow or panicking apply cannot
block the relay read loop or kill the connector.

## Risks / Trade-offs

- **Reverses a stated invariant.** Mitigated by opt-in (D2), owner authorization, no HTTP route
  (D6), never-log/never-echo (D5), and documentation. Reviewers should confirm the trust boundary:
  only an opted-in device's own owner (or an admin) can push.
- **Best-effort delivery.** No durable per-device queue; register-time hydration reconciles.
- **Broadcast fan-out.** Admin-only, and fans out to every opted-in device of the project.
- **Live reload blast radius.** Validate + apply-before-persist + panic-recovered goroutine.

## Migration

Additive migration with defaults; existing rows become `target='server'` and are unaffected. No
backfill. Rollback drops the two columns.
