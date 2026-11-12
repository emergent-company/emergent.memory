# E2E tests (Playwright)

End-to-end suite that drives a **real dev installation** — the memory API,
Zitadel, and the gateway in session mode — through the full tenant lifecycle:

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
chat project      live-LLM chat-UI scenarios (agent switch + session rail, run-control
                  inline-card/queue/rail) on a scratch project, sequential; skips fast when
                  E2E_SCENARIO_LLM_API_KEY is unset
connector project memory-connector CLI PKCE sign-in with the test user; gateway-independent
                  (needs E2E_CONNECTOR_SERVER or E2E_MEMORY_API_URL; no gateway/setup)
globalTeardown    delete the bootstrap org (idempotent, runs on failure)
```

## Prerequisites

1. The gateway is running in **session mode** on a reachable URL
   (`AUTH_MODE=session`, Zitadel configured). Start it with `task dev`; set
   `E2E_BASE_URL` if it is not `http://localhost:8095`.
2. A Zitadel **test user** with no MFA exists on the deployment's issuer
   (e.g. `https://zitadel.example.com`). The issuer shows a **passkey panel**
   after the login name even for a user with no registered passkey; the login
   helper takes its "Use password" alternative, so both states work — but a user
   that *requires* a passkey (password login disabled) cannot be driven
   headlessly.

The gateway-backed projects read `E2E_MEMORY_API_URL` at the start of the
`setup` project they depend on, and fail loudly there when it is unset. The
gateway-free `connector` project does not depend on `setup`, so it runs without
it (see [Connector auth](#connector-auth)).

## Setup

```bash
cd tests/e2e
npm install
npx playwright install chromium   # once (--with-deps on a bare host)

# credentials + deployment targets (gitignored)
cp .env.e2e.example .env.e2e
# edit .env.e2e → set E2E_TEST_USER_EMAIL / E2E_TEST_USER_PASSWORD and
#                 E2E_MEMORY_API_URL (the memory API origin)
#                 — the last one is not needed for --project=connector,
#                   which reads E2E_CONNECTOR_SERVER instead (see below)
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
npx playwright test              # full suite (setup + chromium + mutations + scenarios + chat)
npx playwright test --project=chromium   # reuse a previously saved session
npx playwright test --project=chat       # live-LLM chat-UI scenarios only
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
of the suite — the spec skips fast when either is unset) and an API origin:
`E2E_CONNECTOR_SERVER`, or `E2E_MEMORY_API_URL` as the fallback (the spec skips
fast when neither is set). `E2E_MEMORY_API_URL` itself is **not** required for
this project: gateway-backed projects get it enforced by the `setup` project
they depend on, and `connector` does not depend on `setup` — so
`--project=connector` runs on a machine that does not target a deployment. The
connector binary is built once via `go build ./cmd/memory-connector`; no gateway
is needed.

```bash
cd tests/e2e
npx playwright test --project=connector

# env overrides (no baked-in default):
#   E2E_CONNECTOR_SERVER=https://api.example.com   # required (or E2E_MEMORY_API_URL)
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
- The UI is served at the gateway origin your `E2E_BASE_URL` points at; the unit
  file above assumes a local reverse proxy on `127.0.0.1:8123`.

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
  severity, no-alert on explicit model), agent model switch (two agents toggled
  default ↔ specific models, with the model string verified on the agents list,
  dashboard, and settings Model picker after every switch), agent-create, skill-create,
schedule-create, token-create, document-upload, document-extraction
(provider-gated), member-invite, provider-config, object-create, object-edit (versioned save →
`?updated=1` toast → value persists on reload), object-long-text (boosted navigation into a
person's long `notes` field grows the textarea past one row and initialises its live character
counter, which updates on edit), backup-details (list checksum
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
far, four in `api_tokens.templ`: the token row, the revoked badge, the composed
scope-area badge and the one-time secret reveal dialog).

Objects and schema: **object relationships** (edge created from the Connect dialog is
visible from both objects), **object search** (`/objects/search` filters to the created
object, and the `#connect-dst` datalist surfaces it), **object merge** (asserts the
route's deterministic contract only — see below), **blueprint install + unapply**
(installed types appear and are removed again, on a scratch project; the
registry-backed install applies a blueprint by id from its Available row, and
built-ins carry a Built-in badge with no remove action), **blueprint registry
remove** (a non-built-in blueprint that is uninstalled stays in the Available
list, is removed via Remove → confirm, and disappears; an applied one is blocked
with the "uninstall first" flash) and **blueprint
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
real key is needed), agent with an explicit model, `personal-memory` blueprint
imported from the public GitHub repo `mkucharz/personal-memory-blueprint` (pinned
to the `v3.1.0` tag), blueprint types in the fresh project, Person object, then a
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

A2UI surface cards: `specs/chat/a2ui-widgets-ui.spec.ts` renders the A2UI
(v0.9.1) nine-card catalog end-to-end in the browser without an LLM. It
intercepts `POST /api/chat` and fulfils it with a canned SSE `ui` event carrying
all nine catalog components plus an unknown one, then asserts each card's DOM
(`proposal`/`approval`/`question`/`code`/`entity`/`object-form`/`todo`/`result`/
`sources`), the unknown-component summary fallback, and that a card action emits
the `a2ui:action` CustomEvent carrying the `surfaceId` and the
`{componentId, response}` action — for both the proposal/approval action row
(`a2uiActions`) and the question-option path (`a2uiQuestion`). The approval card's
decision set (Cancel / Reject / Approve, by `chat-approval-*` testid) has a single
owner, `helpers/approval-controls.ts`; both this spec and
`specs/js/a2ui-approvals-gate.spec.ts` assert from it, and the gate is the one that
runs the shipped `chat-components.js`, so a renderer change breaks there first. That
gate runs on the `dev -> main` promotion PR and on pushes to `main` — `web-ui.yml`
has no `pull_request` trigger — while the scheduled `mutations` lane fires on `dev`
daily, so this is the pre-production backstop, not an earlier detector of a
schedule-lane red (#584). It also asserts a
UI-only turn leaves no empty assistant text bubble, and that the `result`/`proposal`/
`approval` structured props (`rows`, `body`, `input`) render as key/value rows rather
than a single raw-JSON `<pre>`. Deterministic and ungated — no provider, no LLM, no
env var. The gateway's verbatim `ui` passthrough is covered by the Go unit test
`gateway/sse_markdown_test.go`.

Chat (agent switch): `chat-agent-switch.spec.ts` proves the chat area's
navigation on a fresh scratch project — start a conversation with agent A, switch
to agent B via "New chat", then resume A's conversation from the session rail. It
asserts A's row is active after the first turn, that both A and B coexist in the
rail after the second, and that resuming A switches the `#chat-agent` picker back
to A and loads A's (not B's) transcript. Env-gated on `E2E_SCENARIO_LLM_API_KEY`.

Chat (run control): `chat-run-control.spec.ts` proves the three run-control
signals on the /chat surface against a real model turn: a pending `ask_user`
question card renders INLINE in the transcript with decision controls; a queued
follow-up (Cmd/Ctrl+Enter) parks during a streaming turn and auto-releases in
order when the turn ends; and the session-rail badge reflects the active run's
bucket change without a navigation. Env-gated on `E2E_SCENARIO_LLM_API_KEY`.

Chat (multi-turn context): `chat-multi-turn-context.spec.ts` plants a codeword
in turn 1 of a fresh conversation and asks for it in turn 2, asserting the reply
recalls it — proving the prior turn's history reaches the model on a later turn
of the same conversation (the one thing the navigation/run-control specs do not
assert), plus that both turns persist in `GET /api/conversations/:id/history`.
Env-gated on `E2E_SCENARIO_LLM_API_KEY`; the recall assertion HARD-FAILS (a
temperature-0 recall of a token just supplied is the contract under test).

Chat (question resume): `chat-question-resume.spec.ts` drives a pending
`ask_user` question rendered INLINE in the transcript
(`#chat-messages [data-testid="chat-question-card"]`) end-to-end: select an
option, submit, then wait for the parked run's resumed continuation to render
with the pinned natural-language marker `Great, I will go with <value>` and to
persist in history. (The marker is plain prose on purpose — an earlier machine
token rendered verbatim into the scratch conversation and read as leaked
metadata, #1492.) The resumed continuation is not streamed — memory
resumes the run in the background and the gateway reads it back from history
(`chat.js answerQuestion` → `refreshActiveTranscript`) — so the assertion is
rendered transcript text. The option+submit clicks run in one synchronous
in-page poll because the transcript is re-rendered over the history refresh
while parked. The inline card's own render is covered by
`chat-run-control.spec.ts`. Env-gated on `E2E_SCENARIO_LLM_API_KEY`; skips when
the model never asks or the run errors (deviation/environment) and HARD-FAILS
when an answered card never resumed the run.

Shared live-chat scaffolding (provider save, `createScratchAgent`, `openChat`,
history read, cleanup) lives in `helpers/live-chat.ts`; all four chat scenarios
use it.

Scenario (approvals respond): `approvals-respond.spec.ts` drives a live
human-in-the-loop tool approval end-to-end — scratch project → live provider →
MCP server + synced tool → agent with `defaultToolPolicy: ask` → imperative chat
turn → poll a sibling `/settings/approvals` tab (reload) until the pending
Approve control appears → approve → assert the pending decision resolves. The
chat SSE stream is bound to the request context, so the approvals page is opened
in a sibling tab to keep the stream (and the run) alive while polling. Env-gated
on `E2E_SCENARIO_LLM_API_KEY`; skips on env/provider rejection and when the model
does not call the gated tool, hard-fails on page/selector regressions. Reject and
Cancel share the same respond/cancel routes and remain uncovered (follow-up).

Document extraction: `specs/documents/document-extraction-ui.spec.ts` uploads a
document through the form and triggers extraction from the document detail page,
asserting the PRG round-trip (`?uploaded=1` → `?extracted=1`) un-gated. A second
test is env-gated on `E2E_SCENARIO_LLM_API_KEY`: it configures a provider through
the settings UI and asserts the `Extraction results` section actually lands. The
trigger only enqueues a job (memory does not validate a provider at creation), so
the round-trip needs no LLM; producing results does, which is why the results
test skips fast without the key. A third test — the blocked state, disabled
button + "Extraction needs a generative model." — creates a fresh provider-less
project **and navigates with that project's scope**, because the model config is
per project and the bootstrap `E2E Main` project always has a provider: without
the explicit `{orgId, projectId}` the scoped path still points at the bootstrap
project, so the card renders unblocked (#584).

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

The remaining gaps above are listed per feature area in the coverage sections of
this document; the blocked ones say why they stay uncovered.

## CI — hermetic JS wiring gate

The session-mode suite above **does not run in CI**: it needs a gateway running
in session mode (`AUTH_MODE=session` + Zitadel), a Zitadel test user in
`.env.e2e`, and the real dev memory API. None of that is reproducible on a
GitHub runner without shipping credentials, so historically `web-ui.yml` ran
only `go build / vet / test / lint` — and every JavaScript-side regression was
unguarded (e.g. #1216: `a2uiButton` referenced an undefined `surfaceId`, so
every A2UI card-button click threw and never dispatched; #1201).

The CI gate is therefore a **hermetic DOM-level Playwright run**, added as the
`js` step of the single `web-ui` job in `.github/workflows/web-ui.yml` and
folded into the advisory `web-ui-ci` aggregate:

```bash
cd tests/e2e
npm ci
npx playwright install chromium   # once
npx playwright test --config=js-dom.config.ts   # or: task e2e:js
```

- `js-dom.config.ts` targets only `specs/js/`; it starts **no** server.
- `specs/js/a2ui-wiring.spec.ts` loads the shipped
  `gateway/webui/static/js/chat-components.js` verbatim into a bare page
  (`page.addScriptTag`) and drives `renderA2UISurface` — the exact path the
  browser takes for an A2UI `ui` SSE event. It clicks the proposal/approval
  action rows (`a2uiActions`) and a question option (`a2uiQuestion`) and
  asserts each emits `a2ui:action` carrying `{surfaceId, action:{componentId,
  response}}`, and that no `pageerror` (e.g. a `ReferenceError` for an undefined
  identifier) is raised. It also asserts an unknown component degrades to a
  summary card without throwing, and that the `result`/`proposal`/`approval`
  structured props render as key/value rows (not raw JSON).
- `specs/js/a2ui-surface-lifecycle.spec.ts` covers two A2UI renderer
  regressions. First, a history replay with two card answers that both name
  their surface `s1` (the id the prompt teaches) used to keep only the newest
  card, because the surface registry was keyed by `surfaceId` across the whole
  transcript; the spec replays two such turns and asserts both cards render.
  Second, a card that streamed in was silent to assistive tech: the spec drives
  the real live frame sequence (createSurface alone, then components, then a
  later frame) and asserts exactly one hidden `role=status`
  `aria-live=polite` announcement (`New card 1: Result card.`) that does not
  change on later progressive frames, that a second surface of the same kind
  announces distinct text, that each card is a `role=group` with its kind as
  `aria-label`, and that history replay announces nothing.
- `specs/js/chat-session-lifecycle.spec.ts` drives the session-rail lifecycle
  wiring (#1318): the delete confirmation is shown with the session title and
  sends no request until confirmed, dismissing it keeps the session, confirming
  issues the `DELETE` and reconciles the rail, the `includeArchived` filter
  round-trips through a server refresh, and archive → unarchive round-trips a
  row through its persisted state. Hermetic (no gateway/credentials), so it runs
  in this CI job with the rest of `specs/js/`.
- `specs/js/toast-persistent.spec.ts` loads the shipped `app.js` against a fake
  Alpine toast queue and asserts the `feedback.toast` severity rule: `error`
  toasts are added with `duration 0` (persistent) on both `MemoryApp.toast` and
  the delegated `memory-toast` event, while `success`/`info`/`warning` use the
  shared auto-dismiss timeout.
- The same CI job runs `node --check` over every
  `gateway/webui/static/js/*.js` first — a fast syntax gate.
- The gateway-free specs are excluded from the live `chromium` project
  (`playwright.config.ts` `testIgnore`) so a local full-suite run does not
  schedule them behind `setup`.

**What the gate covers:** the wiring of the shipped static JS in a real browser
engine — A2UI card rendering + action-event dispatch + no uncaught runtime
errors on the exercised paths, plus JS syntax across the static bundle.

**What it does NOT cover:** the gateway↔browser round trip (SSE passthrough,
htmx swaps), session/auth flows, and the rest of the session-mode suite
(navigation, mutations, live-LLM scenarios). Those still require the running
dev installation and stay manual / not-in-CI. Making the full suite hermetic
would need a stub gateway + memory fixtures that cover every surface. The
`mock-memory.mjs` harness serves the public agent-share surface **and** the
data-source-integrations + document-version endpoints (see the data-sources
note below); the remaining surfaces are still tracked as follow-up.

### Ports for the hermetic suites (read before adding one)

The hermetic suites (`share`, `datasources`, `gateway-smoke`, `mcp-inspect`,
`dev-gallery`) **must not contain a port constant**. The `memory` pool runs
concurrent jobs on shared hosts, so two runs of the *same* job can reach the
same suite step at once; a pinned gateway/mock pair means the second job binds
the first job's ports and dies in `run-e2e.sh`'s pre-flight (#810).

Adding a suite or a job — the rule, in four steps:

1. **Never pin a port.** No `?? <number>` default, no `MEMORY_PORT=…` in a
   workflow, no number in `run-e2e.sh`.
2. **Select from the single source.** Copy the `execFileSync(... '--lane',
   '<your-lane>')` block from an existing config. `ports.mjs` is the only place
   that chooses ports; it writes the pair to `E2E_PORTS_FILE` (under
   `$RUNNER_TEMP` in CI, the platform temp dir locally) and prints it. The lane
   name should be unique per config because it names that file.
3. **Hand the same file to the runner.** Pass `E2E_PORTS_FILE: portsFile` in the
   suite's `webServer.env`. The config builds its `baseURL` from the selection
   and `run-e2e.sh` boots from the file — the runner has no numeric default, and
   a missing file (or an env override that disagrees with it) is fatal, not a
   reason to re-derive a second pair the config is not looking at.
4. **Rely on the provenance check, not on arithmetic.** The probe skips a port
   that is already listening, but it does **not reserve** it: a foreign process
   can take the port between the probe (before the build) and the bind (after
   it). `run-e2e.sh` therefore accepts `/api/health` only when `ss -ltnp` shows
   the gateway *it started* owns the port, and re-checks both PIDs after the
   curl. A lost race fails loudly naming the port; it never tests a stranger.

What this does and does not guarantee: the start offset is random per
invocation and the bands are wide, so a collision is unlikely, but it is
**probabilistic** — two different jobs are not hard-disjoint by construction,
and the probe does not make the selection atomic. The loud provenance check is
the guarantee that a collision cannot silently pass. `reuseExistingServer:
false` is not that guard: Playwright checks the URL before launch and then
trusts it, so it cannot tell our gateway from one that took the port later.

`scripts/provenance-race-test.sh` is the hermetic regression for that window
(CI runs it as the `port provenance` step): it slows the build with a `go` shim,
binds the selected gateway port with a foreign health responder, and asserts the
runner exits non-zero naming the port.

`scripts/ci-contract-check.mjs` (CI: the `Contract: e2e suite gates` step) holds
the rules above down, because a rule that only lives in prose is one revert away
from coming back (#205): it fails if a hermetic config names a port, gives two
configs the same lane, or stops selecting through `ports.mjs`; it also binds a
reservation and re-runs the selection from the same `E2E_PORT_SEED`, asserting it
skips the ports the reservation holds — so the probe cannot rot silently. The
same script asserts the `Report` step of `web-ui.yml` reads the outcome of every
`continue-on-error` step and does not accept `skipped` as a pass — a suite that
never ran must never read as a suite that passed.

### Hermetic agent public-share gate

The agent public-share suite — `specs/share/*` under `share.config.ts` — is
credential-free and deterministic: it drives the anonymous `/share/agent`
surface against the dev-mode mock harness (`mock-memory.mjs`), with no OIDC
session and no live memory backend. It runs in CI as the `share` step of the
single `web-ui` job in `web-ui.yml` (#825), folded into the advisory `web-ui-ci`
aggregate. It boots `bash run-e2e.sh` exactly like `datasources.config.ts`, and
that job provisions the Go + templ + `task` prerequisites once for every suite;
the suite is excluded from the live session-mode suite via `testIgnore`
(`specs/share/**`).

The suites in that job run one at a time, so each boots and tears down its own
stack. Ports are **derived per run** (see **Ports for the hermetic suites**
above): the config selects a free gateway/mock pair, writes it to
`E2E_PORTS_FILE`, and `run-e2e.sh` reads the same file. No default number is
shared between suites or jobs, and the runner refuses to attach: it accepts
`/api/health` only once it has proven the gateway it started owns the port, so
a lost race names the port and fails instead of running against a sibling's
gateway + stateful mock.

```bash
cd tests/e2e
npx playwright test --config=share.config.ts
```

### Hermetic Data Sources gate

The Data Sources (Google Drive) suite — `specs/datasources/*` under
`datasources.config.ts` — is likewise credential-free and deterministic: it
drives the gateway against the dev-mode mock harness (`mock-memory.mjs`), with
no live Drive and no OIDC session. It runs in CI as the `datasources` step of the
single `web-ui` job in the same `web-ui.yml` workflow (which also runs the
gateway build/vet/test/lint steps and the `js` suite), and is folded into the
advisory `web-ui-ci` aggregate.

Unlike the `js` gate it does need the gateway binary:
`datasources.config.ts` boots `bash run-e2e.sh` as its Playwright `webServer`,
which builds the gateway (generating the gitignored templ + `app.css` assets on
a cold tree — see the cold-tree note under **Notes**) and starts
`mock-memory.mjs` + the gateway with `AUTH_MODE=dev` on a gateway/mock pair
derived at run time (see **Ports for the hermetic suites** above).

```bash
cd tests/e2e
npm ci
npx playwright install chromium   # once
npx playwright test --config=datasources.config.ts   # or: task e2e:datasources
```

The CI job provisions the generated-asset prerequisites once for every suite
(Go toolchain + `templ` + `task`) plus Node/Playwright, and the suite stays
excluded from the live session-mode `playwright.config.ts` via `testIgnore`
(`specs/datasources/**`), so a local full-suite run never schedules it behind
`setup`.

### Hermetic gateway browser smoke gate

`specs/gateway-smoke/*` under `gateway-smoke.config.ts` is the browser gate
(#564): the highest-risk UI contracts run without credentials on the
`dev -> main` promotion/hotfix PR and on pushes to `main` (`web-ui.yml` has no
`pull_request` trigger, so a feature PR to `dev` does not run it; the scheduled
`mutations` lane is what covers `dev`).
It boots the same `run-e2e.sh` mock harness (`AUTH_MODE=dev`) on a
gateway/mock pair derived at run time (see **Ports for the hermetic suites**
above). Each invocation selects its own pair; safety comes from the probe plus
the runner's provenance check, not from arithmetic over suite names or an
assumption that only one lane is live on a host.

It covers the documents upload dialog (the #2287 regression class — opener,
modal dialog, file input and submit must stay together), the mobile shell
(zoomable viewport, no horizontal overflow at 390px, keyboard-operable sidebar
trigger), the chat session rail overlay, and the Providers settings save
(`specs/gateway-smoke/provider-add-scope.spec.ts`: adding a provider from a
scoped page must land back on `/o/{org}/p/{project}/settings/providers` — a
flat PRG target is the #2479 class that 404s in session mode after a successful
save). It also pins the documents toolbar's control shape — one search field,
with an applied `?q=` also mirrored as a hidden input in the sibling filter form
— so a `input[name="q"]` selector is ambiguous by design and specs must scope it
(`helpers/documents.ts` `DOCUMENTS_SEARCH_INPUT`), the #584 failure class. The
same `?q=`-only hidden mirror exists on `/objects`
(`objects_browser.templ` `objectsFilterHiddenFilters`), so the trap is not
documents-only: scope any `name="q"` selector to `input[type="search"]`. The
session-mode `*-ui` mutations suite stays schedule-only because it needs
Zitadel; this lane is what makes dialog/markup drift fail before merge.

```bash
cd tests/e2e
npm ci
npx playwright install chromium   # once
npx playwright test --config=gateway-smoke.config.ts   # or: task e2e:smoke
```

### Live Google Drive gate (manual, never CI)

`specs/gdrive-live/gdrive-datasource-live.spec.ts` (the `gdrive-live` project)
proves the **real** Drive path end-to-end against a live deployment — the
hermetic `datasources` suite above mocks the upstream, while this one exercises
it: a source seeded from the stored refresh token → the resource step on a large
account (the bounded listing reports truncation, name search narrows to the
scratch folder) → folder save with the edit form prefill → first sync creates document
v1 → the fixture edits the file → re-sync produces v2 sharing the same
`canonicalId` with a distinct `sourceRevisionId` (v1 still readable) → the
fixture trashes the file → re-sync marks the head `deleted_from_source`.

It is opt-in and **never runs in CI** (no workflow fires it). There is **no
human step and no consent screen** on the default path: a Drive source stores
its `client_id`/`client_secret`/`refresh_token` in the source `config` and the
scheduled sync refreshes from that token, so the spec seeds the source through
the public API with the fixture token and then drives the real UI. The seeding
call is made from Node (`fetch`), not through Playwright's request context, so
the stored credentials never enter a trace or attachment.

The OAuth **consent** handoff is still covered — the authorize URL, the
project-scoped client id, the `drive.readonly` scope with offline access, and
the callback-created source — as a **separate opt-in test** behind
`E2E_GDRIVE_LIVE_CONSENT=1`. That one does need a human to finish Google's
consent UI, and it is skipped unless the flag is set.

**Prereq — provision the fixture refresh token once** (no dependencies):

```bash
cd tests/e2e
node scripts/gdrive-fixture-token.mjs --client <clientId> --secret <clientSecret>
```

This prints a Google consent URL; open it and sign in with the fixture account.
It listens on `http://127.0.0.1:8787` for the redirect, exchanges the code, and
prints the refresh token exactly once. The OAuth client must allow that redirect
URI (a "Desktop app" client does by default). The refresh token is granted the
full `drive` scope so the fixture can create/edit/trash files. The seeded source
stores only `client_id`/`client_secret`/`refresh_token` (no `scope`), so it
refreshes with whatever that token already grants — full `drive` here — and sees
exactly what the fixture account sees: the account must own (or be given) the
folder the spec works in. The restricted `drive.readonly` strategy is what the
**opt-in consent test** configures, not the seeded path.

**Run** (against the live session-mode gateway + dev memory backend, same
`E2E_TEST_USER_*` + `E2E_MEMORY_API_URL` as the other live suites):

```bash
cd tests/e2e
npx playwright test --project=gdrive-live
```

The `gdrive-live` project depends on the shared `setup` project (org/project
bootstrap), so a full run of the command above also runs `setup`. If `setup`
itself is unhealthy on the target deployment, run the gate with a pre-existing
signed-in state: `npx playwright test --project=gdrive-live --no-deps`
(`setup` on dev was crashing in a login/scope bug at the time of writing — see
product issue #1998).

Env (see `.env.e2e.example`):

| Var | Meaning |
|---|---|
| `E2E_GDRIVE_LIVE=1` | gate — the spec skips unless set |
| `E2E_GDRIVE_CLIENT_ID` / `E2E_GDRIVE_CLIENT_SECRET` | Google OAuth app (used by the project OAuth client the spec configures, and by the fixture to refresh its token) |
| `E2E_GDRIVE_FIXTURE_REFRESH_TOKEN` | fixture refresh token from the script above |
| `E2E_GDRIVE_FOLDER_ID` | optional — reuse a pre-made folder; otherwise a scratch folder is created and deleted |
| `E2E_GDRIVE_LIVE_CONSENT=1` | optional — also run the opt-in consent test (needs a human to finish Google's consent UI) |
| `E2E_MEMORY_API_URL` | memory API origin — set it to the deployment under test (the same host that serves `/api/health`; your local `.env.e2e` already carries the value used by the other API specs). The spec reads documents/versions/content directly from it, while the UI and API actions go through the gateway |

#### Consent check (opt-in)

`E2E_GDRIVE_LIVE_CONSENT=1 npx playwright test --project=gdrive-live` also runs
the consent handoff, which is the only part that needs a human:

The test prints (and attaches) the Google authorize URL, then polls for up to
the OAuth state TTL (~10 min) for the callback-created source — open the printed
URL and finish consent in that window. Complete consent promptly after the URL
is printed: the OAuth state expires in ~10 minutes and the test still asserts
three syncs afterwards, so a slow consent can push the whole run past its
timeout.

The connect is started from the real UI connect surface
(`/data-sources/new?providerType=google_drive` → the panel's Connect control,
which drives `/data-sources/oauth/start`) with no config pinned, so the
callback-created source lands with no sync target — exercising the shipped
round-trip fix. The test intercepts that route's 303 redirect to print + attach
the Google authorize URL (it never navigates the test browser to Google). After
the human finishes consent and the source appears, the test walks the detail
page's "choose a sync target" prompt onto the resource-selection step, selects
the scratch folder, and asserts the edit form prefills the folder before the
three syncs run. The OAuth *client* under test is the project-scoped one
configured through the settings UI.

### Live-LLM scenarios (scheduled, not PR-gated)

The `scenarios` + `chat` projects (live-LLM, env-gated on
`E2E_SCENARIO_LLM_API_KEY`) are **not** part of any PR CI gate: they need the
running dev installation plus a real provider key, so they run on a separate
scheduled + manually-dispatchable workflow. That workflow lives in the
infrastructure repository, not here — it targets a deployed environment, so its
configuration (stack URLs, test-user credentials) is infrastructure, and keeping
it out of this repository is what lets the product tree stay free of deployment
details. It has no `pull_request` trigger, so it can never block a PR; it fires
on a daily cron and on manual `workflow_dispatch`.

### Mutations (scheduled, not PR-gated)

The `mutations` project — every `*-ui.spec.ts` (org/project/provider/agent/
skill/schedule/token/object/board/document/backup UI mutations) — is likewise
**not** part of any PR CI gate. It needs the running session-mode gateway plus a
Zitadel test user (the same `E2E_TEST_USER_*` credentials as the live-LLM suite),
so it runs on a scheduled + manually dispatchable workflow that lives in the
infrastructure repository alongside the live-LLM lane, with no `pull_request`
trigger. It shares a concurrency group with the live-LLM workflow because both
reuse the stable `E2E Main` tenant. (Before #1857 this project had no CI lane at
all — the `*-ui.spec.ts` files were authored but never executed.)

## Notes

- The suite does **not** start a gateway. Set `E2E_BASE_URL` to the target
  (default `http://localhost:8095`; the two session-mode CI workflows point it at
  the deployed dev stack because the self-hosted runners are not the gateway host).
- `E2E_MEMORY_API_URL` (required) sets the memory API origin the token-lifecycle
  specs probe directly; those specs use it to prove a regenerated/revoked secret
  is rejected with HTTP 401. The live config fails fast when it is unset, and the
  session workflows set it to the deployed dev stack.
- **Fresh Git worktree:** `apps/web-ui/gateway/*_templ.go` and
  `apps/web-ui/gateway/webui/static/css/app.css` are gitignored, so a new
  worktree has no generated templates or stylesheet and the gateway will **not**
  compile until you run `PATH="/root/go/bin:$PATH" templ generate` (or
  `-f <file>.templ` for one file) and `PATH="/root/go/bin:$PATH" task css`,
  both **from `apps/web-ui/gateway`** — templ records its invocation directory in
  the generated files, so a tree generated from the repo root is rejected by the
  `webui-templ` pre-commit hook (see #651).
  Run them before `go build ./...` in any worktree-based lane. The mock-harness
  configs (`datasources.config.ts`, `gateway-smoke.config.ts`, `dev-gallery.config.ts`, `share.config.ts`)
  generate both on the fly via `run-e2e.sh`, so they boot on a cold tree.
- **One suite at a time per tenant.** All runs share the same bootstrap `E2E Main`
  tenant and the same running gateway. Two suites running concurrently (e.g. another
  session, or a worktree lane) produce scattered, unrelated failures across documents/
  objects/backups/agents. Those specs pass in isolation and on a clean re-run — confirm
  no other run is active before treating a batch of failures as real.
- Legacy mock harness (`mock-memory.mjs`, `run-e2e.sh`) still exists for the
  dev-mode (no-auth) smoke path; this suite is the primary, session-mode path.
- **Dev-gated component gallery** (`specs/dev/component-gallery.spec.ts`): the
  gallery route only serves with `MEMORY_COMPONENT_GALLERY=on`, which the live
  session-mode gateway leaves off. The spec is excluded from the `chromium`
  project and runs under its own flag-on harness config instead —
  `npx playwright test --config=dev-gallery.config.ts` (or `task e2e:gallery`),
  which starts `run-e2e.sh` exactly like `share.config.ts`.
- **Data Sources (Google Drive)** (`specs/datasources/`): connect, sync, and
  document-version flows run hermetically against the dev-mode mock harness —
  `npx playwright test --config=datasources.config.ts` (or
  `task e2e:datasources`). The mock keys sync outcomes off the source's
  `folder_id` (`folder-slow` → in-progress + cancel, `folder-fail` → failed,
  otherwise completed) so no real Drive credentials are needed. Excluded from the
  live `chromium` project via `testIgnore`. `run-e2e.sh` generates templ on an
  unwarmed tree before building the gateway.
- Voice/LiveKit/STT and the iOS client are not browser-testable and are out of scope.
