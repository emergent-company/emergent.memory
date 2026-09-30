import { test, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic DOM-level spec for the chat-stream.js → chat-transport.js wiring
// (Step 3 of the chat-client consolidation, #1286). Loads the two shipped files
// verbatim into a bare page and drives the real `MemoryChatStream.createEngine`
// streamChat path with a stubbed fetch — no gateway, no memory API, no auth.
//
// It proves three things a `node --check` cannot:
//   1. streamChat goes through `MemoryChatTransport.streamSSE` (spy).
//   2. the shared parser buffers a frame split mid-`data:` line across chunks.
//   3. the engine's stop-guard is wired as the transport's `isStopped`, so an
//      `error` frame's terminal state is not overwritten by a trailing "done".
// A fourth, static check pins that chat-stream.js no longer carries its own SSE
// read loop (the whole point of the routing change).

const TRANSPORT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-transport.js',
);
const STREAM_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-stream.js',
);

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent('<!doctype html><html><body></body></html>');
  // Transport first: chat-stream.js resolves it at call time.
  await page.addScriptTag({ path: TRANSPORT_JS });
  await page.addScriptTag({ path: STREAM_JS });
  return errors;
}

test('chat-stream.js no longer carries its own SSE read loop', () => {
  const src = fs.readFileSync(STREAM_JS, 'utf8');
  expect(src).not.toContain('getReader(');
  expect(src).not.toContain('new TextDecoder');
});

test('streamChat routes frames through the shared transport and buffers split chunks', async ({
  page,
}) => {
  const errors = await bootstrap(page);

  const result = await page.evaluate(async () => {
    const w = window as unknown as {
      MemoryChatTransport: {
        streamSSE: (url: string, opts: Record<string, unknown>) => Promise<void>;
      };
      MemoryChatStream: {
        createEngine: (ctx: Record<string, unknown>) => {
          streamChat: (agent: string, text: string) => Promise<void>;
        };
      };
      fetch: typeof fetch;
    };

    const seen: unknown[] = [];
    const argTypes: string[] = [];
    const finishes: string[] = [];
    let calls = 0;
    let url = '';

    // Spy on the shared transport, then delegate so the real wire loop runs.
    const realStreamSSE = w.MemoryChatTransport.streamSSE;
    w.MemoryChatTransport.streamSSE = (u: string, opts: Record<string, unknown>) => {
      calls += 1;
      url = u;
      return realStreamSSE(u, opts);
    };

    // Two frames, each split mid-`data:` line across chunks. A naive per-chunk
    // parse would drop both; only the shared parser's partial-buffer carry can
    // reassemble them.
    const chunks = [
      'data: {"type":"meta","sessionId":"s1"}\ndata: {"type":"tok',
      'en","delta":"hi"}\n\n',
    ].map((f) => new TextEncoder().encode(f));

    const realFetch = w.fetch.bind(w);
    w.fetch = (() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({}),
        body: new ReadableStream<Uint8Array>({
          start(c) {
            for (const chunk of chunks) c.enqueue(chunk);
            c.close();
          },
        }),
      })) as unknown as typeof fetch;

    const ctx: Record<string, unknown> = {
      streaming: false,
      bubble: null,
      bubbleHTML: '',
      bubbleText: '',
      onStreamStart: () => {},
      onStreamFinish: (r: string) => finishes.push(r),
      setStreaming: () => {},
      scrollToBottom: () => {},
      handleEvent: (raw: string) => {
        argTypes.push(typeof raw);
        seen.push(JSON.parse(raw));
      },
    };
    const engine = w.MemoryChatStream.createEngine(ctx);
    await engine.streamChat('a1', 'hello');

    w.fetch = realFetch;
    w.MemoryChatTransport.streamSSE = realStreamSSE;
    return { calls, url, seen, argTypes, finishes };
  });

  expect(result.calls).toBe(1);
  expect(result.url).toBe('/api/chat');
  expect(result.seen).toEqual([
    { type: 'meta', sessionId: 's1' },
    { type: 'token', delta: 'hi' },
  ]);
  // handleEvent's historical contract is the raw JSON string.
  expect(result.argTypes).toEqual(['string', 'string']);
  expect(result.finishes).toEqual(['done']);
  expect(errors).toEqual([]);
});

test('an error frame suppresses the transport trailing done (stop-guard wired)', async ({
  page,
}) => {
  const errors = await bootstrap(page);

  const result = await page.evaluate(async () => {
    const w = window as unknown as {
      MemoryChatStream: {
        createEngine: (ctx: Record<string, unknown>) => {
          streamChat: (agent: string, text: string) => Promise<void>;
          failStream: (msg: string) => void;
        };
      };
      fetch: typeof fetch;
    };

    const finishes: string[] = [];
    const fails: string[] = [];
    let engine: {
      streamChat: (agent: string, text: string) => Promise<void>;
      failStream: (msg: string) => void;
    };

    const realFetch = w.fetch.bind(w);
    w.fetch = (() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({}),
        body: new ReadableStream<Uint8Array>({
          start(c) {
            c.enqueue(new TextEncoder().encode('data: {"type":"error","error":"boom"}\n\n'));
            c.close();
          },
        }),
      })) as unknown as typeof fetch;

    const ctx: Record<string, unknown> = {
      streaming: true,
      bubble: null,
      bubbleHTML: '',
      bubbleText: '',
      onStreamStart: () => {},
      onStreamFinish: (r: string) => finishes.push(r),
      onStreamFail: () => fails.push('fail'),
      setStreaming: () => {},
      scrollToBottom: () => {},
      // Mirror chat-host.js's dispatcher: an error frame terminalises the turn.
      handleEvent: (raw: string) => {
        const evt = JSON.parse(raw) as { type?: string; error?: string };
        if (evt.type === 'error') engine.failStream(evt.error || 'error');
      },
    };
    engine = w.MemoryChatStream.createEngine(ctx);
    await engine.streamChat('a1', 'x');

    w.fetch = realFetch;
    return { finishes, fails };
  });

  expect(result.fails).toEqual(['fail']);
  expect(result.finishes).toEqual([]);
  expect(errors).toEqual([]);
});

test('an aborted request finalises with the aborted reason', async ({ page }) => {
  const errors = await bootstrap(page);

  const result = await page.evaluate(async () => {
    const w = window as unknown as {
      MemoryChatStream: {
        createEngine: (ctx: Record<string, unknown>) => {
          streamChat: (agent: string, text: string) => Promise<void>;
        };
      };
      fetch: typeof fetch;
    };

    const finishes: string[] = [];
    let aborter: AbortController | null = null;

    const realFetch = w.fetch.bind(w);
    // Reject like a real aborted fetch once the engine's signal fires.
    w.fetch = ((_url: string, init: RequestInit) =>
      new Promise((_resolve, reject) => {
        const signal = init.signal;
        if (!signal) return;
        signal.addEventListener('abort', () => {
          const e = new Error('aborted');
          e.name = 'AbortError';
          reject(e);
        });
      })) as unknown as typeof fetch;

    const ctx: Record<string, unknown> = {
      streaming: true,
      bubble: null,
      bubbleHTML: '',
      bubbleText: '',
      onStreamStart: () => {},
      onStreamFinish: (r: string) => finishes.push(r),
      setStreaming: () => {},
      scrollToBottom: () => {},
      handleEvent: () => {},
    };
    const engine = w.MemoryChatStream.createEngine(ctx);
    const pending = engine.streamChat('a1', 'x');
    // streamChat assigns ctx.aborter synchronously before awaiting the transport.
    aborter = (ctx.aborter as AbortController) || aborter;
    aborter?.abort();
    await pending;

    w.fetch = realFetch;
    return { finishes };
  });

  expect(result.finishes).toEqual(['aborted']);
  expect(errors).toEqual([]);
});
