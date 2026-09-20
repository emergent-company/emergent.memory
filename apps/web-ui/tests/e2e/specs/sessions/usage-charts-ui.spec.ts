import { test, expect } from '@playwright/test';

// Usage dashboard render: gateway/usage.templ `uiUsage` (GET /usage). Asserts
// the always-present summary cards (total tokens, estimated cost, sessions),
// the two daily chart mounts, and the embedded `#usage-timeseries` JSON payload
// that the charts read. The "Current month spend" card is deliberately not
// asserted — it is omitted when the backend reports no month-spend data
// (`hasMonthSpend`), so requiring it would flake on an idle tenant.

test('usage dashboard renders summary cards and chart mounts', async ({ page }) => {
  await page.goto('/usage');

  await expect(page.getByTestId('stat-total-tokens')).toBeVisible();
  await expect(page.getByTestId('stat-estimated-cost')).toBeVisible();
  await expect(page.getByTestId('stat-sessions')).toBeVisible();

  await expect(page.getByTestId('usage-token-chart')).toBeVisible();
  await expect(page.getByTestId('usage-session-chart')).toBeVisible();
  await expect(page.locator('#usage-timeseries')).toBeVisible();
});
