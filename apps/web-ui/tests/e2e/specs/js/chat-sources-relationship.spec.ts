import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for relationship citations in the chat Sources footer (the
// client half of #453). Loads the shipped chat-components.js verbatim and
// mounts citations exactly as the live `citations` frame and history items
// deliver them:
//
//   - a relationship citation's url carries `/objects/<src>#relationship-<rel>`
//     and survives OBJECT_URL_RE + citationHref, so the row links with the
//     fragment object-preview.js needs to render "Referenced via";
//   - a relationship with no trusted url never falls back to its relationship
//     id (that is not an object page);
//   - a server-supplied `type_label`/`type_color` wins over the shell's embedded
//     compiled-type map, with the map still the fallback for old citations.
//
// No gateway, no memory API, no auth: object-preview.js is covered separately;
// this spec stops at the href the drawer receives.

const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);

const SOURCE = '11111111-1111-4111-8111-111111111111';
const REL = '22222222-2222-4222-8222-222222222222';

type SourceItem = {
  kind: string;
  id: string;
  type: string;
  label: string;
  url?: string;
  type_label?: string;
  type_color?: string;
};

async function load(page: Page, types: Record<string, { label: string; color?: string }>) {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><head><script type="application/json" id="memory-object-types"></script></head>' +
      '<body><div id="chat-messages"></div></body></html>',
  );
  await page.locator('#memory-object-types').evaluate((el, map) => {
    el.textContent = JSON.stringify(map);
  }, types);
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  return errors;
}

async function attach(page: Page, items: SourceItem[]) {
  await page.evaluate((citations: SourceItem[]) => {
    const mc = (window as unknown as {
      MemoryChatComponents: { attachSources: (wrap: Element, list: unknown[]) => void };
    }).MemoryChatComponents;
    const wrap = document.createElement('div');
    wrap.className = 'chat chat-start memory-assistant';
    wrap.innerHTML = '<div class="memory-md">answer</div>';
    document.getElementById('chat-messages')?.appendChild(wrap);
    mc.attachSources(wrap, citations);
  }, items);
}

test.describe('Chat sources — relationship context and type label', () => {
  test('a relationship row keeps the fragment in its href and prefers type_label', async ({ page }) => {
    const errors = await load(page, { works_at: { label: 'Works at', color: '#0EA5E9' } });
    await attach(page, [
      {
        kind: 'relationship',
        id: REL,
        type: 'works_at',
        type_label: 'Employed by',
        type_color: '#16A34A',
        label: 'Acme —works at→ Bob',
        url: `/objects/${SOURCE}#relationship-${REL}`,
      },
    ]);

    const row = page.getByTestId('chat-source').first();
    await expect(row.locator('a')).toHaveAttribute(
      'href',
      new RegExp(`/objects/${SOURCE}#relationship-${REL}$`),
    );
    // Server label wins over the embedded map label ("Works at").
    await expect(row.locator('[data-testid=chat-source-type]')).toHaveText('Employed by');
    await expect(row.locator('[data-testid=chat-source-type]')).toHaveAttribute('style', /#16A34A/i);
    expect(errors).toEqual([]);
  });

  test('a relationship with no trusted url stays inert rather than linking its id', async ({ page }) => {
    const errors = await load(page, {});
    await attach(page, [
      { kind: 'relationship', id: REL, type: 'works_at', label: 'Acme —works at→ Bob' },
    ]);

    await expect(page.getByTestId('chat-source')).toHaveCount(1);
    await expect(page.getByTestId('chat-source').first().locator('a')).toHaveCount(0);
    expect(errors).toEqual([]);
  });

  test('a synthesized non-UUID relationship id keeps its source-object link', async ({ page }) => {
    const errors = await load(page, {});
    await attach(page, [
      {
        kind: 'relationship',
        id: `${SOURCE}:works_at:22222222-2222-4222-8222-222222222222`,
        type: 'works_at',
        label: 'Acme —works at→ Bob',
        url: `/objects/${SOURCE}`,
      },
    ]);

    // buildCitation omits the fragment for a synthesized id (it names no real
    // edge), so the url is the plain source page and the row still links there.
    await expect(page.getByTestId('chat-source').first().locator('a')).toHaveAttribute(
      'href',
      new RegExp(`/objects/${SOURCE}$`),
    );
    expect(errors).toEqual([]);
  });

  test('an older citation without type_label still uses the embedded map', async ({ page }) => {
    const errors = await load(page, { person: { label: 'Person', color: '#4F46E5' } });
    await attach(page, [
      { kind: 'object', id: SOURCE, type: 'person', label: 'Ada', url: `/objects/${SOURCE}` },
    ]);

    await expect(page.getByTestId('chat-source').first().locator('[data-testid=chat-source-type]')).toHaveText(
      'Person',
    );
    expect(errors).toEqual([]);
  });
});
