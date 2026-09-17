## 1. Database — ui_config column

- [ ] 1.1 Add Goose migration `00155_add_agent_appearance_ui_config.sql`: `ALTER TABLE kb.agent_definitions ADD COLUMN ui_config jsonb NOT NULL DEFAULT '{}'` (plus a down migration). Verify with `task migrate:up` / `task migrate:status`.
- [ ] 1.2 Update the test schema fixture (`apps/server/internal/testutil/schema.sql`) so `kb.agent_definitions` includes `ui_config`.

## 2. Server — entity + DTOs + persistence

- [ ] 2.1 Add `UIConfig json.RawMessage` (`bun:"ui_config,type:jsonb,default:'{}'" json:"uiConfig,omitempty"`) to `AgentDefinition` in `apps/server/domain/agents/entity.go`. Verify with `go build ./...`.
- [ ] 2.2 Add `uiConfig json.RawMessage json:"uiConfig,omitempty"` to `AgentDefinitionDTO`, `AgentDefinitionSummaryDTO`, `CreateAgentDefinitionDTO`, and `UpdateAgentDefinitionDTO` in `apps/server/domain/agents/dto.go`. Verify with `go build ./...`.
- [ ] 2.3 Map `UIConfig` in `ToDTO` and `ToSummaryDTO`; wire create/update handlers (`apps/server/domain/agents/handler.go` + service) to persist `UIConfig`. Verify with `go build ./...`.
- [ ] 2.4 Unit test (TDD): DTO JSON round-trips `uiConfig`; create/update persist it; unset `uiConfig` yields `{}`. Verify with `go test ./...` in `apps/server`.

## 3. Blueprints — manifest `ui`

- [ ] 3.1 Add `UI map[string]any json:"ui,omitempty"` to `AgentManifest` in `apps/server/domain/blueprints/manifest.go`, mirroring `ObjectTypeDef.UI`. Verify with `go build ./...`.
- [ ] 3.2 Map `AgentManifest.UI` onto agent create/update in apply. Verify with `go build ./...`.
- [ ] 3.3 Unit test (TDD): a blueprint agent declaring `ui: {icon, color}` round-trips and is persisted on apply. Verify with `go test ./...` in `apps/server/domain/blueprints`.

## 4. CLI — `--icon` / `--color`

- [ ] 4.1 Add `--icon` and `--color` flags to `memory agent-definitions create`/`update` in `apps/cli/internal/cmd/agent_definitions.go`, building the `uiConfig` blob into the create/update request. Verify with `go build ./...` in `apps/cli`.

## 5. Web UI — editor + rendering

- [ ] 5.1 Add `lucide--bot` to the supported icon catalog (`apps/web-ui/gateway/type_icons.go`) so it is selectable. Verify with `templ generate` + `go build ./...` in `gateway/`.
- [ ] 5.2 Use `ui.IconPicker`/`ui.ColorPicker` (backed by `supportedIconPickerOptions()` and `schemaColorPresets`) in the agent editor settings form + create/edit modal, with `lucide--bot` as the default/fallback. Verify with `templ generate` + browser smoke test.
- [ ] 5.3 Render agent icon+color via `typeGlyph`/`typeIconTile`/`typeNameChip`/`typeColorStyle` on: agent list cards/rows, dashboard header + summary card, session/chat author bubbles, session row + detail header, schedules rows/detail, ⌘K spotlight, blueprint agent rows, and client-side chat-stream bubbles; fall back to neutral `lucide--bot` when unset. Verify with `templ generate` + browser smoke test.
- [ ] 5.4 UI test: configured agent renders its icon+color; unset agent renders the neutral `lucide--bot` tile. Verify with `go test ./...` in `gateway/`.

## 6. Verify

- [ ] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server`; `go build ./...` in `apps/cli`.
- [ ] 6.2 `templ generate` + `task lint` + `go test ./...` in `gateway/`.
- [ ] 6.3 Smoke test: create an agent with an icon+color, confirm it renders across the dashboard, session list, schedules, and ⌘K, and that clearing it restores the neutral `lucide--bot` tile.
