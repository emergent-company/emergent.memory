import { execFileSync } from 'node:child_process';
import path from 'node:path';
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
// Ports are derived per run, never pinned (#810). `ports.mjs` probes a free
// gateway/mock pair, writes it to a file and prints it; this config builds its
// baseURL from that selection and hands the same file to `run-e2e.sh` as
// E2E_PORTS_FILE, so the config and the servers cannot disagree. Two concurrent
// runs of this job on one host therefore get different pairs.
const { gateway: port, mock, file: portsFile } = JSON.parse(
  execFileSync(process.execPath, [path.join(__dirname, 'ports.mjs'), '--lane', 'datasources'], {
    encoding: 'utf8',
  }),
) as { gateway: number; mock: number; file: string };
process.env.MEMORY_PORT = String(port);
process.env.MOCK_PORT = String(mock);
process.env.E2E_PORTS_FILE = portsFile;
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
    env: { E2E_PORTS_FILE: portsFile },
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
