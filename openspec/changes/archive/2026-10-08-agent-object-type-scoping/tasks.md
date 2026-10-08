# agent-object-type-scoping

## 1. Spec artifacts

- [x] 1.1 Write `openspec/changes/agent-object-type-scoping/proposal.md`
- [x] 1.2 Write `openspec/changes/agent-object-type-scoping/tasks.md`
- [x] 1.3 Write `openspec/changes/agent-object-type-scoping/.openspec.yaml`
- [x] 1.4 Write delta specs for `agent-object-type-scoping` (new), `agent-definitions` (modified), `web-agent-settings` (added)
- [x] 1.5 Validate with `openspec validate agent-object-type-scoping --strict`

## 2. Per-agent type-scope config (`agents` domain)

- [x] 2.1 Add canonical config keys `objectTypes` / `relationshipTypes` and `AgentDefinition.ObjectTypes()` / `RelationshipTypes()` readers (default empty = all types, nil-safe) in `domain/agents/entity.go`
- [x] 2.2 Expose `objectTypes` / `relationshipTypes` on `AgentDefinitionDTO` (read), `CreateAgentDefinitionDTO`, and `UpdateAgentDefinitionDTO` (write) in `domain/agents/dto.go`
- [x] 2.3 Map the lists into the stored `config` blob on create/update in `domain/agents/handler.go`; remove the key when the list is empty so stored config stays minimal
- [x] 2.4 Populate `objectTypes` / `relationshipTypes` in `AgentDefinition.ToDTO()`

## 3. Scope threading (`agents` executor → `mcp` tool layer)

- [x] 3.1 Add `mcp.TypeScope` value and `NewTypeScope` / `ContextWithTypeScope` / `TypeScopeFromContext` plumbing
- [x] 3.2 Derive the scope once per run from the definition in `runPipeline` and attach it to the tool-layer context
- [x] 3.3 Ensure every execution surface (HTTP trigger, A2A, chat sessions, `/v1/chat/completions`, resume, sub-agents) runs through the same derivation so the scope is identical
- [x] 3.4 Leave HTTP MCP transports with no agent scope unrestricted (unchanged)

## 4. Server-side enforcement (`mcp` tool layer)

- [x] 4.1 Search tools (`search-hybrid`, `search-semantic`, `search-similar`): intersect requested `types` with the object allowlist; only-excluded requests return empty; results never contain an excluded type
- [x] 4.2 Single-type query tools (`entity-query`, `entity-search`): reject a type outside the allowlist with a clear error
- [x] 4.3 `graph-traverse`: reject/intersect excluded relationship types and filter returned relationships and objects
- [x] 4.4 `relationship-list` / `entity-edges-get`: filter returned relationships by the relationship allowlist
- [x] 4.5 Listing/advertising tools (`entity-type-list`, `schema-get`, `schema-compiled-types`): advertise only allowed object/relationship types
- [x] 4.6 Empty allowlist reproduces current behaviour byte-for-byte

## 5. Web UI (`web-agent-settings`)

- [x] 5.1 Add a type-scope control (object types + relationship types) to the agent General settings form (`agent.templ`)
- [x] 5.2 Map the control onto `Config["objectTypes"]` / `Config["relationshipTypes"]` in `applyAgentGeneralSection` (`agent.go`)
- [x] 5.3 Unit-test the reader/mapping

## 6. Tests

- [x] 6.1 Readers: absent key and empty list both yield "all types"; nil-safe
- [x] 6.2 DTO round-trip (read/create/update) for populated and empty allowlists
- [x] 6.3 Search tools: intersection narrows results; only-excluded request returns empty; no excluded type in results
- [x] 6.4 Single-type query tools: excluded type rejected with a clear error; allowed type accepted
- [x] 6.5 Graph tools: excluded relationship types rejected/intersected; returned relationships/objects filtered
- [x] 6.6 Listing tools: excluded types are not advertised
- [x] 6.7 Cross-surface parity: same definition yields the same scope across trigger / A2A / chat / chat-completions / resume / sub-agents
- [x] 6.8 Empty allowlist reproduces pre-change behaviour

## 7. Verification

- [x] 7.1 `go build ./...` + `go vet ./...`
- [x] 7.2 `go test ./domain/agents/... ./.../mcp/... -count=1`
- [x] 7.3 `golangci-lint run --new-from-rev=origin/main` on touched packages → 0 new issues
- [x] 7.4 `openspec validate agent-object-type-scoping --strict`
- [x] 7.5 No migration added (config-blob storage) — migration guard not applicable

## 8. Second review round — additional read paths + delegation

- [x] 8.1 `migration-archive-list` / `migration-archive-get`: type-filter results against the object allowlist and reject/omit excluded types
- [x] 8.2 `journal-list`: drop entries whose affected graph type is outside the object allowlist
- [x] 8.3 Fuzzy key-suggestion errors (`entity-query` / `entity-search` key-not-found path) never name an excluded type
- [x] 8.4 Synchronous/in-process delegation (`trigger_agent` sync, `spawn_agents`, resume): inherit caller's scope via context and intersect with the target definition's own allowlist
- [x] 8.5 Async/queued `trigger_agent`: refuse delegation when the target definition's allowlist would widen beyond the caller's scope
- [x] 8.6 Tests: audit/read tools never surface excluded types; delegation intersection (sync) and refusal (async); no excluded type reachable through any read tool; `search-knowledge` fails closed
- [x] 8.7 `schema-migrate-preview` fails closed under any graph type scope (its plan embeds type names that cannot be coherently scrubbed)
- [x] 8.8 `journal-list`: relationship-created events (empty `object_type`, type in `metadata.rel_type`) and merge/batch metadata are scoped on both axes — relationship type written into `object_type` at creation, metadata recursively scrubbed, nil `object_type` fails closed; falsifiable executor test + mutation check
