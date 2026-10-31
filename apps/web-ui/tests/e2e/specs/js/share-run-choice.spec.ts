import { test, expect } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for the public-share tool-run choice carry-over (#2323).
//
// The share SSE does not carry the turn's run id, so the live run is keyed
// "live-turn-N" while the recorded history render is keyed by run_id. These
// helpers capture the reader's open/closed state before the render and re-key
// it onto the newest history run, so the run no longer snaps shut at settle.
const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);

const LIVE_HTML = `<!doctype html><html><body>
  <div id="log">
    <details class="memory-tool-run" data-turn="live-turn-1" open>
      <summary>2 steps</summary>
    </details>
  </div>
</body></html>`;

test('a live run choice is re-keyed onto the newest history run', async ({ page }) => {
  await page.setContent(LIVE_HTML);
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });

  const out = await page.evaluate(() => {
    const components = (window as unknown as { MemoryChatComponents: any })
      .MemoryChatComponents;
    const log = document.getElementById('log')!;

    const captured = components.captureLiveToolRunChoice(log);
    // The history render replaces the live DOM with the recorded run.
    log.innerHTML =
      '<details class="memory-tool-run" data-turn="run-9"><summary>2 steps</summary></details>';
    const adopted = components.adoptNewestToolRunChoice(log, captured);

    const history = log.querySelector('details[data-turn="run-9"]')!;
    // A later run with the same key consults the remembered choice at settle.
    const rerendered = components.createToolRun(log, 'run-9', { openDefault: false });
    rerendered.addStep(document.createElement('span'), { startedAt: 1, settledAt: 2 });
    rerendered.addStep(document.createElement('span'), { startedAt: 3, settledAt: 4 });
    rerendered.finish();

    return {
      captured,
      adopted,
      historyOpen: history.hasAttribute('open'),
      rebuiltOpen: rerendered.el.hasAttribute('open'),
    };
  });

  expect(out).toEqual({
    captured: true,
    adopted: true,
    historyOpen: true,
    rebuiltOpen: true,
  });
});

test('no live run means nothing to carry over', async ({ page }) => {
  await page.setContent(`<!doctype html><html><body>
    <div id="log"><details class="memory-tool-run" data-turn="run-9"></details></div>
  </body></html>`);
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });

  const out = await page.evaluate(() => {
    const components = (window as unknown as { MemoryChatComponents: any })
      .MemoryChatComponents;
    const log = document.getElementById('log')!;
    const captured = components.captureLiveToolRunChoice(log);
    return {
      captured,
      adopted: components.adoptNewestToolRunChoice(log, captured),
    };
  });

  expect(out).toEqual({ captured: null, adopted: false });
});
