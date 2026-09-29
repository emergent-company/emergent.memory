import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Deterministic client-render contract for the chat Sources footer. No LLM and
// no backend fixture: we build an assistant bubble in the page and hand it to
// the shared renderer (MemoryChatComponents.attachSources) exactly as the live
// stream and history paths do, then assert the footer contract (in-bubble,
// collapsed by default, expand, row testids, link safety, human type labels).
// The compiled-type map the shell normally embeds is seeded in-page so the
// schema-label path (map label replaces the raw type) is exercised, not just
// the humanizer fallback.
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

// seedObjectTypes replaces the shell's embedded type map (or injects one) with a
// known map so the schema-label path is deterministic. Runs before the first
// objectTypes() read (attachSources), so the lazily-cached map is this one.
async function seedObjectTypes(
  page: import('@playwright/test').Page,
  types: Record<string, { label: string; color?: string }>,
) {
  await page.evaluate((map) => {
    const el = document.getElementById('memory-object-types') as HTMLScriptElement | null;
    const target = el ?? document.createElement('script');
    target.type = 'application/json';
    target.id = 'memory-object-types';
    if (!el) document.head.appendChild(target);
    target.textContent = JSON.stringify(map);
  }, types);
}

test.describe('Chat sources footer', () => {
  test('citations render as a collapsed in-bubble footer', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    // The map label is intentionally distinct from the humanizer output
    // (humanizeTypeName('LegalParagraph') === "Legal paragraph"), so asserting
    // the rendered badge equals the map label proves the schema-label path.
    await seedObjectTypes(page, {
      LegalParagraph: { label: 'Legal provision', color: '#4F46E5' },
    });

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

    // The compiled-type map supplies the label, not the raw name or the
    // humanizer: "LegalParagraph" renders the seeded map label "Legal provision",
    // which is intentionally distinct from humanizeTypeName's "Legal paragraph".
    const humanized = await page.evaluate(() =>
      (window as unknown as {
        MemoryChatComponents: { humanizeTypeName: (s: string) => string };
      }).MemoryChatComponents.humanizeTypeName('LegalParagraph'),
    );
    expect(humanized).toBe('Legal paragraph');
    await expect(bubble.locator('[data-testid=chat-source-type]').nth(0)).toHaveText('Legal provision');
    // A type absent from the seeded map falls back to the humanized name.
    await expect(bubble.locator('[data-testid=chat-source-type]').nth(1)).toHaveText('Zzz unknown type');

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
