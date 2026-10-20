import { defineConfig, devices } from '@playwright/test';
import { config as loadEnv } from 'dotenv';
import path from 'node:path';
import { BASE_URL, STORAGE_STATE } from './constants/storage';

// Load the gitignored test-user credentials before config reads them.
loadEnv({ path: path.join(__dirname, '.env.e2e') });

// The live suite targets a real deployment, so there is no baked-in host.
// Fail at config load instead of letting a relative URL silently hit the gateway.
if (!process.env.E2E_MEMORY_API_URL) {
  throw new Error(
    'E2E_MEMORY_API_URL is required (there is no deployment default); set it in .env.e2e or the environment',
  );
}

export default defineConfig({
  testDir: './',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ['list'],
    ['html', { open: 'never' }],
    ['json', { outputFile: 'test-results/results.json' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: BASE_URL,
    headless: true,
    // Capture a trace (step-by-step DOM snapshots + screenshots) for every test
    // so the Playwright UI runner can show visual confirmation, not just on retry.
    trace: 'on',
    screenshot: 'only-on-failure',
  },
  globalTeardown: './global-teardown.ts',
  projects: [
    {
      // One serial login + tenant bootstrap; produces the shared storage state.
      name: 'setup',
      testMatch: /auth\.setup\.ts/,
    },
    {
      // Read/use surface, reusing the bootstrap tenant. Excludes the mutation
      // specs and the live-LLM scenarios. Runs after `setup` (login + tenant +
      // schema seed).
      name: 'chromium',
      testIgnore: [
        /scenarios\/.*\.spec\.ts/,
        /-ui\.spec\.ts/,
        /specs\/connector\/.*\.spec\.ts/,
        /specs\/share\/.*\.spec\.ts/,
        // Gateway-free DOM wiring specs run under their own config
        // (`js-dom.config.ts`) — they need no gateway/session, so the live
        // suite must not schedule them behind `setup`.
        /specs\/js\/.*\.spec\.ts/,
        // Dev-gated component gallery (/dev/components). The live gateway runs
        // with MEMORY_COMPONENT_GALLERY off, so the route 404s and the spec
        // would only ever skip. It runs under its own flag-on harness
        // (`dev-gallery.config.ts`).
        /specs\/dev\/.*\.spec\.ts/,
        // Data-sources surface. It needs a stateful upstream (providers,
        // sources, sync jobs, versions) and no real Drive credentials, so it
        // runs under its own hermetic mock harness (`datasources.config.ts`).
        /specs\/datasources\/.*\.spec\.ts/,
        // MCP inspect surface. It needs a deterministically OAuth-unavailable
        // server that the live memory backend cannot produce, so it runs under
        // its own hermetic mock harness (`mcp-inspect.config.ts`).
        /specs\/mcp\/.*\.spec\.ts/,
        // Live Google Drive gate (`gdrive-live`). Opt-in — real Google OAuth
        // creds plus a human to finish the consent screen — so it runs under
        // its own project, never behind `setup` or the default full run.
        /specs\/gdrive-live\/.*\.spec\.ts/,
      ],
      use: { ...devices['Desktop Chrome'], storageState: STORAGE_STATE },
      dependencies: ['setup'],
    },
    {
      // UI/API mutations (org/project/provider/agent/skill/…). Depends on
      // `setup` only (NOT chromium), so running a single mutation test runs just
      // `setup` (seed) + that test — not the whole read surface. Serialized
      // (single worker) internally; mutations self-clean so they don't pollute
      // the parallel read specs.
      name: 'mutations',
      testMatch: /-ui\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], storageState: STORAGE_STATE },
      dependencies: ['setup'],
      workers: 1,
    },
    {
      // memory-connector CLI auth. Self-contained: drives its own browser
      // sign-in and a temp connector store, so it has NO `setup` dependency and
      // runs without the gateway. Serial (one browser sign-in at a time).
      name: 'connector',
      testMatch: /specs\/connector\/.*\.spec\.ts/,
      workers: 1,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // Live-LLM scenario suite (tests/e2e/scenarios/ — a top-level folder
      // grouping the fresh-project journeys). Depends on `setup` only; specs
      // env-gate: they skip fast when E2E_SCENARIO_LLM_API_KEY is unset, so
      // the default full run stays green.
      name: 'scenarios',
      testMatch: /scenarios\/.*\.spec\.ts/,
      // The strictly chat-UI scenarios moved into scenarios/chat/ and run under
      // the dedicated `chat` project below.
      testIgnore: /scenarios\/chat\//,
      use: { ...devices['Desktop Chrome'], storageState: STORAGE_STATE },
      dependencies: ['setup'],
      workers: 1,
    },
    {
      // Live-LLM chat UI suite (tests/e2e/scenarios/chat/) — the scenarios whose
      // sole subject is the /chat surface (agent switch + session-rail resume,
      // run control: dock/queue/rail). Depends on `setup` only; specs env-gate
      // on E2E_SCENARIO_LLM_API_KEY so the default full run stays green.
      name: 'chat',
      testMatch: /scenarios\/chat\/.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], storageState: STORAGE_STATE },
      dependencies: ['setup'],
      workers: 1,
    },
    {
      // Live Google Drive gate (`specs/gdrive-live/`). Opt-in and NEVER in CI:
      // it configures a project-scoped Google OAuth app, connects a REAL Drive
      // folder, and needs a human to finish the Google consent screen mid-test
      // (the test prints + attaches the authorize URL, then polls for the
      // callback-created source). Serial (single worker) and long (OAuth TTL +
      // three syncs) with no retries.
      name: 'gdrive-live',
      testMatch: /specs\/gdrive-live\/.*\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: STORAGE_STATE,
        trace: 'retain-on-failure',
      },
      dependencies: ['setup'],
      workers: 1,
      retries: 0,
      timeout: 12 * 60_000,
    },
  ],
});
