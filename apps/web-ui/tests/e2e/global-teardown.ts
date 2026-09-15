import { chromium, FullConfig } from '@playwright/test';
import fs from 'node:fs';
import { BASE_URL, BOOTSTRAP_FILE, STORAGE_STATE } from './constants/storage';
import type { BootstrapState } from './helpers/bootstrap';

/**
 * The bootstrap tenant ("E2E Main") is reused across runs, so by default we
 * leave it in place. Set E2E_RESET=1 to delete it and force a clean slate on
 * the next run (cascades to its projects and entities).
 */
export default async function globalTeardown(_config: FullConfig) {
  if (process.env.E2E_RESET !== '1') {
    console.log('[teardown] reuse mode — E2E tenant left in place (E2E_RESET=1 to delete)');
    return;
  }

  if (!fs.existsSync(BOOTSTRAP_FILE)) {
    console.log('[teardown] no bootstrap file — nothing to clean up');
    return;
  }
  const state = JSON.parse(fs.readFileSync(BOOTSTRAP_FILE, 'utf8')) as BootstrapState;
  if (!state.orgId) {
    console.log('[teardown] bootstrap file has no orgId — nothing to clean up');
    return;
  }

  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({
      baseURL: BASE_URL,
      storageState: STORAGE_STATE,
    });
    try {
      const resp = await context.request.post(`/orgs/${encodeURIComponent(state.orgId)}/delete`);
      const ok = resp.status() >= 200 && resp.status() < 400;
      console.log(
        `[teardown] delete org ${state.orgId} (${state.orgName}): HTTP ${resp.status()} ${ok ? '✓' : '(ignored)'}`,
      );
    } finally {
      await context.close();
    }
  } finally {
    await browser.close();
  }
}
