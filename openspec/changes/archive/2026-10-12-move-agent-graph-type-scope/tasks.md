## 1. Gateway — Graph subpage route + section

- [x] 1.1 `apps/web-ui/gateway/agent.go`: add `sectionGraph = "graph"` and include it in `agentSettingsSections` (ordered after `general`).
- [x] 1.2 `apps/web-ui/gateway/main.go`: register `e.POST("/agents/:id/settings/graph", s.uiAgentUpdateGraph)`.
- [x] 1.3 Unit test: `GET /agents/:id/settings/graph` returns 200 and renders the two pickers; the section is accepted by `uiAgentSettingsSection` (not a 404).

## 2. Gateway — move the applier (TDD)

- [x] 2.1 Add `applyAgentGraphSection(def, c)` holding the type-scope loop (moved verbatim), mapping `objectType` / `relationshipType` → `Config["objectTypes"]` / `Config["relationshipTypes"]`, deleting the key on a present-but-blank field and skipping an absent field.
- [x] 2.2 Remove the type-scope loop from `applyAgentGeneralSection`; update its doc comment.
- [x] 2.3 Add `uiAgentUpdateGraph` calling `applyAgentSettingsSection(c, sectionGraph, applyAgentGraphSection)`.
- [x] 2.4 Unit tests: retarget `TestApplyAgentGraphSectionTypeScope` (order, trimmed blanks, present-but-empty removes the key, field-absent preserves the stored value, unrelated config untouched); assert `applyAgentGeneralSection` no longer touches the scope keys.

## 3. Gateway — loader

- [x] 3.1 `loadAgentSettings`: move the `GetCompiledTypes` fetch from the `sectionGeneral` branch to a new `sectionGraph` branch; update the doc comment.
- [x] 3.2 Unit tests: extend `TestLoadAgentSettingsScopesCatalogFetches` — `graph` loads `GetCompiledTypes` exactly once, `general` loads none.

## 4. Gateway — template

- [x] 4.1 `apps/web-ui/gateway/agent.templ`: add the Graph `SubNavItem` (`lucide--git-branch`) after General and a `case "graph"` in `agentSettingsSectionPanel`.
- [x] 4.2 Add `agentGraphSettingsForm`: a `Graph` `ui.Section` with the two `form.FormControl` pickers (each with its hidden companion input), a helper line, POSTing to `/agents/:id/settings/graph` with the shared `agentSettingsSave()` footer.
- [x] 4.3 Remove the two pickers from `agentGeneralSettingsForm`.
- [x] 4.4 Unit tests: retarget `TestAgentGraphSettingsFormTypeScope`; assert the General page no longer renders `name="objectType"` / `name="relationshipType"` while the Graph page does; add the Graph href to the nav assertions.

## 5. Verify

- [x] 5.1 `cd apps/web-ui && task generate` (templ regenerated; never hand-edit `*_templ.go`).
- [x] 5.2 `cd apps/web-ui && go build ./...`.
- [x] 5.3 `cd apps/web-ui && task lint`.
- [x] 5.4 `openspec validate move-agent-graph-type-scope --strict`.
