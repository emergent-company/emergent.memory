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
// Ports are overridable so this suite can run alongside another harness on one
// machine. CI runs the suites sequentially in one job — one stack at a time —
// so these defaults only have to be free when it is this suite's turn.
const port = process.env.MEMORY_PORT ?? '8096';
const mockPort = process.env.MOCK_PORT ?? '5304';
// The webServer below boots the mock on this port, but its env does not flow
// back to the test process. Export the resolved value so fixture-contract specs
// that read the mock directly (the discovery start/cancel shapes) agree with a
// default-port run and a `MOCK_PORT=…` one alike.
process.env.MOCK_PORT ??= mockPort;
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
    // run-e2e.sh defaults to 8097/5301; pass this lane's ports explicitly so
    // `npx playwright test --config=gateway-smoke.config.ts` with no env works.
    command: `MEMORY_PORT=${port} MOCK_PORT=${mockPort} bash run-e2e.sh`,
    cwd: __dirname,
    url: `${baseURL}/api/health`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
