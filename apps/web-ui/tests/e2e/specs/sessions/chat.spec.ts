import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Chat page', () => {
  test('renders (empty state or chat UI)', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    // A fresh tenant has no agents → empty state; a reused tenant (with agents
    // created by the mutation specs) shows the input + session rail instead.
    const empty = page.getByRole('heading', { name: /no agents to talk to/i });
    if (await empty.isVisible().catch(() => false)) {
      await expect(page.getByRole('link', { name: /create an agent/i })).toBeVisible();
    } else {
      await expect(page.getByTestId('chat-input')).toBeVisible();
    }
  });
});
