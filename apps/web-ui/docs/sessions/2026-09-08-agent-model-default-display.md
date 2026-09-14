# 2026-09-08 — Agent model default display + config warnings

## Goal
User: an agent with no explicit model shows only the opaque "auto — default
model"; they want the UI to reveal **exactly which model is used**. After
clarification: also warn when the project has no provider / no default model,
so it is obvious an agent cannot run or its model is unpinned.

## Outcome
Done (3 commits pushed to master). UI now shows the resolved model on the
agents table + agent dashboard (default tagged "(default)") and warns on the
agent dashboard + agent-settings Model section when nothing resolves.

- `11183b0` feat(gateway): show exact agent model, tag defaults
- `9c573b7` feat(gateway): warn when agent has no resolvable model
- `adc6681` fix(gateway): inline warning action button with alert

Diagnosis of the live case (agent `test`, dev project): memory GET
`/agent-definitions/:id` returned `effectiveModel: null`, the project
model-config is empty, and the project has **zero configured providers
**(`/api/v1/projects/{id}/providers` → `[]`). So there genuinely is no
resolvable model — the warning (error severity) is now the honest UI.

## Decisions
- **Resolution order: explicit override → memory `effectiveModel` → project default generative model** — exactly matches what will run, in the most-authoritative-to-least order the gateway can read.
- **"(default)" tag only when there is no explicit override** — an explicit override is a deliberate choice, not a default; tagging auto-resolved models as default is truthful.
- **Agents-table Model column via one GET per agent (N+1, sequential, best-effort)** — memory's list endpoint returns summaries *without* the model; acceptable on the low-traffic management page. Dropping the N+1 needs a memory list-enrichment change (task).
- **Warning severities: "error" when zero providers configured (chats can't run), "warning" when providers exist but no project default is set (model unpinned)** — matches user's mental model; both link to Settings → Providers.
- **Warning only when the agent is auto AND nothing resolves** — explicit-model agents are excluded (their model is a separate concern).
- **Provider "configured" = `ListProjectProviders` non-empty** — that endpoint returns project credential configs.
- **Button inline on the alert's row, same height; no wrapper card** — go-daisy `ui.Alert` has no children slot and compiled daisy CSS fixes `.btn` height (btn-xs ≈24px) vs `.alert` (grid, ≈44px), so the button needed `ExtraClass: "h-auto"` + a structural `flex items-stretch` row (a `class` attr on `ui.Alert` would be dropped — the component emits `class` twice and the browser keeps the first).

## Changes
- `gateway/memory.go` — `AgentDefinition` now decodes `effectiveModel` (memory GET `/agent-definitions/:id` enrichment; never on the list endpoint).
- `gateway/agent.go` — `agentDashboardData.DefaultModel`/`HasProviders`; `agentSettingsData.DefaultModel`/`HasProviders`; `uiAgent` + `uiAgentSettings` best-effort fetch project model-config + providers; helpers `agentModelName`, `agentModelWarningSeverity`, `modelWarningAlertType`.
- `gateway/ui.templ` — `AgentsPage` gained an `agentDefs map[string]*AgentDefinition` + `defaultModel` param; agents table added a **Model** column (mono name, muted "(default)", or dash).
- `gateway/agent.templ` — dashboard summary Model field renders the resolved model; `agentModelNotice` (dashboard, under summary card) + `agentModelSettingsWarning` (settings Model section, above the picker) share `agentModelWarningBox` (inline action button, equal height, no card).
- `gateway/agent_ui_test.go` — `TestAgentModelDisplay`, `TestAgentModelWarnings`; signature updates for `AgentsPage`.

## Verification
- `templ generate` (gateway) — clean, regenerated `agent_templ.go`/`ui_templ.go` (gitignored).
- `go build ./...` — pass.
- `golangci-lint run ./...` — 0 issues (`task lint` unusable: lefthook not installed, exit 127).
- `go test ./... -count=1` — pass for all touched areas; a full-suite failure `TestUIObjectUpdate` appears only while another session's uncommitted `objects.go`/`objects_test.go` WIP is present (pre-existing, unrelated).
- Live data probe against api.dev memory via `MEMORY_URL`/`MEMORY_TOKEN` confirmed the diagnosis above (no browser verification possible — UI is behind Zitadel session auth).

## Open questions / follow-ups
- **Memory never reports a default for an auto agent on a project with no project default.** `ResolveGenerativeModel` is project-config-only (empty → `ModelSourceNone`); the GET enrichment only sets `effectiveModel` when that resolves non-empty. So even on current memory master the exact model can't be named unless a project default exists — needs a memory-side change → [task](../tasks/agent-effective-model-memory.md).
- Browser verification still owed → [task](../tasks/verify-agent-model-ui-browser.md).
- `docs/spec/00-vision.md` decision-log row (D26) and the `04-go-application.md` Agents-bullet note are **deferred**: both files currently hold another session's uncommitted WIP (D25/Object-editor docs) and were intentionally not edited to avoid entangling commits. Agent-model spec facts went into the clean `docs/spec/03-agent-model.md` instead.
- Parallel sessions share this checkout; unrelated WIP seen in the tree (`gateway/ui.go` hx-boost title sync, `app.css`, `openspec/changes/add-p0-e2e-coverage/`, `avatar_test.go`/`objects.*`/`org_members_ui.*`) is not ours.

## Tasks
- [agent-effective-model-memory](../tasks/agent-effective-model-memory.md) — memory should always surface the agent's resolved generative model (GET, and ideally the list endpoint)
- [verify-agent-model-ui-browser](../tasks/verify-agent-model-ui-browser.md) — signed-in browser pass over the new model column, "(default)" tags, and warnings
