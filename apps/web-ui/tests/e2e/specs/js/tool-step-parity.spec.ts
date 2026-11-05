import { test, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Cross-surface parity: the recorded-session page (Go templ) and the chat chip
// (JS) must describe a tool call with the same verb, subject, outcome and body.
//
// Both sides read one file — `gateway/toolresult/testdata/envelope-search.step.json`
// — and the parser test (`toolresult/contract_test.go`) is the only place it is
// compared against what `toolresult` produces. The templ side of this pair lives
// in `gateway/sessions_test.go` (`TestRenderSessionToolStep`), reading the same
// file. Change a wording rule and the parser test fails first; then both surfaces
// pick the new line up from the contract instead of drifting apart.
const STEP_CONTRACT = path.resolve(
  __dirname,
  '../../../../gateway/toolresult/testdata/envelope-search.step.json',
);
const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);
const CHAT_STREAM_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-stream.js',
);

interface StepContract {
  tool: string;
  category: { family: string; verb: string; runningVerb: string; icon: string };
  subject: string;
  view: {
    kind: string;
    outcome: string;
    items?: { title: string; detail?: string }[];
    raw: string;
  };
}

function readContract(): StepContract {
  return JSON.parse(fs.readFileSync(STEP_CONTRACT, 'utf8')) as StepContract;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent('<!doctype html><html><body><div id="d"></div></body></html>');
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  return errors;
}

test.describe('tool step parity', () => {
  test('the chip names the call the way the contract names it', async ({ page }) => {
    const errors = await bootstrap(page);
    const contract = readContract();

    const chip = await page.evaluate((c) => {
      const w = window as unknown as {
        MemoryChatComponents: {
          toolLabel: (cat: unknown, name: string, running: boolean, subject: string) => string;
          toolIcon: (cat: unknown) => string;
        };
      };
      return {
        label: w.MemoryChatComponents.toolLabel(c.category, c.tool, false, c.subject),
        running: w.MemoryChatComponents.toolLabel(c.category, c.tool, true, c.subject),
        icon: w.MemoryChatComponents.toolIcon(c.category),
      };
    }, contract);

    expect(chip.label).toBe(`${contract.category.verb} · ${contract.subject}`);
    expect(chip.running).toBe(`${contract.category.runningVerb} · ${contract.subject}`);
    expect(chip.icon).toBe(contract.category.icon);
    expect(errors).toEqual([]);
  });

  test('the opened chip shows the contract body and keeps the payload closed', async ({ page }) => {
    await bootstrap(page);
    const contract = readContract();

    const step = await page.evaluate((c) => {
      const w = window as unknown as {
        MemoryChatComponents: { renderToolDetails: (el: Element, p: unknown) => void };
      };
      const el = document.getElementById('d');
      if (!el) throw new Error('missing #d mount');
      w.MemoryChatComponents.renderToolDetails(el, c);

      const details = el.querySelector('details.memory-raw-disclosure') as HTMLDetailsElement | null;
      const bodyText = Array.from(el.childNodes)
        .filter((node) => node.nodeType !== Node.ELEMENT_NODE || !(node as Element).matches('details'))
        .map((node) => node.textContent || '')
        .join('');
      return {
        items: Array.from(el.querySelectorAll('section li')).map((li) => li.textContent || ''),
        bodyText,
        details: !!details,
        open: details ? details.open : null,
        rawText: details?.querySelector('pre')?.textContent || '',
        disclosureText: details ? details.textContent || '' : '',
      };
    }, contract);

    const items = contract.view.items || [];
    expect(items.length).toBeGreaterThan(0);
    expect(step.items).toHaveLength(items.length);
    items.forEach((item, i) => {
      expect(step.items[i]).toContain(item.title);
      if (item.detail) expect(step.items[i]).toContain(item.detail);
    });

    // The payload is behind a closed disclosure, not in the body.
    expect(step.details).toBe(true);
    expect(step.open).toBe(false);
    expect(step.rawText).toBe(contract.view.raw);
    expect(step.disclosureText).toContain(contract.tool);
    expect(step.bodyText).not.toContain(contract.view.raw);
  });

  test('a live chip settles on the contract outcome', async ({ page }) => {
    const errors = await bootstrap(page);
    const contract = readContract();

    const secondary = await page.evaluate((c) => {
      const stream = document.createElement('div');
      stream.id = 'stream';
      document.body.appendChild(stream);
      const w = window as unknown as {
        MemoryChatStream: { createEngine: (ctx: unknown) => { handleToolEvent: (e: unknown) => void } };
      };
      const engine = w.MemoryChatStream.createEngine({
        messages: stream,
        badgeCtx: { messages: stream },
        scrollToBottom: () => {},
        hideEmpty: () => {},
        setStreaming: () => {},
        onStreamFail: () => {},
      });
      engine.handleToolEvent({
        tool: c.tool,
        id: 'call-1',
        status: 'completed',
        result: { ok: true },
        toolCategory: c.category,
        toolView: c.view,
      });
      const chips = Array.from(document.querySelectorAll('.memory-tool-chip'));
      const chip = chips[chips.length - 1];
      if (!chip) return null;
      const note = chip.querySelector('.memory-badge-secondary');
      return {
        label: chip.querySelector('.memory-badge-label')?.textContent || '',
        secondary:
          note && !note.classList.contains('hidden') ? note.textContent || '' : '',
      };
    }, contract);

    expect(errors).toEqual([]);
    expect(secondary?.secondary).toBe(contract.view.outcome);
    // The terminal frame carries the view and no subject; the chip still reads the
    // call by its verb, so a settled step never falls back to the wire name.
    expect(secondary?.label).toBe(contract.category.verb);
  });
});
