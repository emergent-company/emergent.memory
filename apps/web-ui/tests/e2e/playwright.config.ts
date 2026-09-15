import { defineConfig, devices } from '@playwright/test';
import { config as loadEnv } from 'dotenv';
import path from 'node:path';
import { BASE_URL, STORAGE_STATE } from './constants/storage';

// Load the gitignored test-user credentials before config reads them.
loadEnv({ path: path.join(__dirname, '.env.e2e') });

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
      use: { ...devices['Desktop Chrome'], storageState: STORAGE_STATE },
      dependencies: ['setup'],
      workers: 1,
    },
  ],
});
