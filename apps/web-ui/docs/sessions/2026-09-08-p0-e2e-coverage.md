# 2026-09-08 — P0 e2e coverage (Playwright)

## Goal
Analyze the latest gateway changes (read the collected session logs in `docs/sessions/`),
recommend Playwright e2e specs covering the newest features + bug fixes, then plan,
implement, and archive an OpenSpec change for the P0 set.

## Outcome
Done. Analyzed 14 session logs (2026-09-06→08) + mapped the existing 29-spec e2e suite,
proposed a P0 gap list, then took it through the full OpenSpec lifecycle:
propose → apply → archive (`add-p0-e2e-coverage`, `skip_specs: true`).

Seven new spec files authored (5 parallel fixer lanes + direct selector/geometry fixes),
full suite green: **67 passed, 1 skipped** in 1.4m.

## Decisions
- **`skip_specs: true` for the change** — e2e specs only, no product behavior change;
  matches archived `2026-09-06-add-playwright-e2e-suite` / `e2e-full-surface-coverage` precedent.
- **Seven independent authoring lanes** (one spec file each, fixers) — disjoint write scope,
  parallel authoring; browser runs kept serial (orchestrator) to avoid racing the shared
  `.auth/state.json` + bootstrap tenant on the live dev gateway.
- **API-seed → UI-act → API-verify → `finally`-cleanup pattern** (per `project-transfer-ui.spec.ts`)
  — deterministic, self-cleaning; scratch orgs deleted + `expect.poll` for async deletes.
- **Warning-severity agent-model test is skip-gated** — dev memory rejects provider upsert
  (`generative model test failed ... 401` fake key / unsynced catalog), same gate as `auth.setup`;
  documented skip rather than a red herring failure.
- **Mobile/CSS asserts via computed styles + viewport-relative geometry** — `mask-image`/`::after`
  content and proportional full-screen checks (headless dvh reads ~4% under the set viewport);
  icon-only trigger tolerates the `btn` border (36px ± 4).
- **Projects-table locators scoped to the delete form** — the topbar project switcher also
  carries hidden `projectId` inputs; page-wide `input[name=projectId]` counts were wrong.
- **Account-menu trigger already had `data-testid="account-menu-trigger"`** — no templ change
  needed; semantic locators for the avatar dropdown were stable.

## Changes
- `openspec/changes/add-p0-e2e-coverage/` → `openspec/changes/archive/2026-09-08-add-p0-e2e-coverage/`
  — proposal, design, tasks (planning artifacts).
- `tests/e2e/specs/agent-model-warning-ui.spec.ts` — error severity (provider-less project),
  warning severity (skip-gated), no-alert on explicit model; covers `9c573b7`.
- `tests/e2e/specs/account-menu-ui.spec.ts` — avatar-only trigger, dropdown name+email
  (backfill regression), profile card email-not-duplicated-name; covers `0a78ab7`/`09062ef`/`add7993`.
- `tests/e2e/specs/projects-table-ui.spec.ts` — select-all + bulk delete (async-202 tolerant),
  per-row menu delete, row-menu Open activation.
- `tests/e2e/specs/orgs-row-click-activate.spec.ts` — `/orgs` project row → activate → `/agents`
  (bootstrap project reactivated in cleanup).
- `tests/e2e/specs/org-settings-hub.spec.ts` — Tools/Danger-zone rail + legacy
  `tool-settings`→`settings` 301.
- `tests/e2e/specs/spotlight-mobile.spec.ts` — desktop pill + Esc close, mobile icon-only +
  full-screen sheet + ✕ close.
- `tests/e2e/specs/css-go-daisy-scan.spec.ts` — hamburger `mask-image` + hidden
  `#spotlight-toggle` regression guard (closes `css-godaisy-scan-regression-guard`).
- `tests/e2e/README.md` — coverage section updated with the new smoke/mutation specs.
- `docs/tasks/css-godaisy-scan-regression-guard.md` — marked closed (covered by e2e spec).

## Verification
- `cd tests/e2e && npx playwright test` (full suite, live dev gateway) — **67 passed, 1 skipped**.
  The skip = agent-model-warning warning-severity (provider upsert unavailable on dev memory).
- Per-spec `npx playwright test --list` compile gates during authoring.
- `openspec validate add-p0-e2e-coverage` — valid (32/32 changes pass).
- Fix loop: 3 first-run failures (projectId locator scope, trigger width tolerance,
  `.modal-box` strict-mode multiplicity + dvh height) all corrected and re-verified green.

## Open questions / follow-ups
- Agent-model-warning warning-severity e2e is skip-gated until dev memory has a synced
  provider catalog (or a real key). My full-suite run hit the gate cleanly (1 skipped); a
  parallel session (`2026-09-08-prg-toast-titles`) saw that case FAIL rather than skip in its
  run — same root cause, tracked as `e2e-agent-model-warning-provider-env`. Worth reconciling
  why the skip fired in one run but not the other (provider upsert sticking intermittently).
- `verify-agent-model-ui-browser` remains open: Model column "(default)" tags on the agents
  list are still only manually verifiable (not in the new specs).

## Tasks
- [e2e-agent-model-warning-provider-env](../tasks/e2e-agent-model-warning-provider-env.md) — already tracked by the parallel prg-toast-titles session; fix the env-dependent warning-severity/no-alert e2e cases (skip vs fail).
- Closed this session (superseded by e2e): verify-spotlight-mobile-browser,
  verify-orgs-project-row-click, verify-org-settings-hub-browser, verify-projects-table-browser,
  verify-account-identity-browser, css-godaisy-scan-regression-guard.
