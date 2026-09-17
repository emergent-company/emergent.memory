# E2E tests (Playwright)

End-to-end suite that drives the **real dev installation** — dev memory
(`https://api.dev.emergent-company.ai`) + dev Zitadel + the gateway in session
mode — through the full tenant lifecycle:

```
setup project     OIDC login → create org → project → provider (API) → save storage state
chromium project  read/use smoke surface, parallel, reuse the bootstrap tenant
mutations project UI/API mutations (create agent/skill/schedule/token/object, invite, org delete,
                  project restore, token lifecycle), serial (workers: 1). Depends on `setup`
                  ONLY — not on chromium — so running a single mutation spec runs just
                  login/bootstrap + that spec instead of the whole read surface. Mutation specs
                  self-clean, so they never race the parallel read surface.
scenarios project live-LLM full journey (provider → agent → blueprint → object → chat) on a
                  scratch project, sequential; skips fast when E2E_SCENARIO_LLM_API_KEY is unset
connector project memory-connector CLI PKCE sign-in with the test user; gateway-independent
globalTeardown    delete the bootstrap org (idempotent, runs on failure)
```

## Prerequisites

1. The gateway is running in **session mode** on the tailnet URL
   (`AUTH_MODE=session`, Zitadel configured). Start it with `task dev`.
2. A Zitadel **test user** with no MFA exists in `zitadel.dev.emergent-company.ai`.

## Setup

```bash
cd tests/e2e
npm install
npx playwright install chromium   # once (--with-deps on a bare host)

# credentials (gitignored)
cp .env.e2e.example .env.e2e
# edit .env.e2e → set E2E_TEST_USER_EMAIL / E2E_TEST_USER_PASSWORD
```

Live-LLM scenario credentials (optional — the scenario spec skips without a
key, so the default run needs none of these; defaults target the dev litellm
proxy, provider `openai`):

```bash
# edit .env.e2e → set E2E_SCENARIO_LLM_PROVIDER=openai
#                   E2E_SCENARIO_LLM_API_KEY=<real provider key>
#                   E2E_SCENARIO_LLM_BASE_URL=http://litellm:4000/v1
#                   E2E_SCENARIO_LLM_MODEL=openai/deepseek-v4-flash
#                   E2E_SCENARIO_LLM_EMBEDDING_MODEL=openai/gemini-embedding-001
```

## Run

```bash
cd tests/e2e
npx playwright test              # full suite (setup + chromium + mutations + scenarios)
npx playwright test --project=chromium   # reuse a previously saved session
npx playwright report            # HTML report
```

Each run reuses the stable `E2E Main` org + project (created once); teardown
leaves it in place unless `E2E_RESET=1` is set (deletes it, next run recreates).

## Connector auth

`specs/connector/connector-auth.spec.ts` signs the **same dev Zitadel test
user** in through the `memory-connector` CLI, without the gateway. Dev Zitadel
disables the device flow, so the spec drives the connector's two-step
Authorization Code + PKCE flow instead:

```
memory-connector auth start   → authorize_url + login_id + state
Playwright goto(authorize_url) → Zitadel login form (test user)
capture custom-scheme callback (com.emergent.memory.connector://callback)
memory-connector auth complete → auth status + projects list
```

Chromium cannot follow the non-HTTP custom scheme, so the callback URL is read
from the aborted navigation (`requestfailed`) — which carries the full
`code`/`state` query — or the redirect response `Location` header. The spec
asserts `auth status --json` reports `signed_in: true` with the test-user email,
and that `projects list --json` returns a valid array. All CLI calls pass a
temp `--config`, so the real connector store is never touched.

Required env: `E2E_TEST_USER_EMAIL` / `E2E_TEST_USER_PASSWORD` (same as the rest
of the suite — the spec skips fast when either is unset). The connector binary is
built once via `go build ./cmd/memory-connector`; no gateway is needed.

```bash
cd tests/e2e
npx playwright test --project=connector

# env overrides (defaults target dev):
#   E2E_CONNECTOR_SERVER=https://api.dev.emergent-company.ai
#   E2E_CONNECTOR_CLIENT_ID=390138928478289930
#   E2E_CONNECTOR_REDIRECT=com.emergent.memory.connector://callback
#   MEMORY_CONNECTOR_BIN=/path/to/memory-connector   # skip the build
```

## Interactive UI runner (auto-managed)

The Playwright UI is supervised by systemd + `air` so it stays alive and
hot-reloads when spec/config files change:

```bash
systemctl status playwright-ui   # active?
sudo systemctl start playwright-ui
journalctl -u playwright-ui -f   # follow (shows air reload logs)
```

- air watches `tests/e2e/**` and restarts the UI server on change (`.air.toml`).
- systemd `Restart=always` keeps air itself alive across crashes/session teardown.
- Unit file: `tests/e2e/playwright-ui.service` (install: `cp` to `/etc/systemd/system/`, then `systemctl daemon-reload && systemctl enable --now playwright-ui`).
- The UI is served at `https://alfred-dev.tail0358fa.ts.net:8444` (tailscale serve → `127.0.0.1:8123`).

## Test-ID convention

The gateway renders a stable `data-testid` anchor on the main content root of
every page, derived from the page title (`gateway/ui.go: pageTestID`):

```
<main id="main-content" data-testid="page-<title-slug>">
  /agents            → page-agents
  /settings/providers → page-providers
  /orgs/new          → page-new-organization
```

Detail pages that title themselves with an entity name (agent/skill/document)
produce `page-<entity-name>`; those specs assert on the dynamic name, which also
catches failed loads (an error state renders a static label without the name).

Use `page.getByTestId('page-…')` to assert a page rendered; prefer semantic
locators (`getByRole`, form `name=` attrs) for controls. Add per-control
`data-testid` anchors (via go-daisy `Attrs`) only where a semantic locator is
unstable.

## Coverage

Smoke (read/use): agents (+detail/settings/sandbox/sessions), chat, documents,
objects (+new, detail), schema, blueprints (+migrations), skills (+new/detail), schedules
(+new), sessions, backups, usage, settings (project/assistant/overrides/voice/
devices/approvals/tokens/providers), orgs, org-context (landing/members/
settings hub), members (+invite), profile (+invitations/tokens), spotlight
(palette open/close, mobile full-screen sheet, filter), CSS regression guard
(go-daisy-derived icons render, hidden `#spotlight-toggle` stays hidden).

Mutations: org-create, org-delete (danger zone), project-create,
project-transfer, project row-click activation, org-settings-hub render + legacy
`tool-settings` 301, projects table (bulk + row-menu delete), account menu
(avatar trigger, name + email identity), agent model warning (error/warning
severity, no-alert on explicit model), agent-create, skill-create,
schedule-create, token-create, document-upload, document-extraction
(provider-gated), member-invite, provider-config, object-create, object-edit (versioned save →
`?updated=1` toast → value persists on reload), backup-details (list checksum
cleanup + row→`/backups/:id` link, details page sections/settings/integrity, and
the unavailable state for a missing backup).

Secret lifecycle and destructive additions: **project token lifecycle**
(`/settings/tokens` — create → edit scopes → regenerate → revoke, plus a guard test
asserting no live `E2E` tokens are left behind, and a direct memory API probe
asserting the previous secret is rejected with HTTP 401 — not merely that its row is
marked revoked), **account token lifecycle**
(`/profile/tokens` — the same lifecycle, 401 probes and guard), **project restore**
(row-menu schedule-for-deletion → restore → back in the active list and no longer
pending), and **invite revoke** (create a pending invite for a unique address →
revoke → no longer pending in the DOM or in `/api/invites`). All of these assert
resulting state rather than transient toasts, and assert only on the entities they
created — never on absolute list contents, because the mutation project shares the
bootstrap tenant with every other run.

Further additions: **backup lifecycle** (create via the form → wait for `ready` →
download affordance + the route's 302 → delete → gone), **document delete** (upload →
detail → delete → gone, chunk view reports the document as unavailable),
**project-settings inline autosave** (project info and editor agent persist across a
full reload, with the original values restored afterwards) and **project overrides**
(create → delete, each asserted across reloads, plus a no-leftovers guard).

Agent-scoped MCP endpoints: `specs/agents/agent-mcp-keys-ui.spec.ts` drives the
"MCP endpoint" section on an agent's own Settings page — create the endpoint and
verify its URL (readable and actually copied), the sessions panel's empty state,
a labeled key's one-time secret reveal, the secret never reappearing after a
navigate-away + reload, a duplicate label's readable 409 error, rotation (new
secret once, same key identity), revoking one key while another stays active,
and revoking the endpoint back to its not-enabled state, plus a guard that the
project shares page (`/settings/mcp-servers/shares`) offers no agent picker. It
uses one dedicated `E2E MCP Agent` and revokes every key plus the endpoint in
`afterAll`. The spec probes the upstream memory backend once and skips the
endpoint-dependent tests with an annotated reason when that backend does not yet
expose `POST /api/projects/:projectId/agents/:agentId/mcp-endpoint` (the UI ships
ahead of the memory deploy); the section-render and shares-guard tests always run.

Note on `data-testid`: none of the four flows above needed one — semantic locators
sufficed. Anchors are added only where an element has no stable accessible name (so
far: three in `api_tokens.templ`, for the token row, the revoked badge and the
one-time secret reveal panel).

Objects and schema: **object relationships** (edge created from the Connect dialog is
visible from both objects), **object search** (`/objects/search` filters to the created
object, and the `#connect-dst` datalist surfaces it), **object merge** (asserts the
route's deterministic contract only — see below), **blueprint enable + unapply**
(installed types appear and are removed again, on a scratch project) and **blueprint
migration + rollback** (forced migration drops an archived property; the rollback
assertion is skipped pending the defect below).

Three limitations worth knowing before extending these:

- **Object merge has no deterministic endpoint.** `GET /objects/:id/merge?with=` only
  303-redirects to `/chat` with an agent and a prompt; the fusion is LLM work. Outcome
  assertions belong in the env-gated `scenarios` suite.
- **The gateway has no object-delete route**, so object specs clean up through the
  memory API using the signed-in session token (`memory_session` cookie) + `X-Project-ID`.
  Those deletes are soft (`POST /api/graph/objects/:id/restore`): cleanup removes the
  objects and their edges from live listings, but the soft-deleted rows remain
  restorable in the archive.
- **`POST /blueprints/migrate/rollback` restores zero objects** — a server defect:
  `Repository.List` does not select `migration_archive`, so `RollbackSchemaMigration`
  skips every object. The spec drives the route and asserts its response, with the
  restoration assertion behind `test.skip(true, …)` that reverts to a hard assertion
  once fixed.

Scenarios: one full journey on a fresh scratch project —
provider add via the settings UI (live-validated by the memory backend, so a
real key is needed), agent with an explicit model, bundled `personal-memory`
blueprint install, blueprint types in the fresh project, Person object, then a
real chat turn about the object (asserted reply text) plus session-rail
persistence. Sequential (`workers: 1`), isolated from the parallel chromium and
serial mutations projects, and env-gated: without `E2E_SCENARIO_LLM_API_KEY`
the whole spec skips fast so the default run stays green.

Scenario (OpenAI-compatible): `provider-openai-litellm-config.spec.ts` drives
the "openai" provider type (Base URL field + API-key show/hide toggle) from a
fresh scratch project against a LiteLLM proxy, then asserts the configured
provider row carries the LiteLLM base URL. Env-gated on `E2E_OPENAI_API_KEY`
(default base URL `http://litellm:4000/v1`); skips fast when the key is unset
or the memory backend rejects the live-validated save.

Scenario (agent proposal card): `agent-proposal-card.spec.ts` is parametrized over
the writable first-class proposal kinds (`blueprint`, `skill`, `agent`,
`mcp_server`, `provider`) — each case drives a chat turn where an agent calls
`ask_user` with a structured `proposal` of that kind and asserts the gateway
renders the reviewable proposal card — "Proposed changes" header + kind badge,
the kind's structured preview, and the Accept/Reject answer controls. The
structured preview is proved two ways: the model-independent
`[data-proposal-kind="<kind>"]` section hook the card carries, and a body-field
value unique to that kind. `object` is excluded — the operator cannot emit
`object` proposals until the graph-write scope grant lands, so its renderer is
unit-tested only. The ask_user tool pauses the run, so completion is detected on
the `.proposal-card` in the DOM rather than a closed SSE stream. Env-gated on
`E2E_SCENARIO_LLM_API_KEY`; the spec skips fast on a run error, when the model
never asked (no `question` SSE event), on a markdown-fallback degradation (a
`question` event with an empty/absent `proposalHtml`, i.e. the proposal body was
empty or malformed), on a different kind, or on a different pinned body value —
those are non-deterministic model deviations, annotated and skipped. It
HARD-FAILS on the gateway regressions: a `question` event carrying a non-empty
`proposalHtml` with no `.proposal-card` rendered, or a card carrying the
requested kind's badge without that kind's `[data-proposal-kind]` section (a
registered kind degraded to a summary-only card). A fetch interceptor tees the
`/api/chat` SSE stream and records the `question` events, so a gateway render
regression can no longer masquerade as a skip.

Scenario (chat agent switch): `chat-agent-switch.spec.ts` proves the chat area's
navigation on a fresh scratch project — start a conversation with agent A, switch
to agent B via "New chat", then resume A's conversation from the session rail. It
asserts A's row is active after the first turn, that both A and B coexist in the
rail after the second, and that resuming A switches the `#chat-agent` picker back
to A and loads A's (not B's) transcript. Env-gated on `E2E_SCENARIO_LLM_API_KEY`.

Document extraction: `specs/documents/document-extraction-ui.spec.ts` uploads a
document through the form and triggers extraction from the document detail page,
asserting the PRG round-trip (`?uploaded=1` → `?extracted=1`) un-gated. A second
test is env-gated on `E2E_SCENARIO_LLM_API_KEY`: it configures a provider through
the settings UI and asserts the `Extraction results` section actually lands. The
trigger only enqueues a job (memory does not validate a provider at creation), so
the round-trip needs no LLM; producing results does, which is why the results
test skips fast without the key.

Out of scope (need seeded schema or a live LLM): schema object-type detail,
session detail page (the scenario covers one live turn but not session-detail
rendering), blueprint detail (needs an applied pack), run detail, sandbox runs,
provider live test, chat streaming/SSE (the scenario sends
one real turn but does not assert streamed tokens). Agent-model-warning's
warning-severity case skips when the dev-memory provider catalog is unsynced
(provider upsert rejected); the scenario skips too when its provider save is
rejected or `E2E_SCENARIO_LLM_API_KEY` is unset.

Out of scope (need a second identity): member role change, member removal, member
detail, and invite accept/decline all require a second member in the org. The suite
has one Zitadel test user, so these stay uncovered rather than being weakened into
page-load assertions. Seeding a second member during `setup` is the intended fix.

The remaining gaps above are tracked as an OpenSpec change:
`openspec/changes/web-ui-e2e-coverage/` (phases per feature area, with per-task
status and the blocked items recorded in `tasks.md`).

## Notes

- The suite reuses the already-running gateway; it does **not** start one.
  `E2E_BASE_URL` overrides the target (default `http://alfred-dev.tail0358fa.ts.net:8095`).
- `E2E_MEMORY_API_URL` overrides the memory API origin the token-lifecycle specs
  probe directly (default `https://api.dev.emergent-company.ai`); those specs use it
  to prove a regenerated/revoked secret is rejected with HTTP 401.
- **Fresh Git worktree:** `apps/web-ui/gateway/*_templ.go` are gitignored, so a new
  worktree has no generated templates and the gateway will **not** compile until you
  run `PATH="/root/go/bin:$PATH" templ generate` (or `-f <file>.templ` for one file).
  Run it before `go build ./...` in any worktree-based lane.
- **One suite at a time per tenant.** All runs share the same bootstrap `E2E Main`
  tenant and the same running gateway. Two suites running concurrently (e.g. another
  session, or a worktree lane) produce scattered, unrelated failures across documents/
  objects/backups/agents. Those specs pass in isolation and on a clean re-run — confirm
  no other run is active before treating a batch of failures as real.
- Legacy mock harness (`mock-memory.mjs`, `run-e2e.sh`) still exists for the
  dev-mode (no-auth) smoke path; this suite is the primary, session-mode path.
- Voice/LiveKit/STT and the iOS client are not browser-testable and are out of scope.
