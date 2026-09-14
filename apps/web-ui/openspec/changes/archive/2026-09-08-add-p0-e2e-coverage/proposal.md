## Why

Recent gateway work (2026-09-06→HEAD) shipped several user-facing features and bug fixes — agent no-resolvable-model warnings, account identity (avatar menu), org-landing projects table (bulk delete), clickable org project rows, org Settings hub, mobile spotlight, and the go-daisy CSS scan fix — but almost none of it has Playwright coverage. `org.spec.ts`/`profile.spec.ts` only smoke-render pages; the mutations that these features introduced (bulk delete, row activation, account menu, alert rendering, mobile-only CSS) are exercised only by ad-hoc unit assertions and repeatedly-deferred manual browser checks. Bugs like the cancelled go-daisy `@source` scan (blank hamburger icon, visible `#spotlight-toggle`) can regress silently because nothing asserts computed styles. The suite needs e2e specs that pin these behaviors.

## What Changes

Add seven Playwright e2e specs (all in the existing serial `mutations` / chromium projects, `tests/e2e/specs/`, using the established API-seed → UI-act → API-verify → `finally`-cleanup pattern):

- `agent-model-warning-ui.spec.ts` — error alert when project has no provider, warning alert when providers exist but no project default resolves, no alert when model is explicit or default resolves (`9c573b7`).
- `account-menu-ui.spec.ts` — avatar-only topbar trigger opens menu showing name + email (email backfilled from Memory profile); profile identity card line 2 shows email, not duplicated name; no chevron on trigger (`0a78ab7`, `09062ef`, `add7993`).
- `projects-table-ui.spec.ts` — org landing data table: multi-select + bulk delete, select-all, per-row 3-dots menu Delete, async-delete tolerance (`fad54c2`→`e428ec8`).
- `orgs-row-click-activate.spec.ts` — clicking a project row on `/orgs` activates it and lands on `/agents` (`b4d9b03`).
- `org-settings-hub.spec.ts` — Settings hub side rail renders (Tools / Danger zone); legacy `GET /orgs/:id/tool-settings` still 301-redirects to `/settings` (`c3b15d7`, `8884310`, `add7a3c`).
- `spotlight-mobile.spec.ts` — mobile viewport: icon-only trigger, full-screen palette, X close button; desktop: Esc close regression (`6a6d881`, `774e349`, `2026-09-04-spotlight-escape`).
- `css-go-daisy-scan.spec.ts` — regression guard: hamburger icon has computed `mask-image`, `#spotlight-toggle` checkbox is 0×0/opacity:0 on mobile; closes the `css-godaisy-scan-regression-guard` follow-up (`9fa687f`).

No product behavior change — test specs only. Some assertions may require stable locator hooks already present (`page-*`, `data-row-menu-trigger`, `project-switcher`, `account-menu`) or minor test-only `data-testid` additions.

## Capabilities

### New Capabilities

None — no spec-level behavior change (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- `tests/e2e/specs/` — 7 new spec files (6 mutations-project, 1 chromium-read for CSS/visual assertions).
- `tests/e2e/helpers/` — possible small additions: mobile-viewport fixture, provider/model-config API helper, project seeding helper.
- `gateway/*.templ` — only if a stable locator hook is missing (test-only attributes, invisible to users); otherwise no gateway changes.
- No changes to API contracts, backend behavior, or the memory backend.
- Known dependent coverage gaps deliberately excluded (P1/P2): cookie `Max-Age`, provider default-model dropdown grouping, model-catalog multi-model sync, hx-boost title, switcher single-column layout.
