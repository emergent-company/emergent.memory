import { test, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic layout spec for #1383: the chat transcript column's width must not
// depend on how much content the session has.
//
// The transcript is a `layout.Container` (`.mx-auto …`) rendered as a flex item
// of the scroll-fill wrapper `flex min-h-full … flex-col justify-end`. A flex
// item's `mx-auto` cross-axis margins suppress flex `stretch`, so the column is
// sized shrink-to-fit: a short session collapses to the width of its longest
// bubble, while a long one grows toward the max-width cap. The fix pins the
// column to the pane with a full-width guard wrapping the Container.
//
// This spec is non-vacuous against the template: it reads `chat.templ`, detects
// whether the transcript's Container is wrapped in the `w-full` guard, and
// builds its DOM from exactly that structure. On the pre-fix template the guard
// is absent, so the skeleton reproduces the collapse and the width assertions
// fail; on the fixed template the guard is present and every session length
// measures the full pane width. No gateway, no memory API, no auth — runs in the
// js-dom gate (`npx playwright test --config=js-dom.config.ts`).

const CHAT_TEMPL = path.resolve(
  __dirname,
  '../../../../gateway/chat.templ',
);

// The scroll-fill wrapper and the wide chat Container's class, mirrored from
// chat.templ / layout.Container. The wrapper's members are already pinned by the
// Go test TestChatWorkspaceKeepsScrollFillAndSharedContainer; the Container
// class is pinned by TestChatWorkspaceKeepsScrollFillAndSharedContainer's
// chatContainerClass. Only the presence of the #1383 guard is read from source.
const WRAPPER_CLASS = 'flex min-h-full min-w-0 flex-col justify-end';
const COLUMN_CLASS = 'mx-auto p-6 lg:p-8 max-w-[100rem]';

// Tailwind/utilities the skeleton uses, inlined because the js-dom gate loads
// no compiled stylesheet. Only the rules that determine the column's box width
// matter here.
const CSS = `
  * { box-sizing: border-box; }
  html, body { margin: 0; }
  .flex { display: flex; }
  .flex-col { flex-direction: column; }
  .min-w-0 { min-width: 0; }
  .min-h-full { min-height: 100%; }
  .justify-end { justify-content: flex-end; }
  .w-full { width: 100%; }
  .mx-auto { margin-left: auto; margin-right: auto; }
  .p-6 { padding: 1.5rem; }
  .max-w-\\[100rem\\] { max-width: 100rem; }
  .gap-3 { gap: 0.75rem; }
  .chat { display: block; }
  .chat-bubble { display: block; width: fit-content; max-width: 90%; }
  .chat-bubble p { margin: 0; overflow-wrap: anywhere; }
  /* A bounded, non-scrolling transcript column: fixed width so the assertion is
     about the column filling it, and overflow hidden so a long session cannot
     introduce a scrollbar that would perturb the measured widths. */
  #chat-log { width: 1000px; height: 600px; overflow: hidden; }
`;

// Detect the #1383 guard in the real template: a `w-full` element between the
// scroll-fill wrapper and the transcript's layout.Container.
function transcriptHasFullWidthGuard(): boolean {
  const src = fs.readFileSync(CHAT_TEMPL, 'utf8');
  const start = src.indexOf('id="chat-log"');
  const end = src.indexOf('<form id="chat-form"');
  if (start < 0 || end < 0 || end <= start) {
    throw new Error('could not locate the chat transcript region in chat.templ');
  }
  const region = src.slice(start, end);
  return /class="w-full">\s*@layout\.Container\(/.test(region);
}

function chatRow(text: string): string {
  return `<div class="chat chat-end"><div class="chat-bubble chat-bubble-primary"><p>${text}</p></div></div>`;
}

// Build the transcript exactly as chat.templ renders it, including the guard
// only when the template has it. That is what makes this spec fail on the
// pre-fix template and pass on the fixed one.
function skeleton(messages: string): string {
  const guard = transcriptHasFullWidthGuard();
  return `<!doctype html>
<html><head><meta charset="utf-8" /><style>${CSS}</style></head>
<body>
  <div id="chat-log">
    <div class="${WRAPPER_CLASS}">
      ${guard ? '<div class="w-full">' : ''}
        <div id="transcript-column" class="${COLUMN_CLASS}">
          <div class="flex w-full flex-col gap-3">
            <div id="chat-messages" class="flex flex-col gap-3">${messages}</div>
          </div>
        </div>
      ${guard ? '</div>' : ''}
    </div>
  </div>
</body></html>`;
}

// The measured column width and pane width for a given message body.
async function widths(page: Page, message: string) {
  await page.setContent(skeleton(chatRow(message)));
  return page.evaluate(() => {
    const column = document.getElementById('transcript-column')!;
    const log = document.getElementById('chat-log')!;
    return {
      column: column.getBoundingClientRect().width,
      pane: log.clientWidth,
    };
  });
}

test.describe('chat transcript column width is content-independent (#1383)', () => {
  test('a very short session renders the column at the same width as a long one', async ({ page }) => {
    const short = await widths(page, 'Test');
    // A single long unbroken token: the session's content is wider than the
    // pane, so without the guard the column grows to the max-w cap.
    const long = await widths(page, 'A'.repeat(600));

    // Content-independent: short and long sessions share one column width.
    expect(
      Math.abs(short.column - long.column),
      `short=${short.column}px long=${long.column}px — the column width tracks message content`,
    ).toBeLessThanOrEqual(1);

    // And that shared width is the full transcript pane, not a content-sized
    // sliver (the failure mode in #1383: short session collapses to ~40px).
    expect(short.column).toBeGreaterThanOrEqual(short.pane - 1);
    expect(long.column).toBeGreaterThanOrEqual(long.pane - 1);

    // The template contract behind the measurement: the #1383 full-width guard
    // must be present. Assert last so the width evidence above is reported.
    expect(
      transcriptHasFullWidthGuard(),
      'chat.templ must wrap the transcript layout.Container in a `w-full` guard',
    ).toBe(true);
  });
});
