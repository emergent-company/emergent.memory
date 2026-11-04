import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the Data Sources surface (Google Drive integration).
//
// The data-sources UI needs a stateful upstream (providers, sources, sync jobs,
// document versions) that the live session-mode gateway cannot produce without
// real Drive credentials. This runs against the dev-mode mock harness
// (`run-e2e.sh` boots `mock-memory.mjs` + the gateway with AUTH_MODE=dev), so
// the connect → sync → version-history flows are exercised deterministically,
// with no Google credentials and no OIDC session.
//
// Run:  npx playwright test --config=datasources.config.ts
//
// The gateway port is overridable so this suite can run alongside another
// harness on one machine without sharing a server (see share.config.ts). CI
// runs the suites sequentially in one job, one stack at a time, so it leaves
// this default at 8097 — the same number `share` uses, just never at the same
// moment. `run-e2e.sh` reads the same variables.
const port = process.env.MEMORY_PORT ?? '8097';
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: './specs/datasources',
  // The mock is one stateful process; serial workers keep tests from racing the
  // shared source/job store. Specs still self-clean the sources they create.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'test-results/datasources-html' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results/datasources',
  webServer: {
    command: 'bash run-e2e.sh',
    cwd: __dirname,
    url: `${baseURL}/api/health`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
  projects: [
    {
      name: 'datasources',
      testMatch: /.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
