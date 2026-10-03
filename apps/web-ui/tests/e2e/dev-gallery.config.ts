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
    baseURL: 'http://localhost:8097',
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  outputDir: 'test-results-dev',
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
      name: 'dev-gallery',
      testMatch: /.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
