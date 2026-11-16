import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

// Work board read surface. One status × execution matrix renders per selected
// board-enabled object type (gateway/work.templ WorkBoardFrame), so the assertion
// pins the swap region, the live-refresh wiring and the two legal render shapes
// (at least one matrix, or an empty state) rather than a fixed lane list — the
// lanes belong to the seeded object types, not to the page.
//
// This file must NOT end in `-ui.spec.ts` so it lands in the chromium (read)
// project.

test.describe('Work board page', () => {
  test('renders the board under session', async ({ page }) => {
    await goto(page, '/work/board');
    await expectAppPage(page, /Work/);

    // Stable anchors: the main-content root (title-derived) and the board region.
    await expect(page.getByTestId('page-work')).toBeVisible();
    await expect(page.getByTestId('work-board')).toBeVisible();
    await expect(page.getByTestId('work-board-count')).toBeVisible();

    // Live refresh: the board subscribes to its own stream, and the region the
    // frames morph into carries the project's status map the drag reads.
    await expect(page.locator('#work-board-live')).toHaveAttribute(
      'hx-sse:connect',
      '/work/board/stream',
    );
    await expect(page.getByTestId('work-board')).toHaveAttribute('data-board-status-map', /ready/);

    // Render contract: either at least one matrix (its lanes carry
    // data-board-column) or an empty state. A half-rendered page fails both.
    const columns = page.locator('[data-board-column]');
    if ((await columns.count()) > 0) {
      await expect(columns.first()).toBeVisible();
    } else {
      await expect(page.getByText(/No items|No board-enabled types/)).toBeVisible();
    }
  });

  test('the retired /board URL redirects to the work board', async ({ page }) => {
    await page.goto('/board');
    await expect(page).toHaveURL(/\/work\/board$/);
    await expect(page.getByTestId('work-board')).toBeVisible();
  });

  test('sidebar nav exposes the Board link', async ({ page }) => {
    await goto(page, '/work/board');
    await expectAppPage(page, /Work/);

    const link = page.getByRole('link', { name: 'Board', exact: true });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', /\/work\/board$/);
  });
});
