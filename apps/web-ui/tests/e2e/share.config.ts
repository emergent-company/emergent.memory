import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the public agent-share surface. The share page is
// anonymous and the owner flow needs no real OIDC session, so this runs against
// the dev-mode (no-auth) mock harness: `run-e2e.sh` boots `mock-memory.mjs`
// plus the gateway (AUTH_MODE=dev) and blocks until killed. No `setup` project,
// no storage state — every test drives its own fresh browser context.
//
// Run:  npx playwright test --config=share.config.ts
//
// Ports are derived per run, never pinned (#810). `ports.mjs` probes a free
// gateway/mock pair, writes it to a file and prints it; this config builds its
// baseURL from that selection and hands the same file to `run-e2e.sh` as
// E2E_PORTS_FILE, so the config and the servers cannot disagree. Two concurrent
// runs of this job on one host therefore get different pairs.
const { gateway: port, mock, file: portsFile } = JSON.parse(
  execFileSync(process.execPath, [path.join(__dirname, 'ports.mjs'), '--lane', 'share'], {
    encoding: 'utf8',
  }),
) as { gateway: number; mock: number; file: string };
process.env.MEMORY_PORT = String(port);
process.env.MOCK_PORT = String(mock);
process.env.E2E_PORTS_FILE = portsFile;
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
    env: { E2E_PORTS_FILE: portsFile },
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
