# Tasks — add P0 e2e coverage

E2E test specs only (`skip_specs: true`); no product behavior change, so no new Go unit tests apply. Existing harness reused: real OIDC via `auth.setup.ts`, bootstrap org/project/provider, serial `mutations` project, `globalTeardown`. Pattern: API-seed → UI-act → API-verify → `finally` cleanup (see `project-transfer-ui.spec.ts`). Each spec runs green against the running dev gateway (tailnet URL, `task dev`). Design notes: mobile viewports via `setViewportSize`, CSS asserts via `locator.evaluate` computed styles, async delete via `expect.poll`, provider-best-effort gating where dev-memory catalog may be unsynced.

## 1. Harness groundwork

- [x] 1.1 Confirm provider/org scoping empirically on dev gateway: does a fresh project under the bootstrap org (which has a provider) resolve a default model? Does a fresh org + project without provider resolve none? Record outcome in spec comments — decides whether `agent-model-warning-ui` needs a new org or just a new project. Verify: probe via `page.request` against running gateway; note result in design/PR notes.
- [x] 1.2 Check test-user profile API response for `email` presence (backfill source) + confirm account-menu trigger locator stability; add `data-testid="account-avatar-trigger"` to the topbar avatar trigger only if no stable semantic locator exists. Verify: `templ generate` + `go build ./...` clean if touched; locator resolves in browser.

## 2. agent-model-warning-ui.spec.ts (mutations)

- [x] 2.1 Error severity: seed project (or org+project) with no provider, agent on auto/no explicit model → dashboard `/agents/{id}` shows role `alert` with no-provider error text + link to `/settings/providers`. Verify: spec green.
- [x] 2.2 Warning severity: add provider via API + no project default pins model → agent shows warning (providers exist, no default). Verify: green; skip-if-unsynced gate if provider upsert fails on dev memory.
- [x] 2.3 No-alert controls: explicit model on agent → no alert; project default set → no alert. Cleanup: delete seeded agent/org in `finally`. Verify: green in isolation.

## 3. account-menu-ui.spec.ts (mutations)

- [x] 3.1 Open topbar avatar menu → dropdown shows name + email (email matches `/api/profile`, i.e. backfilled from Memory profile, not blank). Verify: green.
- [x] 3.2 Profile identity card: line 2 = email, not duplicated name. Verify: green. If test-user Memory record lacks email, assert actual fallback rendering and note it.

## 4. projects-table-ui.spec.ts (mutations)

- [x] 4.1 Seed 2–3 projects via API in a scratch org (avoid mutating bootstrap org state). Multi-select rows → bulk-delete toolbar → POST `/projects/delete`; rows disappear via `expect.poll` (async 202). Verify: green.
- [x] 4.2 Select-all checkbox toggles all rows; per-row `[data-row-menu-trigger]` → Delete removes single project; menu Open navigates to project/agents. Cleanup in `finally`. Verify: green in isolation.

## 5. orgs-row-click-activate.spec.ts (mutations)

- [x] 5.1 On `/orgs`, click a project row → POST activate → lands `/agents`; project shown active in switcher/sidebar. Cleanup: reactivate bootstrap project in `finally` (do not leave session pointed at scratch project). Verify: green.

## 6. org-settings-hub.spec.ts (mutations)

- [x] 6.1 `/orgs/{id}/settings` renders side rail (Tools / Danger zone); Tools toggle present; Danger zone delete section reachable (delete flow already covered by `org-delete-ui` — render-only here). Verify: green.
- [x] 6.2 Legacy redirect: `request`-level GET `/orgs/{id}/tool-settings` → 301 to `/orgs/{id}/settings` (or page-level if browser follows). Verify: green.

## 7. spotlight-mobile.spec.ts (chromium)

- [x] 7.1 Mobile viewport 390px: spotlight trigger is icon-only (w-9, no pill text); open → palette bounding box ≈ full viewport; X button closes (checkbox unchecked, palette hidden). Verify: green.
- [x] 7.2 Desktop viewport regression: Esc closes palette (covers `2026-09-04-spotlight-escape`). Verify: green.

## 8. css-go-daisy-scan.spec.ts (chromium)

- [x] 8.1 Mobile viewport: hamburger trigger computed `mask-image !== "none"` (icon renders); `#spotlight-toggle` is 0×0 / `opacity: 0` (hidden checkbox). Assert a `lucide--*`-style mask utility + hidden state, not a specific icon class. Verify: green. Closes `css-godaisy-scan-regression-guard` follow-up (update task file status on landing).

## 9. Reconcile + docs

- [x] 9.1 Full suite: `npx playwright test` green (chromium + mutations) against running gateway; confirm new specs don't race parallel read specs or pollute bootstrap tenant. Verify: full run green.
- [x] 9.2 Update `tests/e2e/README.md` coverage table with new specs; if `account-avatar-trigger` testid added, document in the test-ID convention section. Verify: README accurate.
