# Tasks — add-external-mcp-management

Assumption recorded from design Open Questions: relay-sourced tools render
allow-only in the agent tool picker in v1 (checkbox on/off via the flat tool
whitelist, no per-tool policy select), unless implementation finds the backend
already applies tool policies to `<instance>_<tool>` names — in which case the
existing policy select is reused for relay groups unchanged.

## 1. Backend client + interface

- [x] 1.1 Extend `MemoryBackend` interface in `gateway/backend.go` with `ListRelaySessions(ctx) ([]RelaySession, error)` and `GetRelaySessionTools(ctx, instanceID string) ([]RelayTool, error)`; update the fake in `gateway/handlers_test.go` so the gateway compiles. Verify: `go build ./...` from `gateway/`.
- [x] 1.2 Define `RelaySession{InstanceID, Version string, ToolCount int, ConnectedAt time.Time}` and `RelayTool{Name string}` DTOs in the gateway (extras.go or new relay.go). Verify: compiles.
- [x] 1.3 Implement `MemoryClient.ListRelaySessions` calling `GET /api/mcp-relay/sessions` with the standard bearer token + project header, decoding plain JSON (no successEnvelope). Verify: unit test with httptest backend asserting decoded fields + project header present.
- [x] 1.4 Implement `MemoryClient.GetRelaySessionTools` calling `GET /api/mcp-relay/sessions/:id/tools`, tolerantly extracting tool names from nested `{"tools":[...]}` and flat-array payloads. Verify: unit tests covering nested, flat, and empty payloads + error propagation.
- [x] 1.5 Ensure relay API errors surface as gateway-friendly errors (mirror existing mcp-server 502 mapping). Verify: unit test asserting error text on backend 500.

## 2. Page handler + route

- [x] 2.1 Add `uiMCPNodes` handler (gateway) rendering node list; `?node=<instance_id>` fetches that node's tools. Empty sessions → empty state; relay API failure → friendly error state, rest of shell usable. Verify: handler unit tests with fake backend for list/empty/error/select-node-not-found.
- [x] 2.2 Register `GET /settings/mcp-nodes` in `gateway/main.go` with page auth consistent with other settings pages. Verify: route responds 200 in handler test harness.

## 3. External MCP nodes page (templ)

- [x] 3.1 Add settings sub-nav entry "MCP nodes" linking `/settings/mcp-nodes` in the settings rail (distinct from sibling change's "MCP Servers" entry). Verify: render unit test asserts link present.
- [x] 3.2 Build `ExternalMCPNodesPage` templ: node rows (instance id, version, tool count, connected-at relative), tools panel for selected node listing `<instance>_<tool>` names + description where available, "no nodes connected" empty state, error state. Verify: `templ generate` succeeds and render tests cover nodes/empty/error states.
- [x] 3.3 Show instance-id-as-unique-key hint copy on the page (tool names prefix by instance). Verify: render test asserts hint text present.

## 4. Agent tool picker shows relay node tools

- [x] 4.1 Extend agent Settings loader (`agent.go`) to fetch relay sessions + per-node tools alongside `ListMCPServers`; expose as relay groups on `agentSettingsData`; failure degrades to "no relay groups" without breaking the rest of the page. Verify: loader unit test with fake backend for nodes-present, nodes-empty, and relay-API-error.
- [x] 4.2 Extend `agentToolPicker` (agent.templ) to render one collapsible group per connected relay node, labelled with instance id + remote badge, checkbox `name="tool" value="<instance>_<tool>"` checked when the agent whitelist contains it; relay groups visually distinct from registry server groups. Verify: render unit tests — relay tools shown/checked, no relay nodes → no relay group, existing registry + Other groups unchanged.
- [x] 4.3 Confirm unlisted-tool preservation path still covers relay tools when a node disconnects (existing Other group). Verify: unit test — agent with `<instance>_<tool>` whitelisted while no node connected renders it in the Other group checked.

## 5. Verification gate

- [x] 5.1 `go build ./...` from `gateway/` passes.
- [x] 5.2 `templ generate` passes (templates changed).
- [x] 5.3 Linter runs clean: `task lint` (or equivalent from `gateway/`).
- [x] 5.4 Full gateway test suite passes: `go test ./...` from `gateway/`.
