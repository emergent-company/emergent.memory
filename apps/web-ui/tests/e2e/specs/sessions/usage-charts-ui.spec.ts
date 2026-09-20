import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Usage dashboard render: gateway/usage.templ `uiUsage` (GET /usage). The
// summary cards (total tokens, estimated cost, sessions), the two daily chart
// mounts, and the embedded `#usage-timeseries` JSON payload only render when
// the tenant has usage data (`hasUsageData(summary, series)`); an idle tenant
// with empty summary + time-series renders the "No usage data" empty state
// instead. This spec therefore asserts the populated state when data is
// present and the empty state otherwise, so it never flakes on the shared
// bootstrap tenant. The "Current month spend" card is not asserted either way —
// it is omitted when the backend reports no month-spend data (`hasMonthSpend`).

test('usage dashboard renders summary cards and chart mounts when data exists', async ({ page }) => {
  await page.goto('/usage');
  await expectAppPage(page, /Usage/);

  // The populated summary/chart section is keyed off `stat-total-tokens`; its
  // absence means the empty state is rendering.
  if ((await page.getByTestId('stat-total-tokens').count()) > 0) {
    await expect(page.getByTestId('stat-total-tokens')).toBeVisible();
    await expect(page.getByTestId('stat-estimated-cost')).toBeVisible();
    await expect(page.getByTestId('stat-sessions')).toBeVisible();

    await expect(page.getByTestId('usage-token-chart')).toBeVisible();
    await expect(page.getByTestId('usage-session-chart')).toBeVisible();
    await expect(page.locator('#usage-timeseries')).toBeVisible();
  } else {
    await expect(page.getByText('No usage data')).toBeVisible();
  }
});
