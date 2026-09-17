## 1. Server — tool group registry (TDD)

- [ ] 1.1 New package `apps/server/domain/agents/toolgroups/`: `type Group struct { ID, Label, Description string }`, `var Groups []Group` in the frozen contract order (D6), and `func GroupForTool(tool string) string` returning a group id or `"other"`.
- [ ] 1.2 Derive group from `RequiredScope` (export the scope lookup from `mcp` — `toolRequiredScope` is package-private, add an exported accessor) plus a static map for unscoped tools: `workspace_read|workspace_glob|workspace_grep|ast_grep` → `workspace-read`; `workspace_write|workspace_edit|workspace_git|workspace_bash|run_python|run_go` → `workspace-exec`; `google_search|url_context|code_execution|webfetch|brave_search|reddit_search` → `web`.
- [ ] 1.3 Unit tests: every tool name in `sandbox.ValidToolNames`-derived workspace names maps to a workspace group; every scope key present in `toolRequiredScope` maps to a known non-`other` group; unknown names → `other`; `Groups` ids and order match the contract; no duplicate ids.

## 2. Server — group policy resolution

- [ ] 2.1 `agents/entity.go`: add `const toolGroupPolicyPrefix = "@group:"` and extend `effectiveToolPolicy` to the order explicit tool → `@group:<GroupForTool(tool)>` → `default_tool_policy`, preserving the `(ToolPolicy, bool)` contract (`false` for a non-disabled allow).
- [ ] 2.2 Unit tests: explicit tool entry beats group; group beats default; group entry absent falls to default; group `{confirm:true}` and `{disabled:true}` both resolve; a group entry with `{ }` resolves to `(zero, true)` and therefore does not enable.
- [ ] 2.3 Unit test at the executor boundary that a tool covered only by a group `{disabled:true}` is rejected before execution.

## 3. Server — DTO exposes the computed catalog

- [ ] 3.1 `agents/dto.go`: add `ToolGroupDTO{ID, Label, Description, Policy, Enabled, Tools []string}` and a computed `ToolGroups []ToolGroupDTO` on the agent definition read DTO, populated on read (not stored).
- [ ] 3.2 Compute per group: `Tools` = the agent's tools in that group (from `def.Tools`, excluding banned), `Policy` = the group's stored policy mapped to `allow|ask|deny|""`, `Enabled` = at least one member present and not banned.
- [ ] 3.3 Unit tests: a definition with a group policy and members round-trips through the DTO with the expected `policy`/`enabled`/`tools`; a group with no members in `def.Tools` is omitted or reported empty per implementation choice — assert one behaviour explicitly.
- [ ] 3.4 Confirm `UpdateAgentDefinition` persists `@group:` keys unchanged (no key validation rejects the prefix) — add a store-level test if the update path filters keys.

## 4. Gateway — form mapping

- [ ] 4.1 `gateway/memory.go`: add the local `ToolGroup` mirror struct with the frozen JSON tags, plus `ToolGroups []ToolGroup` on the local `AgentDefinition`.
- [ ] 4.2 `gateway/agent.go`: in `applyAgentToolsSection`, write `@group:<id>` entries into `ToolPolicies` from `groupPolicy.<id>` (`inherit` clears the key; `allow`/`ask`/`deny` map to the existing `ToolPolicy` values), and apply `groupEnabled.<id>` by adding members to `def.Tools` / removing from `def.BannedTools` (on) or the inverse (off).
- [ ] 4.3 Keep `delegationToolNames` out of every group: group membership fan-out must never add or ban `spawn_agents` / `list_available_agents`.
- [ ] 4.4 Unit tests (extend `agent_ui_test.go`): group `ask` writes `@group:<id>`; group `inherit` omits the key; disabling a group removes members from `Tools` and adds them to `BannedTools`; enabling the inverse; per-tool override for a member still wins in the rendered form value; delegation tools unaffected.

## 5. Gateway — UI

- [ ] 5.1 `gateway/agent.templ`: render the Tools panel as capability groups from `data.Agent.ToolGroups`, each a collapsible `<details>` with an enable switch (`groupEnabled.<id>`), a tri-state policy select (`groupPolicy.<id>`: Inherit / Allow / Ask / Deny), and its member tool checkboxes.
- [ ] 5.2 Nest the existing MCP-server and relay-node sections as sub-groups inside the capability group that owns their tools, keeping today's labels, agent-facing names, and default-open rule; keep the unlisted "Other" group reachable.
- [ ] 5.3 Show inheritance on each tool row: per-tool override select plus a non-interactive "Inherits <Group> · <Ask>" hint; a tool with an explicit override renders its own value.
- [ ] 5.4 A group header with no members in the catalog is not rendered; groups collapse to keep the panel navigable.
- [ ] 5.5 Preserve `data-testid` conventions for the new controls (group header, enable switch, policy select). Run `templ generate` — never hand-edit `agent_templ.go`.
- [ ] 5.6 Unit test: a rendered settings page shows group headers, the policy select reflects the stored group policy, and a member tool shows its inherited hint.

## 6. Spec + verify

- [ ] 6.1 Update `apps/web-ui/docs/spec/14-assistant-agent.md` to document the group layer, resolution order, and enable/disable semantics.
- [ ] 6.2 Server: `cd apps/server && go build ./... && go test ./domain/agents/... && go test ./domain/mcp/...` (from `gateway/` for the gateway module).
- [ ] 6.3 Gateway: `cd apps/web-ui/gateway && go build ./... && go test ./...`.
- [ ] 6.4 `task lint` for the touched modules; `templ generate` re-run and committed output verified in sync.
- [ ] 6.5 Restart the dev server and exercise the Tools panel in a browser: toggle a group off/on, set a group policy, confirm a per-tool override survives, and confirm a disabled group's tool is refused by a run.
- [ ] 6.6 Open one PR containing the OpenSpec change, server, gateway, and spec edits; push the branch.
