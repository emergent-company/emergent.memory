import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level spec for chat-components.js's utf8ByteLength fallback path.
//
// The helper normally uses TextEncoder; the percent-encoding branch only runs
// when TextEncoder is absent (never in the browsers/Node this app targets), so
// a bug there is invisible without forcing the branch. This spec deletes
// TextEncoder before loading the shipped file and drives the public
// renderToolDetails entry point (which surfaces the byte count in its meta
// line) to pin the arithmetic on multi-byte input.

const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);

async function bootstrapNoTextEncoder(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(
    '<!doctype html><html><body><div id="d"></div></body></html>',
  );
  // Force the fallback: utf8ByteLength only reaches encodeURIComponent when the
  // global TextEncoder is undefined.
  await page.evaluate(() => {
    delete (window as unknown as { TextEncoder?: unknown }).TextEncoder;
  });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  return errors;
}

// Renders a tool detail with a string output and returns the response-size meta
// line, e.g. "2 B response". outputByteSize feeds the raw string through
// utf8ByteLength.
async function responseMetaLine(page: Page, output: string): Promise<string> {
  return page.evaluate((out) => {
    const w = window as unknown as {
      MemoryChatComponents: {
        renderToolDetails: (el: Element, p: unknown) => void;
      };
    };
    const el = document.getElementById('d');
    if (!el) throw new Error('missing #d mount');
    w.MemoryChatComponents.renderToolDetails(el, { output: out });
    const meta = el.querySelector('p.font-mono');
    return meta ? meta.textContent || '' : '';
  }, output);
}

test.describe('chat-components.js utf8ByteLength fallback', () => {
  test('counts multi-byte characters as their UTF-8 byte length', async ({ page }) => {
    const errors = await bootstrapNoTextEncoder(page);

    // "é" is 2 UTF-8 bytes but the percent-encoding "%C3%A9" is 6 chars wide;
    // over-counting each escape by 1 would report 4 B.
    expect(await responseMetaLine(page, 'é')).toContain('2 B response');
    // Emoji is 4 bytes ("%F0%9F%98%80" → over-counted to 8 B).
    expect(await responseMetaLine(page, '😀')).toContain('4 B response');
    // ASCII has no escapes: one byte per character.
    expect(await responseMetaLine(page, 'abc')).toContain('3 B response');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
