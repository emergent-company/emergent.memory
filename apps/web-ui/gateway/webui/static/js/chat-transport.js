/* Memory web UI — shared SSE chat wire transport.
 *
 * Dependency-free streaming transport extracted from the two chat clients
 * (share-agent.js and chat-stream.js) so the fetch + ReadableStream +
 * TextDecoder + `data:`-line framing + JSON.parse pipeline has exactly one
 * implementation. Exposes `window.MemoryChatTransport`:
 *
 *   parseSSE(buf, acc)          pure frame splitter
 *   streamSSE(url, opts)        fetch + stream + parse + dispatch
 *
 * Nothing here touches the DOM or any page state — callers own their message
 * stream container and page-local callbacks. Load this BEFORE any client that
 * uses it (it only assigns a global, no auto-run).
 */
(function () {
  "use strict";

  // parseSSE splits a raw decoded SSE buffer into complete `data:` frames and a
  // trailing partial. `buf` is the newly-decoded text (TextDecoder with
  // stream:true); `acc` is the unconsumed partial returned by the previous call
  // ("" on the first). Returns { frames, rest }: frames is the array of `data:`
  // payload strings from complete lines (the payload has the `data:` prefix and
  // surrounding whitespace stripped, exactly as the old inline loops did), and
  // rest is the unconsumed trailing partial (a line with no trailing newline).
  //
  // Pure: no DOM, no state. A frame split mid-chunk is simply carried in `rest`
  // until the next call completes its line; CRLF endings are absorbed by the
  // per-line trim; blank and non-`data:` lines (event:, id:, comments) are
  // ignored. Invalid JSON is NOT rejected here — JSON.parse is streamSSE's job.
  function parseSSE(buf, acc) {
    var data = (acc || "") + buf;
    var frames = [];
    var idx;
    while ((idx = data.indexOf("\n")) !== -1) {
      var line = data.slice(0, idx).trim();
      data = data.slice(idx + 1);
      if (!line) continue;
      if (line.indexOf("data:") === 0) frames.push(line.slice(5).trim());
    }
    return { frames: frames, rest: data };
  }

  // streamSSE POSTs `opts.body` to `url` and consumes the response as a
  // server-sent-event stream. For each complete frame it JSON.parses the
  // `data:` payload and calls opts.onFrame(parsedObject) (invalid-JSON frames
  // are dropped, mirroring the old handleEvent try/catch).
  //
  // Terminal signalling:
  //   - stream ends cleanly           → opts.onFinish("done")  (unless opts.isStopped())
  //   - abort (AbortError)            → opts.onFinish("aborted")
  //   - non-ok / missing body         → opts.onHTTPError(res) (its return value
  //                                      is awaited), or a thrown "Gateway error
  //                                      <status>" when absent
  //   - other fetch/stream errors     → rethrown to the caller's .catch. An error
  //                                      thrown while READING the response body is
  //                                      tagged `err.streamInterrupted = true` so a
  //                                      caller can distinguish a mid-stream drop
  //                                      from a request that never connected
  //                                      (chat-stream.js renders them differently).
  //
  // opts.isStopped() reproduces the callers' stop-guard (chat-stream.js's
  // streamFailed flag): when an `error` frame has already terminalised the
  // stream, the trailing onFinish("done") is suppressed so a success
  // finalization can't overwrite the error state.
  //
  // opts: { signal, body, credentials, onFrame, onHTTPError, isStopped, onFinish, headers }
  function streamSSE(url, opts) {
    opts = opts || {};
    return fetch(url, {
      method: "POST",
      credentials: opts.credentials,
      headers: opts.headers,
      body: opts.body,
      signal: opts.signal,
    })
      .then(function (res) {
        if (!res.ok || !res.body) {
          if (opts.onHTTPError) {
            // Await the handler's result so callers that read the error body
            // (res.json()) finish before streamSSE's promise resolves.
            return opts.onHTTPError(res);
          }
          throw new Error("Gateway error " + res.status);
        }
        var reader = res.body.getReader();
        var decoder = new TextDecoder();
        var acc = "";
        function read() {
          return reader.read().then(function (chunk) {
            if (chunk.done) {
              if (opts.isStopped && opts.isStopped()) return;
              if (opts.onFinish) opts.onFinish("done");
              return;
            }
            var text = decoder.decode(chunk.value, { stream: true });
            var parsed = parseSSE(text, acc);
            acc = parsed.rest;
            for (var i = 0; i < parsed.frames.length; i++) {
              var frame;
              try {
                frame = JSON.parse(parsed.frames[i]);
              } catch (e) {
                continue;
              }
              if (opts.onFrame) opts.onFrame(frame);
            }
            return read();
          });
        }
        // Tag errors raised while reading the body (not while fetching it) so
        // the caller can tell a mid-stream drop from a failed connection.
        return read().catch(function (err) {
          if (err && err.name !== "AbortError" && typeof err === "object") {
            err.streamInterrupted = true;
          }
          throw err;
        });
      })
      .catch(function (err) {
        if (err && err.name === "AbortError") {
          if (opts.onFinish) opts.onFinish("aborted");
          return;
        }
        throw err;
      });
  }

  window.MemoryChatTransport = {
    parseSSE: parseSSE,
    streamSSE: streamSSE,
  };
})();
