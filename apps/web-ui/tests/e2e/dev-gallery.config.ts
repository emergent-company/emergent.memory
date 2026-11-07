import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { defineConfig, devices } from '@playwright/test';

// Self-contained config for the dev-gated component gallery (/dev/components).
//
// The gallery route only serves when MEMORY_COMPONENT_GALLERY=on. The external
// session-mode gateway the main suite targets leaves the flag off (404), so the
// gallery spec always skipped there. This config starts the dev-mode mock
// harness (`run-e2e.sh`, which exports MEMORY_COMPONENT_GALLERY=on) — the same
// harness the share suite uses — so the spec exercises the real surface instead
// of skipping fast. The live `chromium` project in `playwright.config.ts`
// excludes `specs/dev` for this reason.
//
// Run:  npx playwright test --config=dev-gallery.config.ts
//
// Ports are derived per run, never pinned (#810). `ports.mjs` probes a free
// gateway/mock pair, writes it to a file and prints it; this config builds its
// baseURL from that selection and hands the same file to `run-e2e.sh` as
// E2E_PORTS_FILE, so the config and the servers cannot disagree.
const { gateway: port, mock, file: portsFile } = JSON.parse(
  execFileSync(process.execPath, [path.join(__dirname, 'ports.mjs'), '--lane', 'dev-gallery'], {
    encoding: 'utf8',
  }),
) as { gateway: number; mock: number; file: string };
process.env.MEMORY_PORT = String(port);
process.env.MOCK_PORT = String(mock);
process.env.E2E_PORTS_FILE = portsFile;
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: './specs/dev',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'test-results-dev' }],
  ],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results-dev',
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
      name: 'dev-gallery',
      testMatch: /.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
