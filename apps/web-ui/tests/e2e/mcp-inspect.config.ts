import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the MCP servers inspect UI — the disabled-Connect
// branch rendered when a provider publishes no discoverable OAuth metadata.
//
// The inspect outcome depends on memory's admin mcp-servers routes, which the
// live session-mode gateway cannot produce deterministically (it needs a real
// OAuth-required server whose provider omits metadata). This runs against the
// dev-mode (no-auth) mock harness instead: `run-e2e.sh` boots `mock-memory.mjs`
// plus the gateway (AUTH_MODE=dev) and blocks until killed. mock-memory.mjs
// seeds one outbound http server whose inspect answers auth_required +
// unavailable.
//
// Run:  npx playwright test --config=mcp-inspect.config.ts
export default defineConfig({
  testDir: './specs/mcp',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  // The mock is one stateful process; serial workers keep tests from racing its
  // shared server list.
  workers: 1,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'test-results/mcp-html' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: 'http://localhost:8097',
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results/mcp',
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
      name: 'mcp-inspect',
      testMatch: /.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
