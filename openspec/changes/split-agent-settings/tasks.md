## 1. Gateway — split settings into six subpages

- [x] 1.1 Add a `Section` field to `agentSettingsData` and section constants + `agentSettingsSectionPath` helper in `agent.go`.
- [x] 1.2 Refactor `uiAgentSettings` into a shared `renderAgentSettingsSection` (landing = General) plus `uiAgentSettingsSection` (subpages, unknown → 404). Load MCP data only on the MCP subpage.
- [x] 1.3 Replace `uiAgentUpdate` with per-section appliers + `applyAgentSettingsSection`; register `POST /agents/:id/settings/{general,model,tools,skills,delegation}` and repoint `POST /agents/:id/update` at the General handler.

## 2. Gateway — templates

- [x] 2.1 Split `agentSettingsForm` into `agentGeneralSettingsForm` / `agentModelSettingsForm` / `agentToolsSettingsForm` / `agentSkillsSettingsForm` / `agentDelegationSettingsForm`, sharing `agentSettingsSave`; `AgentSettingsPage` renders the panel for `data.Section` (MCP renders `agentMCPSection`).
- [x] 2.2 Replace `agentSubNav` with a Settings group (six indented children, active child highlighted); update Dashboard/Sandbox/Sessions call sites.
- [x] 2.3 Point the dashboard "Share via MCP" button at `/agents/:id/settings/mcp`.

## 3. MCP surface path

- [x] 3.1 `agentMCPSettingsPath` → `/agents/:id/settings/mcp`; `renderAgentSettingsWithMCP` sets `Section` to "mcp".

## 4. Tests

- [x] 4.1 Update `agent_ui_test.go` (per-section render + route + update tests, unknown-section 404, tools/skills non-wipe regression).
- [x] 4.2 Update `agent_mcp_endpoint_handlers_test.go` (MCP subpage GET + PRG redirect targets), `mcp_relay_picker_test.go` (Tools section), `mcp_shares_handlers_test.go` (Share-via-MCP link).
- [x] 4.3 Update Playwright e2e `agent-mcp-keys-ui.spec.ts` + `agent-model-warning-ui.spec.ts` to the new subpage paths.

## 5. Verify

- [x] 5.1 `templ generate` + `go build ./...` + `go test ./...` + `task lint` in `gateway/` all pass.
