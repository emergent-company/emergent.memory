## 1. View model (gateway/agent.go)

- [x] 1.1 Replace the capability-first nested builder with a source-first `buildToolPickerView` returning Built-in capability groups, external MCP-server blocks, relay blocks, and uncovered rows.
- [x] 1.2 Exclude tools owned by external servers/relays from capability-group direct rows; dedupe every tool so it renders at most once.
- [x] 1.3 Drop a capability group that would render an empty body; keep the uncovered fallback reachable.
- [x] 1.4 Keep group enable/policy semantics (`groupEnabled.<id>`, `groupPolicy.<id>`) and the `@group:` policy storage untouched.

## 2. Templates (gateway/agent.templ)

- [x] 2.1 Add the top-level Built-in disclosure wrapping the capability groups.
- [x] 2.2 Render capability-group member tools as direct rows (remove the nested source sub-groups).
- [x] 2.3 Render external MCP servers and relay nodes as top-level siblings of Built-in (per-tool policy on servers, remote badge + enable-only on relays).
- [x] 2.4 Keep the no-groups fallback source-only grouping unchanged and the uncovered fallback reachable.

## 3. Tests

- [x] 3.1 Update `TestRenderAgentSettingsToolGroups` for the Built-in section and direct rows.
- [x] 3.2 Update `TestRenderAgentSettingsToolGroupsFullMembership` (relay tool renders in its own source block).
- [x] 3.3 Update `TestGoldenToolGroupsFixtureContract` (external-only group dropped, relay top-level).
- [x] 3.4 Add `TestRenderAgentSettingsToolSourceSiblings` for Built-in + external server + relay siblings and the uncovered fallback.

## 4. Verify

- [x] 4.1 `templ generate`
- [x] 4.2 `go build ./...`
- [x] 4.3 `go test ./...`
- [x] 4.4 `task lint`
