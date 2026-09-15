# E2E tests (Playwright)

End-to-end suite that drives the **real dev installation** — dev memory
(`https://api.dev.emergent-company.ai`) + dev Zitadel + the gateway in session
mode — through the full tenant lifecycle:

```
setup project     OIDC login → create org → project → provider (API) → save storage state
chromium project  read/use smoke surface, parallel, reuse the bootstrap tenant
mutations project UI/API mutations (create agent/skill/schedule/token/object, invite, org delete),
                  serial, runs AFTER chromium so mutations never race the read surface
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

## Notes

- The suite reuses the already-running gateway; it does **not** start one.
  `E2E_BASE_URL` overrides the target (default `http://alfred-dev.tail0358fa.ts.net:8095`).
- Legacy mock harness (`mock-memory.mjs`, `run-e2e.sh`) still exists for the
  dev-mode (no-auth) smoke path; this suite is the primary, session-mode path.
- Voice/LiveKit/STT and the iOS client are not browser-testable and are out of scope.
