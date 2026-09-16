## 1. Gateway agent model — delegation + config fields

- [x] 1.1 Add `Config map[string]any` (`json:"config,omitempty"`) to the gateway `AgentDefinition` struct in `gateway/memory.go` so `spawnPolicy.allow` can be persisted to memory.
- [x] 1.2 Add a `Delegation` type (`Enabled bool`, `Targets []string`) with a `delegation` JSON field to the gateway agent request/response model used by the CRUD handlers.
- [x] 1.3 Unit test: `AgentDefinition` and `Delegation` fields round-trip through JSON encode/decode.

## 2. Delegation → memory tool mapping

- [x] 2.1 Implement the mapping: when `delegation.enabled` is true, append `spawn_agents` and `list_available_agents` to the definition `Tools` (deduped) and set `Config["spawnPolicy"].allow = targets`; when disabled, strip those two gateway-managed tools from `Tools` and remove `Config["spawnPolicy"]`.
- [x] 2.2 Add validation: `enabled=true` with an empty `targets` list is rejected (HTTP 400).
- [x] 2.3 Unit test: enabled adds both tools + sets the spawn policy; disabled strips the two delegation tools + `spawnPolicy`; empty-targets-with-enabled is rejected; pre-existing tool entries are not duplicated.

## 3. Agent CRUD wiring

- [x] 3.1 Apply the mapping + validation in the gateway agent create and update handlers.
- [x] 3.2 Unit test (handler level, fake `MemoryBackend`): create/update with delegation persists `Tools` + `Config.spawnPolicy.allow` to memory; without delegation neither is added.

## 4. Verify

- [x] 4.1 `go build ./...` + `go vet` + `go test ./...` in `gateway/`.
- [x] 4.2 Smoke test against live memory: create an agent with `delegation {enabled: true, targets: ["B"]}`, confirm the stored definition lists `spawn_agents`/`list_available_agents` and `config.spawnPolicy.allow=["B"]`; disable delegation and confirm the tools are removed on the next definition read.
