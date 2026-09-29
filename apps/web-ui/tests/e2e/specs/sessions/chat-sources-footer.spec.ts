import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Deterministic client-render contract for the chat Sources footer. No LLM and
// no backend fixture: we build an assistant bubble in the page and hand it to
// the shared renderer (MemoryChatComponents.attachSources) exactly as the live
// stream and history paths do, then assert the footer contract (in-bubble,
// collapsed by default, expand, row testids, link safety, human type labels).
const UUID_A = '11111111-1111-4111-8111-111111111111';
const UUID_B = '22222222-2222-4222-8222-222222222222';

type SourceItem = {
  kind: string;
  id: string;
  type: string;
  label: string;
  url: string;
};

async function attachSources(page: import('@playwright/test').Page, items: SourceItem[]) {
  await page.waitForFunction(() => !!(window as unknown as { MemoryChatComponents?: unknown }).MemoryChatComponents);
  await page.evaluate((citations: SourceItem[]) => {
    const mc = (window as unknown as {
      MemoryChatComponents: { attachSources: (wrap: Element, list: unknown[]) => void };
    }).MemoryChatComponents;
    const wrap = document.createElement('div');
    wrap.id = 'e2e-sources-host';
    wrap.className = 'chat chat-start memory-rise';
    wrap.innerHTML =
      '<div class="chat-bubble chat-bubble-neutral"><div class="memory-md">answer</div></div>';
    document.body.appendChild(wrap);
    // Called twice on purpose: attachSources must be idempotent.
    mc.attachSources(wrap, citations);
    mc.attachSources(wrap, citations);
  }, items);
}

test.describe('Chat sources footer', () => {
  test('citations render as a collapsed in-bubble footer', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    await attachSources(page, [
      { kind: 'object', id: UUID_A, type: 'LegalParagraph', label: 'Lov om aksjeselskaper', url: `/objects/${UUID_A}` },
      { kind: 'object', id: UUID_B, type: 'ZZZUnknownType', label: 'Ada Lovelace', url: 'javascript:alert(1)' },
      { kind: 'object', id: 'not-a-uuid', type: 'Thing', label: 'Unsafe source', url: 'https://evil.example/x' },
    ]);

    const bubble = page.locator('#e2e-sources-host .chat-bubble');
    const footer = bubble.getByTestId('chat-sources');

    // The footer lives INSIDE the assistant bubble — never a sibling message row.
    await expect(footer).toHaveCount(1);
    await expect(page.locator('.chat.chat-start.memory-sources')).toHaveCount(0);

    // Collapsed by default; summary shows the singular-aware count.
    await expect(page.getByTestId('chat-sources-toggle')).toHaveText(/3 sources/);
    expect(await footer.evaluate((el) => (el as HTMLDetailsElement).open)).toBe(false);

    // One compact row per source, with name + human type label.
    await expect(page.getByTestId('chat-source')).toHaveCount(3);
    await expect(bubble.locator('[data-testid=chat-source-name]').first()).toHaveText('Lov om aksjeselskaper');

    const firstType = bubble.locator('[data-testid=chat-source-type]').first();
    await expect(firstType).not.toHaveText('LegalParagraph');
    // Humanized fallback for a type the project map almost certainly lacks:
    const humanized = await page.evaluate(() =>
      (window as unknown as {
        MemoryChatComponents: { humanizeTypeName: (s: string) => string };
      }).MemoryChatComponents.humanizeTypeName('LegalParagraph'),
    );
    expect(humanized).toBe('Legal paragraph');

    // Link safety: a valid /objects/<uuid> url and a UUID id become links; an
    // untrusted url with a non-UUID id stays plain text (no anchor).
    await expect(bubble.locator('[data-testid=chat-source]').nth(0).locator('a')).toHaveAttribute(
      'href',
      `/objects/${UUID_A}`,
    );
    await expect(bubble.locator('[data-testid=chat-source]').nth(1).locator('a')).toHaveAttribute(
      'href',
      `/objects/${UUID_B}`,
    );
    await expect(bubble.locator('[data-testid=chat-source]').nth(2).locator('a')).toHaveCount(0);

    // Expanding the disclosure reveals the rows.
    await page.getByTestId('chat-sources-toggle').click();
    expect(await footer.evaluate((el) => (el as HTMLDetailsElement).open)).toBe(true);
    await expect(page.getByTestId('chat-source').first()).toBeVisible();
  });

  test('a single citation reads "1 source"', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    await attachSources(page, [
      { kind: 'object', id: UUID_A, type: 'Zoology', label: 'Solo source', url: `/objects/${UUID_A}` },
    ]);

    await expect(page.getByTestId('chat-sources-toggle')).toHaveText(/1 source/);
    await expect(page.getByTestId('chat-source')).toHaveCount(1);
  });

  test('no citations renders no footer', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    await attachSources(page, []);

    await expect(page.getByTestId('chat-sources')).toHaveCount(0);
  });
});
