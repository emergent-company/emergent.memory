import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level spec for the shared SSE wire transport
// (webui/static/js/chat-transport.js). Loads the shipped file verbatim into a
// bare page (page.addScriptTag) and drives parseSSE (pure) + streamSSE (stubbed
// fetch + ReadableStream) in a real Chromium engine — no gateway, no memory
// API, no Zitadel, no `.env.e2e`. Runs in CI (js-dom.config.ts).

const TRANSPORT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-transport.js',
);

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><body></body></html>',
  );
  await page.addScriptTag({ path: TRANSPORT_JS });
  return errors;
}

// parse returns the { frames, rest } result of MemoryChatTransport.parseSSE for
// the given (buf, acc) pair, evaluated inside the page.
async function parse(page: Page, buf: string, acc: string) {
  return page.evaluate(
    ({ buf, acc }) => {
      const w = window as unknown as {
        MemoryChatTransport: {
          parseSSE: (b: string, a: string) => { frames: string[]; rest: string };
        };
      };
      return w.MemoryChatTransport.parseSSE(buf, acc);
    },
    { buf, acc },
  );
}

test.describe('chat-transport.js parseSSE', () => {
  test('splits a complete data: frame', async ({ page }) => {
    const errors = await bootstrap(page);
    const out = await parse(page, 'data: {"a":1}\n\n', '');
    expect(out).toEqual({ frames: ['{"a":1}'], rest: '' });
    expect(errors).toEqual([]);
  });

  test('carries a chunk split mid-line across calls', async ({ page }) => {
    const errors = await bootstrap(page);
    const first = await parse(page, 'data: {"a"', '');
    expect(first).toEqual({ frames: [], rest: 'data: {"a"' });
    const second = await parse(page, ': 1}\n\n', first.rest);
    expect(second).toEqual({ frames: ['{"a": 1}'], rest: '' });
    expect(errors).toEqual([]);
  });

  test('handles CRLF line endings', async ({ page }) => {
    const errors = await bootstrap(page);
    const out = await parse(page, 'data: {"a":1}\r\n\r\n', '');
    expect(out).toEqual({ frames: ['{"a":1}'], rest: '' });
    expect(errors).toEqual([]);
  });

  test('extracts multiple frames from one buffer', async ({ page }) => {
    const errors = await bootstrap(page);
    const out = await parse(page, 'data: {"a":1}\n\ndata: {"b":2}\n\n', '');
    expect(out).toEqual({ frames: ['{"a":1}', '{"b":2}'], rest: '' });
    expect(errors).toEqual([]);
  });

  test('returns a trailing partial in rest', async ({ page }) => {
    const errors = await bootstrap(page);
    const out = await parse(page, 'data: {"a":1}\n\ndata: {"b"', '');
    expect(out).toEqual({ frames: ['{"a":1}'], rest: 'data: {"b"' });
    expect(errors).toEqual([]);
  });

  test('ignores non-data: lines', async ({ page }) => {
    const errors = await bootstrap(page);
    const out = await parse(
      page,
      'event: message\ndata: {"a":1}\n\n: comment\nid: 3\n\n',
      '',
    );
    expect(out).toEqual({ frames: ['{"a":1}'], rest: '' });
    expect(errors).toEqual([]);
  });
});

test.describe('chat-transport.js streamSSE', () => {
  test('POSTs the body/credentials and routes each data: frame through onFrame', async ({ page }) => {
    const errors = await bootstrap(page);

    const result = await page.evaluate(async () => {
      const w = window as unknown as {
        MemoryChatTransport: {
          streamSSE: (
            url: string,
            opts: {
              signal: AbortSignal;
              body: string;
              credentials?: string;
              onFrame: (f: unknown) => void;
              onFinish: (r: string) => void;
              headers?: Record<string, string>;
            },
          ) => Promise<void>;
        };
      };

      const captured: { url: string; init: Record<string, unknown> } = {
        url: '',
        init: {},
      };
      const frames: unknown[] = [];
      const finishes: string[] = [];

      const chunks = [
        new TextEncoder().encode('data: {"type":"meta","sessionId":"s1"}\n\n'),
        new TextEncoder().encode('data: {"type":"token","delta":"hi"}\n'),
        new TextEncoder().encode('data: {"type":"done"}\n\n'),
      ];

      const realFetch = w.fetch.bind(w);
      w.fetch = ((url: string, init: Record<string, unknown>) => {
        captured.url = url;
        captured.init = init;
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            for (const c of chunks) controller.enqueue(c);
            controller.close();
          },
        });
        return Promise.resolve({ ok: true, status: 200, body: stream });
      }) as typeof fetch;

      await w.MemoryChatTransport.streamSSE('/test/chat', {
        body: JSON.stringify({ message: 'hello', sessionId: 's9' }),
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
        signal: new AbortController().signal,
        onFrame: (f) => frames.push(f),
        onFinish: (r) => finishes.push(r),
      });

      w.fetch = realFetch;
      const h = captured.init.headers as Record<string, string>;
      return {
        url: captured.url,
        method: captured.init.method,
        credentials: captured.init.credentials,
        body: captured.init.body,
        contentType: h['Content-Type'],
        frames,
        finishes,
      };
    });

    expect(result.url).toBe('/test/chat');
    expect(result.method).toBe('POST');
    expect(result.credentials).toBe('include');
    expect(result.body).toBe(JSON.stringify({ message: 'hello', sessionId: 's9' }));
    expect(result.contentType).toBe('application/json');
    expect(result.frames).toEqual([
      { type: 'meta', sessionId: 's1' },
      { type: 'token', delta: 'hi' },
      { type: 'done' },
    ]);
    expect(result.finishes).toEqual(['done']);
    expect(errors).toEqual([]);
  });
});
