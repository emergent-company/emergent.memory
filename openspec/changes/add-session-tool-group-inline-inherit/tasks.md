## 1. Server — Session tool group

- [x] 1.1 `toolgroups.go`: add `GroupSession = "session"`, insert the ordered `Group{Label:"Session"}` entry, and map `session-todo-list`, `session-todo-update`, `set_session_title` in `staticToolGroup`.
- [x] 1.2 `dto.go`: add `sessionBuiltinToolNames`, a `defaultOn` set, the catalog-gated Session membership injection, and thread `defaultOn` through `groupEnabled`.
- [x] 1.3 Tests: `toolgroups_test.go` (group present, order, static mappings) and `tool_group_dto_test.go` (session membership, banned ⇒ not enabled, default-on ⇒ enabled, nil catalog ⇒ absent). Regenerate the golden fixture.
- [x] 1.4 Verify: `cd apps/server && go build ./... && go test ./domain/agents/...`.

## 2. Gateway — inline inherit label + ban-managed row

- [x] 2.1 `agent.go`: add `inheritValueLabel`; delete `toolPolicyHint`/`toolPolicyHintShort` and the `Hint`/`HintShort` fields.
- [x] 2.2 `agent.templ`: `agentPolicyOptions(value, inheritValue, inheritLabel)` renders `Inherit (<value>)`; remove the two hint spans; widen the selects to fit.
- [x] 2.3 `agent.go`: add `banManagedTools`, `NoPolicy`, ban-aware `Checked`, and the `BannedTools` reconciliation in `applyAgentToolsSection`.
- [x] 2.4 Tests: replace hint assertions with the new Inherit labels; add session ban-managed row render + write-path tests.
- [x] 2.5 Verify: `cd apps/web-ui/gateway && templ generate && go build ./... && go test ./...`.

## 3. Spec, docs, verify

- [x] 3.1 Update `apps/web-ui/docs/spec/14-assistant-agent.md` (Session group + inlined inheritance label).
- [ ] 3.2 `task lint` for the touched modules.
- [ ] 3.3 Restart the dev server and exercise agent → Settings → Tools in a browser: the Session group renders with `set_session_title` checked and no policy select; unchecking and saving bans it; the Inherit options show the inherited value and no hint label remains.
- [ ] 3.4 `openspec validate add-session-tool-group-inline-inherit`.
- [ ] 3.5 Open one PR with the change, server, gateway, and docs edits; merge after review.
