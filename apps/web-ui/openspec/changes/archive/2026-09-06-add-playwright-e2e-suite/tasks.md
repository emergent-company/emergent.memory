# Tasks — add Playwright e2e suite

Test infrastructure only (`skip_specs: true`); no gateway/product code changes, so no
new Go unit tests apply. Each task ends with its verification gate. Phase 1 must be fully
green before Phase 2 starts.

## Phase 1 — Harness + API-driven bootstrap + full surface

- [x] 1.1 Scaffold `tests/e2e/`: add `dotenv` + `@types/node` to `package.json`; add `tsconfig.json`; confirm `@playwright/test` and browser binaries installed. Verify: `cd tests/e2e && npx playwright --version` runs.
- [x] 1.2 Add `.env.e2e` (gitignored) with `E2E_TEST_USER_EMAIL` / `E2E_TEST_USER_PASSWORD`; update `.gitignore` for `.env.e2e`, `.auth/`, `test-results/`. Verify: `git check-ignore tests/e2e/.env.e2e tests/e2e/.auth/state.json` reports them ignored.
- [x] 1.3 Write `playwright.config.ts`: `baseURL=http://alfred-dev.tail0358fa.ts.net:8095`, `setup` + `chromium` projects (storageState `.auth/state.json`, `chromium` depends on `setup`, fullyParallel), `globalTeardown`, reporters (list + html + json), sensible `timeout`/`expect.timeout`. Verify: `npx playwright test --list` enumerates setup + chromium projects without starting a server.
- [x] 1.4 Write `helpers/auth.ts`: `login(page)` drives `/auth/login` → sign-in → Zitadel login (loginName → password → skip 2FA → forced password change) → callback. Spike Zitadel selectors first. Verify: an `auth.setup.ts` run saves `.auth/state.json` and a subsequent authenticated navigation reaches `/orgs` without a 302 to `/auth/login`.
- [x] 1.5 Write `auth.setup.ts`: login (1.4), assert session active, save storage state. Verify: `test-results` shows the setup project green in isolation.
- [x] 1.6 Write `helpers/bootstrap.ts`: `createOrg`, `createProject`, `configureProvider` via `page.request` with the session; write `{orgId, projectId, orgName}` to `test-results/.bootstrap.json`; timestamped names. Verify: a dry bootstrap run creates a real org+project on dev memory and the JSON file contains both ids.
- [x] 1.7 Write `globalTeardown`: read `.bootstrap.json`, `POST /orgs/:id/delete`, idempotent (ignore 404/409), log org id. Verify: after a run, the org is gone (GET `/api/orgs` no longer lists it); teardown also passes when the org was already deleted.
- [x] 1.8 Specs — read/use surface (one shared session + active org/project). Cover: `agents`, `chat`, `documents`, `objects`, `settings/providers`, `orgs`, `members`, `profile`. Verify: `npx playwright test` fully green against the running dev gateway.
- [x] 1.9 Wire the suite into CI/documentation: add `tests/e2e/README.md` (run instructions, creds location, prerequisites). Verify: README steps reproduce a green run from a clean checkout (creds supplied).

## Phase 2 — UI-driven wizard/form specs

- [x] 2.1 `specs/org-create-ui.spec.ts`: drive `/orgs/new` → create org via the real form; assert redirect to the new org landing and the org appears in the sidebar/`/orgs` list. Verify: green in isolation.
- [x] 2.2 `specs/project-create-ui.spec.ts`: drive the project create flow (from the org landing or switcher); assert the new project becomes active. Verify: green in isolation.
- [x] 2.3 `specs/provider-config-ui.spec.ts`: drive `/settings/providers/new` → add provider (deepseek, api_key + optional model), then `:provider/edit` → update; assert the panel reflects the saved config and the "Test connection" flow surfaces a toast. Verify: green in isolation.
- [x] 2.4 Full-suite reconciliation: run Phase 1 + Phase 2 together; ensure UI-driven mutations (org/project/provider changes) don't race the parallel read specs — move mutating specs to a serial project if needed. Verify: `npx playwright test` fully green end-to-end, teardown leaves dev memory clean.

## Notes

- No gateway code changes → no `go build ./...`, `templ generate`, or `task lint` additions required by this change. If a helper later needs a unit test, add it under `tests/e2e/` with a lightweight runner, but it is not required for the e2e infrastructure itself.
- **Provider config is best-effort in Phase 1.** The dev memory backend rejects the upsert with `memory 400 bad_request: generative model test failed: no models in catalog for provider deepseek (sync models before testing)` — the deepseek model catalog is not synced on `api.dev.emergent-company.ai`. Once the catalog is synced (or a real key + synced catalog is available), `configureProvider` succeeds and the provider row assertion can be restored; the form flow is covered by 2.3 regardless.
