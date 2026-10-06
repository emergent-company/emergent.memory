# Tasks

## 1. Data model

- [ ] 1.1 Add migration `add_device_target_to_mcp_servers.sql` (next sequential number): `target text NOT NULL DEFAULT 'server' CHECK (target IN ('server','device'))`, `target_instance_id text NULL`; down migration drops both. Unit: migration applies and reverts.
- [ ] 1.2 Extend the `mcpregistry` entity/store with `Target`/`TargetInstanceID`, defaults, and validation (`server`|`device`; instance id only meaningful for `device`; reject `builtin` for device). Unit tests for defaults and invalid target.
- [ ] 1.3 Exclude `target='device'` rows from **every** server outbound path, not just one: `FindAllEnabledTools` (repository.go:353), the slug-mode `FindAllServers` resolver (proxy.go ~196), and `DiscoverAndSyncTools` / `InspectServer` / `CallToolOnServer` (service.go). Unit tests per path proving a device row is never connected, tool-synced, or called outbound, and a `server` row is unchanged.

## 2. Relay control channel

- [ ] 2.1 Add `FrameConfig` + `ConfigFrame{ID, Version, Servers}` to `mcprelay/service.go`; unit test it is distinct from `request`/`response` and round-trips.
- [ ] 2.2 Add `Session.SendConfig(ctx, frame) (ack, error)` non-blockingly (own goroutine + bounded ctx at the call site) with the same pending-map/serialized-write discipline as `SendRequest`; unit test with a fake WS peer (ok, error, timeout) and that it does not block the caller.
- [ ] 2.3 Add `Service.PushConfigToInstance(projectID, instanceID, frame)` and `Service.PushConfigToProject(projectID, frame)`; skip (no error) unregistered or non-opted-in sessions; unit tests.
- [ ] 2.4 Add a **post-ack** hydration hook (`Service.NotifyRegistered`) and call it from `handler.Connect` **after** the `registered` ack is written (handler.go:82-86). Unit test asserts no config frame precedes the ack (the connector's `waitForAck` drops pre-ack frames).
- [ ] 2.5 Extend the `register` frame with `accepts_managed_config` and record the authenticated `OwnerSubject` on the session; unit test that a non-opted-in device is never pushed to.
- [ ] 2.6 Define the resolved wire type `DeviceServerConfig` (name/transport/enabled/command/args/env/url/headers) in `mcpregistry` (so the relay adapter imports it one-way, no cycle) and wire the `DeviceConfigPusher` into `mcprelay.Service`/`NewService`/module params. Round-trip test.
- [ ] 2.7 Add the `version` guard: `ConfigFrame.Version` monotonic per instance; ack `stale` for an older/equal version.
- [ ] 2.8 ~~Add `POST /api/mcp-relay/sessions/:instanceId/config`~~ **Not in v1 (D6)** — no member-reachable push route; pushes are in-process only.

## 3. Registry wiring and assistant surface

- [ ] 3.1 Define a `DeviceConfigPusher` interface in `mcpregistry` and provide an `mcprelay` adapter through fx (mirrors existing cross-domain adapters, e.g. discoveryjobs/extraction); unit test the pusher is injected and invoked.
- [ ] 3.2 Resolve + decrypt + merge device-targeted rows **inside `mcpregistry.Service`** (the encryptor holder) into `DeviceServerConfig`; unit test plaintext resolution and that decrypted values never pass through `ToDTO`/`stripSecretKeys`.
- [ ] 3.3 On create/update/delete of a `device`-targeted row, push asynchronously (goroutine, bounded ctx) to the matching instance or the owner's devices; unit test each trigger and that `server` rows do not push and a slow/offline device does not block the write.
- [ ] 3.4 Authorize device-targeted writes: only the device's `OwnerSubject` or a project admin may target an instance; a broadcast (no instance id) is admin-only (no owner identity exists to scope hydration, so a non-admin broadcast would be a cross-member RCE). Unit tests for owner, non-owner member (denied), admin, and non-admin broadcast (denied).
- [ ] 3.5 Extend the MCP-server agent tools with `target`/`target_instance_id` (register device-targeted create/update as **opt-in** tools requiring confirmation, per `acpTools` in toolpool.go:57-64) and add a device server list tool; wire names in both dispatch switches. Unit tests for create-with-target, list-filtered, update-target, and that the tool is stripped unless opted in.
- [ ] 3.6 Unit test that device-row secret responses expose only key names.

## 4. Connector

- [ ] 4.1 Add the config frame type + parser (+ `version`) to `internal/relay/frames.go`; unit tests for valid/unknown/malformed.
- [ ] 4.2 Add a config applier dispatched on a **goroutine with a panic recover** (like `dispatch`/`invoke`), never inline in the read loop; unit test the read loop stays responsive and a panicking apply does not kill the connector.
- [ ] 4.3 Applier order: validate (`mcphost.ValidateServers`) → apply via `registryManager.reload` → **persist the Memory-managed set only after a successful reload** → re-register → ack. Unit tests: valid applies live and persists; invalid (incl. `Start`-time tool collision, not just `ValidateServers` failures) leaves running config and persisted set untouched and acks error; stale version rejected.
- [ ] 4.4 Reject a frame that exceeds the WS read limit as an **error ack**, not a dropped connection (client.go:325 `SetReadLimit`); unit test.
- [ ] 4.5 Add header-name/value and env-key legality validation to `mcphost.Validate` (reject CR/LF) with unit tests; invalid pushed config acks error.
- [ ] 4.6 Store received credential values via `internal/secretstore` (fallback: restricted config file) and assert no secret value appears in ack/registration/status; unit test.
- [ ] 4.7 Add the connector opt-in setting/flag that advertises `accepts_managed_config` (default false) and unit-test that pushes are ignored when off.
- [ ] 4.8 Persist the Memory-managed set separately from the user's local `mcp_servers` (so a push cannot silently mutate hand-edited config) with documented merge precedence (server-managed wins on name collision; local-only preserved); unit test.

## 5. Mac app (phase 2, non-blocking)

- [ ] 5.1 The mac app launches the Go connector engine, so pushed-config apply is inherited; add the Swift UI surface (Memory-managed badge, not locally editable) in `MCPServersPage`; Swift unit test for the managed flag.

## 6. Verification

- [ ] 6.1 `openspec validate device-mcp-config` passes.
- [ ] 6.2 Server: `go build ./...` and focused `go test ./domain/mcpregistry/... ./domain/mcprelay/... ./domain/mcp/...` pass.
- [ ] 6.3 Connector: `go build ./...` and focused `go test ./internal/relay/... ./internal/mcphost/... ./cmd/...` pass.
- [ ] 6.4 Lint the touched trees (`task lint` / web-ui + connector lint).
- [ ] 6.5 Manual end-to-end: an opted-in device connects; the assistant creates a device-targeted server; the tool appears as `<instance>_<server>_<tool>` and a call succeeds; kill/restart the device and confirm config re-hydrates on register; confirm a non-owner member cannot target the device.
- [ ] 6.6 Open one PR containing the change directory and the implementation; summarize delta specs in the description.
