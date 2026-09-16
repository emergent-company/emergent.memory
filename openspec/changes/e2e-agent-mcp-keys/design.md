# Design — e2e-agent-mcp-keys

## Context

The agent-owned MCP endpoint lives on the agent's own configuration surface
(`/agents/:id/settings`, `gateway/agent_mcp_endpoint.templ` +
`agent_mcp_endpoint_handlers.go`). It supersedes the project-share agent picker:
the project shares page (`/settings/mcp-servers/shares`) now exposes tool-scoped
shares only, while each agent owns one endpoint with many labeled keys. The UI
already ships stable `agent-mcp-*` test ids and semantic roles, so this change is
a spec only.

## Decisions

### D1 — Playwright project: `mutations` (serial, self-cleaning)

The spec creates and destroys credentials. It is named `*-ui.spec.ts`, which
`playwright.config.ts` matches to the `mutations` project (`workers: 1`, depends
on `setup` only). That keeps a single-file run cheap (login/bootstrap + this
spec) and, because the spec cleans up after itself, it never races the parallel
`chromium` read surface. No new project and no config change is needed; the
filename convention is the assignment mechanism.

### D2 — One dedicated agent for the whole file

`beforeAll` creates a single `E2E MCP Agent <timestamp>` through the real UI
(`createAgentViaModal`). Reusing one dedicated agent keeps the shared bootstrap
tenant's agents untouched and avoids paying agent creation per test. Tests are
declared inside `test.describe.serial`, so ordering is deterministic and each
test can rely on state the previous one established (e.g. the endpoint id).

### D3 — The one-time secret guarantee is asserted against the live token

The create-key and rotate-key responses render `agent-mcp-key-secret` inside a
modal; the raw token is never persisted by the gateway. The spec captures the
token from that reveal, then leaves the POST response through a plain `GET`
(navigating away) and a `page.reload()` of that `GET`. A `reload()` of the POST
response itself would re-submit the create form, so the spec deliberately
navigates to the settings URL first; that also exercises the realistic "come
back later" path. The token must be absent from the DOM and from the page text,
while the key row itself survives.

### D4 — Sessions coverage does not need a live agent run

The sessions requirement is "the panel renders its empty state when the endpoint
has no sessions". A freshly created dedicated endpoint has none, so the spec
asserts `agent-mcp-sessions` + `agent-mcp-sessions-empty` and the status filter
group. Producing a real session is an external-MCP-client integration and stays
out of scope (a live-run scenario would be non-deterministic and belongs in the
env-gated `scenarios` suite).

### D5 — Locators use the section's contract, not layout

Every control is reached through its role/accessible name (`Create key`,
`Rotate key <label>`, `Revoke this agent's MCP endpoint`, the confirm dialogs'
`dialog[open]` + exact action name) or its `agent-mcp-*` test id. Rows are
filtered by their label text. No CSS-position or `nth-child` selectors. Copy
"actually works" is proven by granting clipboard permissions and reading the
clipboard back, not by asserting the button exists alone.

### D6 — The superseded-feature guard is structural

The regression guard asserts the project shares create form
(`[data-mcp-share-form-fields]`) contains no `select` and no control whose `name`
mentions an agent, and that the shares list's only agent affordance is the link
to `/agents`. That is a structural check of the contract, not a copy assertion.

### D7 — Cleanup is API-based and belt-and-braces

`afterAll` runs against a freshly created authenticated context: it revokes every
key on the endpoint, revokes the endpoint, then deletes the agent, all
best-effort. The endpoint revoke in the last test additionally invalidates any
remaining key, so a failed assertion cannot strand a live credential in the
bootstrap tenant. No secret is ever written to a fixture or committed.

## Risks

- **Shared tenant**: the suite shares the bootstrap `E2E Main` project; running
  two suites concurrently produces unrelated failures. The dedicated agent and
  the `E2E`-prefixed labels make this spec's footprint identifiable and clean.
- **Clipboard in headless Chromium**: gated behind `grantPermissions` and an
  `expect.poll`, so an environment that refuses clipboard access fails loudly on
  the copy step rather than flaking.

## Known environment blocker (verified 2026-09-16)

The spec was executed against the running dev gateway and could not complete its
endpoint/key/session tests. The gateway runs with
`MEMORY_URL=https://api.dev.emergent-company.ai`; that memory backend is
`v0.81.1-23-gaf19e9dab` (built 2026-09-16T08:08Z), which predates the
agent-scoped endpoint server API (`2acc3dd77`,
`feat(mcp): authorize agent endpoint via keys and add endpoint/key CRUD (#502)`,
merged 2026-09-16T08:49Z). An authenticated probe confirms it:

```
POST /api/projects/<pid>/agents/<aid>/mcp-endpoint -> 404 {"code":"not_found","message":"Not Found"}
GET  /api/projects/<pid>/agents                    -> 200 {...}
```

`GET .../mcp-endpoint` also 404s upstream and the gateway renders "no MCP
endpoint yet", so the UI section exists (its render test passes) but the backing
API does not. The spec therefore probes the API once in `beforeAll` and skips the
endpoint-dependent tests with an annotated reason naming the backend version and
the missing route; the section-render and shares-guard tests still run and pass.
Once a backend containing that commit is deployed, the skips become live
assertions with no spec change.
