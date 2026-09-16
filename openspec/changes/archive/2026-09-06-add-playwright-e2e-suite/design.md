## Context

The gateway is already wired for session mode on this host and runs via `air` on `:8095`:

```
MEMORY_URL            = https://api.dev.emergent-company.ai      (dev memory backend)
AUTH_MODE             = session
ZITADEL_ISSUER        = https://zitadel.dev.emergent-company.ai
ZITADEL_CLIENT_ID     = 389347199156289546   (public client — PKCE, no client secret)
ZITADEL_REDIRECT_URI  = http://alfred-dev.tail0358fa.ts.net:8095/auth/callback
PUBLIC_BASE_URL       = http://alfred-dev.tail0358fa.ts.net:8095
```

Key facts that shape the design:

- **Tenancy is session-mode only.** Org/project/provider are proxied to the memory backend, scoped by `sessionContext{Token, ProjectID, OrgID}` → outbound `Authorization` + `X-Project-ID` / `X-Org-ID` headers. In dev mode these headers fall back to the static `MEMORY_PROJECT_ID` and no org header — so the org/project/provider lifecycle is only meaningfully testable in session mode (which is already on).
- **The OIDC redirect URI is host-pinned.** `ZITADEL_REDIRECT_URI` points at the tailnet URL, not `localhost`. The session cookie is set on the callback origin, so the suite must run against `http://alfred-dev.tail0358fa.ts.net:8095`, not `localhost:8095`, or the callback bounces to a different origin and the session is lost.
- **Reuse the running gateway.** A second instance on another port has no matching Zitadel redirect URI. `reuseExistingServer: true` against the tailnet URL is the simplest correct approach.
- **Token exchange uses PKCE only** (`exchangeCode` sends `client_id` + `code_verifier`, no `client_secret`). No secret to manage in test config.

## Goals / Non-Goals

**Goals:**

- A Playwright suite that drives the full dev installation end-to-end via real Zitadel OIDC.
- Lifecycle: login → create org → create project → configure provider → run the surface → delete org (teardown).
- Phase 1: API-driven bootstrap + full read/use surface, green.
- Phase 2: UI-driven specs for the org/project/provider creation forms.

**Non-Goals:**

- No gateway/product code changes (pure test infrastructure; `skip_specs: true`).
- No test-only auth shortcut (real OIDC used).
- No voice/LiveKit/STT coverage (native, not browser-reachable).

## Decisions

### Base URL and server reuse

- `baseURL = http://alfred-dev.tail0358fa.ts.net:8095` (matches `PUBLIC_BASE_URL` and the Zitadel redirect URI).
- `webServer` omitted; instead require the gateway to already be running (`reuseExistingServer` semantics). A guard in `auth.setup.ts` fails fast with a clear message if `/api/health` or `/auth/login` is unreachable.

### Project structure (Playwright projects)

```
project "setup"      testMatch: auth.setup.ts
  → OIDC login → save storageState (.auth/state.json)
  → create org + project + configure provider (API)
  → write {orgId, projectId} to test-results/.bootstrap.json
project "chromium"   dependencies: ['setup'], storageState: .auth/state.json, fullyParallel
  → read/use specs reuse one session + active org/project
globalTeardown       → POST /orgs/:id/delete (idempotent, best-effort)
```

### Bootstrap: API-driven (Phase 1) then UI-driven (Phase 2)

- Phase 1 bootstraps org/project/provider via `page.request` using the authenticated session (bearer token read from the session context), for reliability. The active project/org is established by calling `POST /api/projects/:id/activate` and `POST /orgs/:id/activate` so subsequent page loads are correctly scoped.
- Phase 2 adds dedicated specs that drive the real forms (`/orgs/new`, project create, `/settings/providers/new` + `:provider/edit`) to test the UI itself; bootstrap remains API-driven.

### Credentials

- `tests/e2e/.env.e2e` (gitignored) with `E2E_TEST_USER_EMAIL` / `E2E_TEST_USER_PASSWORD`.
- Loaded by `playwright.config.ts` via `dotenv` (mirrors the cloned repo's layered loading).

### Directory layout

```
tests/e2e/
  playwright.config.ts
  package.json / tsconfig.json
  .env.e2e                 (gitignored)
  constants/  storage.ts, routes.ts
  helpers/    auth.ts (login), bootstrap.ts (org/project/provider), teardown.ts
  fixtures/   (optional extended test fixture)
  specs/      agents.spec.ts, chat.spec.ts, documents.spec.ts, objects.spec.ts,
              settings-providers.spec.ts, orgs.spec.ts, members.spec.ts,
              profile.spec.ts, auth.spec.ts, … + phase-2 wizard specs
  .auth/state.json         (gitignored)
  test-results/            (gitignored)
```

## Risks / Spikes

1. **Zitadel login selectors** — the real `zitadel.dev.emergent-company.ai` login form (email → password → continue) differs from the cloned repo's old Zitadel. Spike: drive the login once, capture the selectors (`input[name=loginName]`, `input[type=password]`, submit button), and encode them in `helpers/auth.ts`.
2. **Tailnet resolution from the Playwright browser** — confirm the browser resolves `alfred-dev.tail0358fa.ts.net` to this host and completes the OIDC callback on `:8095`. If MagicDNS fails inside the browser context, fall back to a `hosts`-style mapping or run the browser with `--host-resolver-rules`.
3. **Parallel mutation isolation** — specs that mutate shared state (org switch, org create, provider config change, token revoke) must not race with read-only specs. Rule: state-mutating assertions live in Phase 2 serial specs or their own serial project; the `chromium` project is read/use-only against the bootstrap tenant.
4. **Teardown robustness** — if a spec fails, teardown must still clean up. `globalTeardown` runs even on failure; the delete must be idempotent (ignore 404/409) and log the org id so a manual cleanup is possible if delete itself fails.
5. **Test-data pollution** — every run creates a real org on dev memory. Timestamped names (`E2E <purpose> <epoch>`) aid identification; teardown deletes exactly the org recorded in the bootstrap file.

## Verification

- Phase 1 gate: `cd tests/e2e && npx playwright test` fully green against the running dev gateway.
- Phase 2 gate: UI-driven wizard specs green without regressing Phase 1.
- No `gateway/` code changes, so no `go build`/`templ`/unit-test additions are required by this change (test-infra only).
