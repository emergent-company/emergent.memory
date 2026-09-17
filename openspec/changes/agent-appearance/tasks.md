## 1. Database — ui_config column

- [x] 1.1 Add Goose migration `00155_add_ui_config_to_agent_definitions.sql`: `ALTER TABLE kb.agent_definitions ADD COLUMN IF NOT EXISTS ui_config JSONB NOT NULL DEFAULT '{}'` (plus a `-- +goose Down` drop). Verified with `go run ./cmd/migrate -c up` → `successfully migrated database to version: 155`.
- [x] 1.2 Update the test schema fixture (`apps/server/internal/testutil/schema.sql`) so `kb.agent_definitions` includes `ui_config jsonb DEFAULT '{}'::jsonb NOT NULL`.

## 2. Server — entity + DTOs + persistence

- [x] 2.1 Add `UIConfig json.RawMessage` (`bun:"ui_config,type:jsonb,default:'{}'" json:"uiConfig,omitempty"`) to `AgentDefinition` in `apps/server/domain/agents/entity.go`. `default:'{}'` is required so the many insert paths that never set the column do not write SQL `NULL` into a `NOT NULL` column.
- [x] 2.2 Add `UIConfig json.RawMessage json:"uiConfig,omitempty"` to `AgentDefinitionDTO`, `AgentDefinitionSummaryDTO`, `CreateAgentDefinitionDTO`, and `UpdateAgentDefinitionDTO` in `apps/server/domain/agents/dto.go`.
- [x] 2.3 Map `UIConfig` in `ToDTO` and `ToSummaryDTO`; wire create/update handlers (`apps/server/domain/agents/handler.go`) to persist it (update only when provided).
- [x] 2.4 Unit test: DTO JSON round-trips `uiConfig` on the full and summary DTOs, and omits it when nil (`apps/server/domain/agents/dto_test.go`).

## 3. Blueprints — manifest `ui`

- [x] 3.1 Add a typed `UI *AgentUIManifest` (`{Icon, Color}`, `json`+`yaml`, `omitempty`) to `AgentManifest` in `apps/server/domain/blueprints/manifest.go`.
- [x] 3.2 Marshal `AgentManifest.UI` into `UIConfig` on agent create/update in apply (only when at least one value is non-empty; omitted on update preserves an existing appearance).
- [x] 3.3 Unit test: blueprint agent `ui: {icon, color}` reaches `UIConfig` on create and update, and an absent/empty block leaves it untouched (`apps/server/domain/blueprints/apply_test.go`).
- [x] 3.4 Carry the same `ui` block through the gateway's bundled-blueprint types (`BundledAgent`, `blueprintAgent`, `bundledAgentsFromManifest`) and decode it from `agents/*.yaml`, with tests for manifest build, YAML decode, and the manifest→view round-trip.

## 4. CLI — `--icon` / `--color` (DEFERRED, not in this change)

- [ ] 4.1 Add `--icon` and `--color` flags to `memory agent-definitions create`/`update`, plus the blueprint-applier `ui` passthrough. **Deferred:** these read the new SDK `uiConfig` field, and `apps/cli` is built against the published SDK module (`cli.yml` uses `GOWORK: off`; `apps/cli/go.mod` pins `sdk v0.82.0`). SDK module tags are cut only on release tags, so this lands as a follow-up after the next SDK tag bumps `apps/cli/go.mod`. See design.md D6.

## 5. Web UI — editor + rendering

- [x] 5.1 Add `lucide--bot` to the supported icon catalog (`apps/web-ui/gateway/type_icons.go`) so it is selectable and resolvable.
- [x] 5.2 Use `ui.IconPicker`/`ui.ColorPicker` (backed by `supportedIconPickerOptions()` and `schemaColorPresets`) in the agent settings form and the create/edit modal, defaulting to `lucide--bot`.
- [x] 5.3 Render agent icon+color via the shared `typeGlyph`/`typeIconTile`/`typeNameChip`/`typeColorStyle` primitives (through new `agentGlyph`/`agentIconTile`/`agentNameChip` delegates) on: agent list cards/rows, dashboard header + summary card, session/chat author bubbles, session row + detail header, schedules rows/detail, ⌘K spotlight, blueprint agent rows, and client-side chat-stream bubbles; neutral `lucide--bot` when unset.
- [x] 5.4 UI tests updated for the new option attributes and signatures; `agent_ui` helper tests added.
- [x] 5.5 Playwright spec `tests/e2e/specs/agents/agent-icon-picker-ui.spec.ts` (pick icon+colour → save → assert list row + detail tile, plus reset to neutral). Runs in CI.

## 6. Verify

- [x] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server` (agent + blueprint packages green; the one blueprint apply failure is pre-existing on `main` — see PR notes); `gofmt` clean; migration applied.
- [x] 6.2 `templ generate` + `go build ./...` + `go vet ./...` + `go test ./...` + `golangci-lint run ./...` (0 issues) in `apps/web-ui/gateway`.
- [ ] 6.3 Browser smoke test: create an agent with an icon+color, confirm it renders across the dashboard, session list, schedules, and ⌘K, and that clearing it restores the neutral `lucide--bot` tile. Needs a deployed stack with the backend change (local dev gateway talks to the deployed backend), so it is exercised by the PR's e2e job / preview environment rather than locally.
