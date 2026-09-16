## 0. Recon

- [x] 0.1 Read `tests/e2e/README.md` (projects, auth, helper/test-id conventions) and `playwright.config.ts` — confirmed `mutations` matches `/-ui\.spec\.ts/`, depends on `setup` only, `workers: 1`, and `chromium` ignores `-ui.spec.ts`.
- [x] 0.2 Mapped the UI under test (`gateway/agent_mcp_endpoint.templ`, `agent_mcp_endpoint_handlers.go`, `agent_mcp_endpoint.go`) to the existing `agent-mcp-*` test ids and accessible names; no new test id or gateway change needed.
- [x] 0.3 Traced the routes the spec drives: `POST /agents/:id/mcp-endpoint`, `/mcp-endpoint/revoke`, `/mcp-endpoint/keys`, `/mcp-endpoint/keys/:keyId/rotate|revoke`, and the JSON echo surface used for capture/cleanup (`/api/agents/:id/mcp-endpoint`, `/api/agent-mcp-endpoints/:id/keys`, `/api/agent-mcp-keys/:id`).
- [x] 0.4 Confirmed the behavior contracts in `openspec/specs/agent-mcp-keys/spec.md` and `openspec/specs/agent-mcp-sessions/spec.md`.

## 1. Spec implementation

- [x] 1.1 `specs/agents/agent-mcp-keys-ui.spec.ts` — one dedicated `E2E MCP Agent` created via `createAgentViaModal` in `beforeAll`; whole file in `test.describe.serial`; `afterAll` revokes every key, revokes the endpoint, and deletes the agent.
- [x] 1.2 Section render: `agent-mcp-section` visible and the create-the-endpoint first step before one exists; no live endpoint block.
- [x] 1.3 Endpoint create: PRG to `?mcpCreated=1`, active status, `#agent-mcp-endpoint-url` matches the URL the backend reports, and the copy affordance actually copies it (clipboard permissions + `expect.poll`).
- [x] 1.4 Sessions panel: `agent-mcp-sessions` + `agent-mcp-sessions-empty` + the status filter group — no live agent run required.
- [x] 1.5 Key create: one-time secret in `agent-mcp-key-secret`, row with label + active status, rotate/revoke actions; JSON list carries the key but never the token.
- [x] 1.6 Secret-once guarantee: navigate away (plain GET) then reload; `agent-mcp-key-secret` absent and the raw token appears nowhere in the page text, while the row survives.
- [x] 1.7 Duplicate label: inline `agent-mcp-action-error` with a readable message, no reveal, exactly one row/one JSON key with that label.
- [x] 1.8 Rotate: new secret shown once and different from the prior one, key id unchanged, secret absent after reload.
- [x] 1.9 Revoke one key: revoked row marked revoked and read-only; the other key still active and actionable; endpoint still up.
- [x] 1.10 Revoke endpoint: section back to the create-the-endpoint state, endpoint and keys blocks gone.
- [x] 1.11 Superseded-feature guard: `/settings/mcp-servers/shares/new` form has no `select` and no control named for an agent; the shares list links to `/agents` instead of selecting one.
- [x] 1.12 Environment gate: an authenticated probe of the upstream memory backend (`helpers/objects.memoryAuthHeaders` + `E2E_MEMORY_API_URL`) classifies "route missing" (`404 not_found`) from a domain error; endpoint-dependent tests skip with an annotated reason, the always-runnable render/guard tests still execute.

## 2. OpenSpec change

- [x] 2.1 `.openspec.yaml` (`schema: spec-driven`, `created: 2026-09-16`), `proposal.md`, `design.md`, `tasks.md`.
- [x] 2.2 `specs/e2e-agent-mcp-keys/spec.md` — real `## Purpose` plus `## ADDED Requirements`, each with at least one `#### Scenario:`.
- [x] 2.3 No change to `openspec/specs/**`, gateway templ/handlers, server, SDK, CLI, or Playwright config.

## 3. Verification

- [x] 3.1 `openspec validate e2e-agent-mcp-keys --strict` → `Change 'e2e-agent-mcp-keys' is valid`.
- [x] 3.2 Spec typechecks: `npx --yes --package typescript@5 tsc --noEmit -p tsconfig.json` reports no errors in `agent-mcp-keys-ui.spec.ts` (the only errors are the pre-existing `css-go-daisy-scan.spec.ts` `offsetWidth`/`offsetHeight` ones, unchanged by this work).
- [x] 3.3 `npx playwright test --list` discovers all 10 tests in the `[mutations]` project.
- [x] 3.4 Run attempt: `npx playwright test specs/agents/agent-mcp-keys-ui.spec.ts --project=mutations` → **3 passed, 8 skipped, 0 failed** (setup + the section-render test + the shares guard pass; the endpoint/key/session tests skip via the backend gate).
- [ ] 3.5 Run the endpoint/key/session tests green against a backend that exposes the agent MCP endpoint API. **Blocked — environment:** the gateway runs with `MEMORY_URL=https://api.dev.emergent-company.ai` and that memory build is `v0.81.1-23-gaf19e9dab` (built 2026-09-16T08:08Z). An authenticated probe of `POST /api/projects/:projectId/agents/:agentId/mcp-endpoint` returns `404 {"code":"not_found"}`, while `GET /api/projects/:projectId/agents` returns 200 — the deployed backend predates server commit `2acc3dd77` (`feat(mcp): authorize agent endpoint via keys and add endpoint/key CRUD (#502)`, merged 2026-09-16T08:49Z). GET through the gateway also 404s upstream and is silently rendered as "no MCP endpoint yet", so the UI is present but its backing API is not. Re-run once that commit is deployed.
