import { test as base, expect } from '@playwright/test';
import { resetMockStore } from './helpers/mock';

/**
 * The `test` every spec in the hermetic mock suite should use.
 *
 * The mock harness keeps one store for a whole Playwright run — one process
 * backs every spec file (`datasources.config.ts`: `workers: 1`,
 * `fullyParallel: false`) — so without a reset a spec's empty list is really an
 * assertion about run order: whichever file wrote an OAuth app / source /
 * credential first leaks it into everything after it (#633). The auto fixture
 * below POSTs the mock's `/__mock/reset` before *every* test, so each test
 * starts from the documented seed (mock-memory.mjs) and a spec must not — and
 * cannot — rely on another spec's residue.
 *
 * It resets before the test body but not before the `page` fixture is created,
 * which is fine: nothing navigates or mutates until the body runs.
 *
 * Specs that do not talk to the mock keep importing `test` from
 * `@playwright/test`; this fixture would fail on a stack with no mock.
 */
export const test = base.extend<{ mockStoreReset: void }>({
  mockStoreReset: [
    async ({}, use) => {
      await resetMockStore();
      await use();
    },
    { auto: true },
  ],
});

export { expect };
