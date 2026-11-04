import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the public agent-share surface. The share page is
// anonymous and the owner flow needs no real OIDC session, so this runs against
// the dev-mode (no-auth) mock harness: `run-e2e.sh` boots `mock-memory.mjs`
// plus the gateway (AUTH_MODE=dev) and blocks until killed. No `setup` project,
// no storage state — every test drives its own fresh browser context.
//
// Run:  npx playwright test --config=share.config.ts
//
// The gateway port is overridable so this suite can run alongside another
// harness on one machine without sharing a server: set MEMORY_PORT (and a
// matching MOCK_PORT) when something else already holds 8097. CI runs the
// suites sequentially in one job, so it sets neither and this default applies.
// `run-e2e.sh` reads the same variables.
const port = process.env.MEMORY_PORT ?? '8097';
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: './specs/share',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  // The mock is a single stateful process; serial workers keep tests from
  // racing its shared share-link/session state.
  workers: 1,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'test-results-share' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results-share',
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
      name: 'share',
      testMatch: /.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
