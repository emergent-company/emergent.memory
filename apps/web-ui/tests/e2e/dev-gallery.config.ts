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
// The gateway port is overridable so this suite can run concurrently with the
// other hermetic lanes on one machine/runner without sharing a server (see
// share.config.ts); the default stays 8097 for local runs. `run-e2e.sh` reads
// the same variable.
const port = process.env.MEMORY_PORT ?? '8097';
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
