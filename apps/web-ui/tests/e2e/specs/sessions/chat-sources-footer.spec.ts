import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import { cleanupObjects, memoryAuthHeaders } from '../../helpers/objects';
import { MEMORY_API_URL } from '../../helpers/tokens';

// Deterministic client-render contract for the chat Sources footer. Most tests
// need no LLM and no backend fixture: we build an assistant bubble in the page
// and hand it to the shared renderer (MemoryChatComponents.attachSources)
// exactly as the live stream and history paths do, then assert the footer
// contract (in-bubble, collapsed by default, expand, row testids, link safety,
// human type labels). The compiled-type map the shell normally embeds is seeded
// in-page so the schema-label path (map label replaces the raw type) is
// exercised, not just the humanizer fallback.
//
// One test also exercises the relationship preview end-to-end: it creates two
// scratch objects + an edge through the memory API, mounts a relationship
// citation exactly as the stream now carries it (`/objects/<src>#relationship-
// <rel>`), and asserts the drawer renders "Referenced via". The scratch graph
// rows are deleted in `finally`, pass or fail.
const UUID_A = '11111111-1111-4111-8111-111111111111';
const UUID_B = '22222222-2222-4222-8222-222222222222';

type SourceItem = {
  kind: string;
  id: string;
  type: string;
  label: string;
  url: string;
  type_label?: string;
  type_color?: string;
};

async function attachSources(page: import('@playwright/test').Page, items: SourceItem[]) {
  await page.waitForFunction(() => !!(window as unknown as { MemoryChatComponents?: unknown }).MemoryChatComponents);
  await page.evaluate((citations: SourceItem[]) => {
    const mc = (window as unknown as {
      MemoryChatComponents: { attachSources: (wrap: Element, list: unknown[]) => void };
    }).MemoryChatComponents;
    const wrap = document.createElement('div');
    wrap.id = 'e2e-sources-host';
    wrap.className = 'chat chat-start memory-rise memory-assistant';
    wrap.innerHTML =
      '<div class="memory-md">answer</div><div class="memory-assistant-actions"></div>';
    // Mount inside the chat list so object-preview.js scopes clicks here and
    // opens the drawer instead of navigating (its isChatObjectRef checks
    // #chat-messages / #sidepanel-messages).
    const host = document.getElementById('chat-messages') ?? document.body;
    host.appendChild(wrap);
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
    await goto(page, '/chat');
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

    const bubble = page.locator('#e2e-sources-host .memory-assistant');
    const footer = bubble.getByTestId('chat-sources');

    // The footer lives INSIDE the assistant turn — never a sibling message row.
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
    // untrusted url with a non-UUID id stays plain text (no anchor). The href is
    // scope-prefixed by the client link renderer, so match the trailing path.
    await expect(bubble.locator('[data-testid=chat-source]').nth(0).locator('a')).toHaveAttribute(
      'href',
      new RegExp(`/objects/${UUID_A}$`),
    );
    await expect(bubble.locator('[data-testid=chat-source]').nth(1).locator('a')).toHaveAttribute(
      'href',
      new RegExp(`/objects/${UUID_B}$`),
    );
    await expect(bubble.locator('[data-testid=chat-source]').nth(2).locator('a')).toHaveCount(0);

    // Expanding the disclosure reveals the rows.
    await page.getByTestId('chat-sources-toggle').click();
    expect(await footer.evaluate((el) => (el as HTMLDetailsElement).open)).toBe(true);
    await expect(page.getByTestId('chat-source').first()).toBeVisible();
  });

  test('a relationship source keeps its fragment and prefers the server type label', async ({ page }) => {
    await goto(page, '/chat');
    await expectAppPage(page, /Chat/);

    // The map label ("Works at") intentionally differs from the server-supplied
    // type_label so asserting the latter proves the wire field wins over the
    // embedded map.
    await seedObjectTypes(page, { works_at: { label: 'Works at', color: '#0EA5E9' } });

    const REL = '33333333-3333-4333-8333-333333333333';
    await attachSources(page, [
      {
        kind: 'relationship',
        id: REL,
        type: 'works_at',
        type_label: 'Employed by',
        type_color: '#16A34A',
        label: 'Acme —works at→ Bob',
        url: `/objects/${UUID_A}#relationship-${REL}`,
      },
    ]);

    const row = page.getByTestId('chat-source').first();
    // The source row link carries the relationship fragment, so object-preview
    // parseRef sees rel and opens the referenced-via preview rather than the
    // bare object page. The client scopes the path, so match the trailing path.
    await expect(row.locator('a')).toHaveAttribute(
      'href',
      new RegExp(`/objects/${UUID_A}#relationship-${REL}$`),
    );
    await expect(row.locator('[data-testid=chat-source-type]')).toHaveText('Employed by');
  });

  test('a relationship source opens the preview with "Referenced via"', async ({ page }) => {
    test.setTimeout(120_000);
    const stamp = Date.now();
    let taskId = '';
    let personId = '';

    try {
      // Scratch grammar: personal-memory's assigned_to edge is Task → Person,
      // so the Task is the relationship source whose page the citation opens.
      const headers = await memoryAuthHeaders(page);
      const createObject = async (type: string, key: string): Promise<string> => {
        const resp = await page.request.post(`${MEMORY_API_URL}/api/graph/objects`, {
          headers,
          data: { type, key, status: 'ready', properties: { title: key } },
          failOnStatusCode: false,
        });
        if (!resp.ok()) {
          throw new Error(`create ${type} failed (HTTP ${resp.status()}): ${await resp.text()}`);
        }
        const body = (await resp.json()) as { canonical_id?: string };
        if (!body.canonical_id) {
          throw new Error(`create ${type}: response missing canonical_id`);
        }
        return body.canonical_id;
      };
      taskId = await createObject('Task', `E2E Sources Rel Task ${stamp}`);
      personId = await createObject('Person', `E2E Sources Rel Person ${stamp}`);

      const relResp = await page.request.post(`${MEMORY_API_URL}/api/graph/relationships`, {
        headers,
        data: { type: 'assigned_to', src_id: taskId, dst_id: personId },
        failOnStatusCode: false,
      });
      if (!relResp.ok()) {
        throw new Error(`create relationship failed (HTTP ${relResp.status()}): ${await relResp.text()}`);
      }
      const relId = ((await relResp.json()) as { id?: string }).id;
      if (!relId) throw new Error('create relationship: response missing id');

      await goto(page, '/chat');
      await expectAppPage(page, /Chat/);

      await attachSources(page, [
        {
          kind: 'relationship',
          id: relId,
          type: 'assigned_to',
          label: 'Task —assigned to→ Person',
          url: `/objects/${taskId}#relationship-${relId}`,
        },
      ]);

      await page.getByTestId('chat-sources-toggle').click();
      await page.getByTestId('chat-source').first().locator('a').click();

      // object-preview.js passes ?rel= to the gateway, which renders the
      // relationship line from the edge it names — never a plain object drawer.
      const drawer = page.locator('#object-preview-body');
      await expect(drawer).toContainText('Referenced via');
      await expect(page).toHaveURL(/\/chat$/);
    } finally {
      await cleanupObjects(page, [taskId, personId]);
    }
  });

  test('a single citation reads "1 source"', async ({ page }) => {
    await goto(page, '/chat');
    await expectAppPage(page, /Chat/);

    await attachSources(page, [
      { kind: 'object', id: UUID_A, type: 'Zoology', label: 'Solo source', url: `/objects/${UUID_A}` },
    ]);

    await expect(page.getByTestId('chat-sources-toggle')).toHaveText(/1 source/);
    await expect(page.getByTestId('chat-source')).toHaveCount(1);
  });

  test('no citations renders no footer', async ({ page }) => {
    await goto(page, '/chat');
    await expectAppPage(page, /Chat/);

    await attachSources(page, []);

    await expect(page.getByTestId('chat-sources')).toHaveCount(0);
  });
});
