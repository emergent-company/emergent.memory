## Why

The agent Settings page is a single long form: General, Model, Tools, Skills, Delegation, and the MCP sharing section all render in one scroll, saved by one POST that unconditionally overwrites every field. That monolithic form has two problems:

1. **Too much in one page.** Six unrelated concerns (identity, model, tool allowlist, skills, delegation, MCP credentials) force one giant page and one save button.
2. **One save wipes the others.** `uiAgentUpdate` re-reads the definition and overwrites every field from the form, so a subpage that only holds some fields would blank the rest. Any future split is blocked by this overwrite-all handler.

Split the single page into six subpages — one per concern — each with its own form and POST, and refactor the update path into per-section appliers that mutate only their own fields.

## What Changes

- The agent Settings surface becomes a group of six subpages under the agent rail: **General** (`/agents/:id/settings`, the landing), **Model**, **Tools**, **Skills**, **Delegation**, and **MCP sharing** (`/agents/:id/settings/mcp`).
- Each subpage renders only its own panel plus its own Save button; the MCP sharing subpage is the existing non-form `agentMCPSection` (endpoint + keys + sessions).
- The update handler is replaced by per-section appliers (`applyAgentGeneralSection`, `applyAgentModelSection`, `applyAgentToolsSection`, `applyAgentSkillsSection`, `applyAgentDelegationSection`) that mutate only their own fields on a freshly fetched definition, then `applyDelegation` and persist. `POST /agents/:id/update` remains as a back-compat alias for the General handler.
- PRG redirects target each section (`?updated=1` on success, `?err=<msg>` on failure); MCP flows redirect to `/agents/:id/settings/mcp...#mcp`.
- The dashboard "Share via MCP" button links straight to `/agents/:id/settings/mcp`.

## Capabilities

### Modified Capabilities

- `agent-dashboard-ui`: the agent sections nav gains a Settings group with six subpages; in-page editing becomes per-section forms.
- `agent-mcp-endpoint`, `agent-mcp-keys`, `agent-mcp-sessions`: the MCP sharing surface moves to its own settings subpage at `/agents/:id/settings/mcp`.

## Impact

- Gateway (`gateway/`): `agent.go` (settings section routing + per-section appliers), `agent.templ` (split forms + settings nav group), `agent_mcp_endpoint_handlers.go` (MCP redirect target), `main.go` (routes). No backend/service or DB changes.
- Tests: `agent_ui_test.go`, `agent_mcp_endpoint_handlers_test.go`, `mcp_relay_picker_test.go`, `mcp_shares_handlers_test.go` updated; Playwright e2e `agent-mcp-keys-ui.spec.ts` and `agent-model-warning-ui.spec.ts` point at the new subpage paths.
