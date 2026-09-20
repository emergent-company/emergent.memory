import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the public agent-share surface. The share page is
// anonymous and the owner flow needs no real OIDC session, so this runs against
// the dev-mode (no-auth) mock harness: `run-e2e.sh` boots `mock-memory.mjs`
// plus the gateway (AUTH_MODE=dev) and blocks until killed. No `setup` project,
// no storage state — every test drives its own fresh browser context.
//
// Run:  npx playwright test --config=share.config.ts
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
    baseURL: 'http://localhost:8097',
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results-share',
  webServer: {
    command: 'bash run-e2e.sh',
    cwd: __dirname,
    url: 'http://localhost:8097/api/health',
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
