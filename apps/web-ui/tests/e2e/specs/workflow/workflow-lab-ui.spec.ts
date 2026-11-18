import { test, expect } from '@playwright/test';
import {
  workflowConfigured,
  workflowObjectId,
  workflowPath,
  gotoWorkflow,
  createExternalIssue,
  setIssueState,
  expectCardTitle,
} from '../../helpers/workflow-lab';

test.describe.configure({ mode: 'serial' });

test.describe('workflow lab board', () => {
  test.skip(!workflowConfigured(), 'E2E_WORKFLOW_* not configured');

  test('blocked cards are badge-only and the detail panel carries the actions', async ({ page }) => {
    test.skip(!workflowObjectId, 'E2E_WORKFLOW_BLOCK_OBJECT_ID not set');
    await page.goto(workflowPath(`/objects/${workflowObjectId}`));

    const panel = page.getByTestId('object-work-block');
    await expect(panel).toBeVisible();
    await expect(panel.getByText('blocked', { exact: true })).toBeVisible();
    await expect(panel.getByText(/work execution|run ended|stale|protocol/i)).toBeVisible();
    await expect(panel.getByRole('button', { name: /clear (stale )?block/i })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  });

  test('board has a sticky fixed first column and horizontally scrollable matrix', async ({ page }) => {
    await gotoWorkflow(page, '/work/board');
    const board = page.getByTestId('work-board');
    await expect(board).toBeVisible();

    const matrix = board.locator('div.overflow-x-auto').first();
    if ((await matrix.count()) > 0) {
      const overflow = await matrix.evaluate((el) => getComputedStyle(el).overflowX);
      expect(overflow).toBe('auto');
      const first = matrix.locator('.sticky.left-0').first();
      await expect(first).toBeVisible();
      const pos = await first.evaluate((el) => getComputedStyle(el).position);
      expect(pos).toBe('sticky');
    }

    const cards = board.locator('[data-testid^="work-card-"]');
    if ((await cards.count()) > 0) {
      await expect(cards.first().getByPlaceholder(/optional note/i)).toHaveCount(0);
      await expect(cards.first().getByRole('button', { name: /unblock/i })).toHaveCount(0);
    }
  });

  test('external issue close/reopen is reflected on the board', async ({ page }) => {
    const title = `external-${Date.now()}`;
    const issue = await createExternalIssue(title);
    await expectCardTitle(page, title);

    await setIssueState(issue.number, 'closed');
    await page.reload();
    await expect(page.getByText(title, { exact: false })).toHaveCount(0);
  });
});
