import { test, expect } from '@playwright/test';
import {
  workflowConfigured,
  workflowObjectId,
  workflowPath,
  gotoWorkflow,
  createExternalIssue,
  setIssueState,
  expectCardTitle,
  expectCardStatus,
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
    await expect(panel.locator('p').filter({ hasText: /work execution|run ended|stale|protocol/i })).toBeVisible();
    await expect(panel.getByRole('button', { name: /clear (stale )?block/i })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  });

  test('board has a sticky fixed first column and a badge-only blocked card', async ({ page }) => {
    test.skip(!workflowObjectId, 'E2E_WORKFLOW_BLOCK_OBJECT_ID not set');
    await gotoWorkflow(page, '/work/board');
    const board = page.getByTestId('work-board');
    await expect(board).toBeVisible();

    // The board renders one matrix per object type and a narrow-screen lane
    // fallback for the same item. Scope to the matrix that contains the fixture
    // card, then use its desktop card (the first in DOM order).
    const card = board.getByTestId(`work-card-${workflowObjectId}`).first();
    await expect(card).toBeVisible();
    const matrix = card.locator('xpath=ancestor::div[contains(@class,"overflow-x-auto")][1]');
    await expect(matrix).toBeVisible();
    expect(await matrix.evaluate((el) => getComputedStyle(el).overflowX)).toBe('auto');
    const first = matrix.locator('.sticky.left-0').first();
    await expect(first).toBeVisible();
    expect(await first.evaluate((el) => getComputedStyle(el).position)).toBe('sticky');

    await expect(card.getByText('blocked', { exact: true })).toBeVisible();
    await expect(card.getByRole('button', { name: /clear|unblock/i })).toHaveCount(0);
    await expect(card.getByPlaceholder(/optional note/i)).toHaveCount(0);
  });

  test('external issue close/reopen is reflected on the board', async ({ page }) => {
    test.skip(process.env.E2E_WORKFLOW_CODING_ISSUES !== '1', 'fixture coding App does not subscribe to issues yet');
    await gotoWorkflow(page, '/work/board');
    const title = `external-${Date.now()}`;
    const issue = await createExternalIssue(title);
    await expectCardTitle(page, title);

    await setIssueState(issue.number, 'closed');
    await expectCardStatus(page, title, 'cancelled');

    await setIssueState(issue.number, 'open');
    await expectCardStatus(page, title, 'ready');
  });
});
