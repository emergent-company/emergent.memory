import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Kanban board read surface. The board renders its six canonical status lanes
// whenever at least one work item exists, or a single empty state when none do
// — so the assertion accepts either, matching the actual render contract
// (gateway/board.templ BoardColumns). This file must NOT end in `-ui.spec.ts`
// so it lands in the chromium (read) project.
const COLUMNS = ['ready', 'in_progress', 'review', 'revision', 'blocked', 'done'];

test.describe('Board page', () => {
  test('renders the board under session', async ({ page }) => {
    await page.goto('/board');
    await expectAppPage(page, /Board/);

    // Stable anchors: the main-content root (title-derived) and the board region.
    await expect(page.getByTestId('page-board')).toBeVisible();
    await expect(page.getByTestId('board')).toBeVisible();

    // Render contract: either the six canonical lanes (when items exist — the
    // lanes always render even empty so drag targets stay stable) or the empty
    // state (when no items exist). Never both, never neither.
    const columns = page.locator('[data-board-column]');
    if ((await columns.count()) > 0) {
      for (const status of COLUMNS) {
        await expect(page.locator(`[data-board-column="${status}"]`)).toHaveCount(1);
      }
    } else {
      await expect(page.getByText('No work items')).toBeVisible();
    }
  });

  test('sidebar nav exposes the Board link', async ({ page }) => {
    await page.goto('/board');
    await expectAppPage(page, /Board/);

    const link = page.getByRole('link', { name: 'Board', exact: true });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', '/board');
  });
});
