# go-daisy: run Playwright e2e suite in CI

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-htmx-v4-migration](../sessions/2026-09-09-htmx-v4-migration.md)

## What

Add a CI job to go-daisy that runs `tests/e2e` (Playwright): build gallery, `task gallery:e2e:start`, `npx playwright test`, stop gallery. go-daisy currently has **no CI checks** (PR #6 merged with an empty `statusCheckRollup`).

## Why

Two e2e problems surfaced this session that CI would have caught earlier:

1. The suite was **red against current master** — specs asserted `#gallery-shell`, an id removed in a shell refactor (`a18ca9c`), so every spec timed out on element-not-found. Fixed in `125672c`, but only noticed by chance while validating the htmx migration.
2. Running the suite required installing a matching chromium build (npm playwright revision vs `~/.cache/ms-playwright` drift: `chromium_headless_shell-1228` missing). A fresh CI runner avoids stale local caches and pins the browser revision via `npx playwright install`.

## Depends on

- none

## Notes

- go-daisy Taskfile already has `test:e2e` (starts gallery on :11001, `npm ci`, `npx playwright install chromium`, `npx playwright test`) and `gallery:e2e:stop`.
- Repo has no CI config today — pick the org's usual runner (GitHub Actions `emergent-company/go-daisy`).
