import { defineConfig } from '@playwright/test';

// Hermetic, gateway-free Playwright config for the static-JS wiring gate.
//
// The full suite (`playwright.config.ts`) drives a real gateway in session mode
// plus a Zitadel test user and the live memory API, so it cannot run in CI. The
// specs under `specs/js/` instead load the shipped `chat-components.js`
// verbatim into a bare page (`page.addScriptTag`) and drive its DOM renderers
// and event wiring in a real Chromium engine — no gateway, no memory API, no
// auth, no `.env.e2e`. This is the regression guard for the #1216-class defect
// (an event handler referencing an undefined identifier, which `node --check`
// cannot see because it is a runtime ReferenceError, not a syntax error).
//
// Two rules keep a spec honest about what it proves (#1083):
//   - load shipped code: page.addScriptTag the real static JS, never a copy;
//   - never inline server-rendered markup by hand. When a spec needs markup a
//     templ renders, commit it under specs/js/fixtures/ as generated output
//     (apps/web-ui/gateway/object_preview_e2e_fixtures_test.go re-renders it on
//     every `go test` run and fails on drift) and fs.readFile it. A hand-copied
//     fixture asserts on itself: reverting the template leaves the spec green.
//     Bare skeletons that only pin ids/data-attributes for JS wiring are fine,
//     but their header must not claim to verify the template.
//
// Run:  npx playwright test --config=js-dom.config.ts
export default defineConfig({
  testDir: './specs/js',
  // The specs are read-only against a file on disk; keep it serial so failures
  // are deterministic and the run stays well under a minute.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list']],
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    headless: true,
    trace: 'off',
    screenshot: 'off',
  },
  outputDir: 'test-results-js',
});
