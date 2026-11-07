import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { defineConfig } from '@playwright/test';

// Per-PR hermetic gateway browser smoke (#564).
//
// The `*-ui.spec.ts` mutations suite is the only coverage of real gateway UI
// behaviour, but it needs a session-mode gateway + Zitadel test user and runs
// only on a schedule. This project boots the same dev-mode mock harness as
// `datasources`/`share` (`run-e2e.sh`: mock-memory.mjs + gateway AUTH_MODE=dev)
// and drives the highest-risk browser contracts on the pages UI PRs touch:
// the upload dialog, the app shell, the chat rail. No auth, no live backend.
//
// Ports are derived per run, never pinned (#810). `ports.mjs` probes a free
// gateway/mock pair, writes it to a file and prints it; this config builds its
// baseURL from that selection and hands the same file to `run-e2e.sh` as
// E2E_PORTS_FILE, so the config and the servers cannot disagree. The resolved
// values are exported to the test process too, so fixture-contract specs that
// read the mock directly agree with the servers.
const { gateway: port, mock: mockPort, file: portsFile } = JSON.parse(
  execFileSync(process.execPath, [path.join(__dirname, 'ports.mjs'), '--lane', 'gateway-smoke'], {
    encoding: 'utf8',
  }),
) as { gateway: number; mock: number; file: string };
process.env.MEMORY_PORT = String(port);
process.env.MOCK_PORT = String(mockPort);
process.env.E2E_PORTS_FILE = portsFile;
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: './specs/gateway-smoke',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'test-results/gateway-smoke-html' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results/gateway-smoke',
  webServer: {
    // The runner reads the same reservation file this config selected from.
    command: 'bash run-e2e.sh',
    cwd: __dirname,
    env: { E2E_PORTS_FILE: portsFile },
    url: `${baseURL}/api/health`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
