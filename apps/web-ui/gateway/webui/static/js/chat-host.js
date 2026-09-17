/* Memory web UI — shared chat host glue.
   Loaded by BOTH the /chat page (chat.js) and the global side panel
   (sidepanel.js). The streaming engine (chat-stream.js) and component
   builders (chat-components.js) are already shared; this module holds the
   host-page plumbing the two surfaces used to duplicate: composer helpers,
   the drag-resize width subsystem, the timeline sort, the SSE dispatcher, and
   the gateway POST helper. Each surface passes its own element/state accessors
   and page-local callbacks, so nothing shared is mutable. */
(function () {
  "use strict";

  /* ---------- pure helpers ---------- */

  // Stable-sort a server timeline by effective time, then step_number (a
  // per-run counter that can't order across runs on its own). run_end items
  // carry the run's start time in created_at and the real end time in
  // completed_at — sort by the latter so "Run complete" lands after the run's
  // content instead of right after "Run started".
  function sortTimeline(items) {
    return (items || []).slice().sort(function (a, b) {
      var ta = itemTime(a);
      var tb = itemTime(b);
      if (ta !== tb) return ta - tb;
      return ((a && a.step_number) || 0) - ((b && b.step_number) || 0);
    });
  }

  function itemTime(it) {
    if (it && it.kind === "run_end" && it.completed_at) {
      var t = new Date(it.completed_at).getTime();
      if (!isNaN(t)) return t;
    }
    if (it && it.created_at) {
      var t = new Date(it.created_at).getTime();
      if (!isNaN(t)) return t;
    }
    return 0;
  }

  // True for failures expected during a brief memory outage / restart, which
  // should not be reported to Sentry: fetch transport failures (fetch rejects
  // with a TypeError, e.g. "Failed to fetch") and gateway-unavailable
  // responses (502/503/504 — either attached as err.status by the caller or
  // thrown as new Error("HTTP " + status), e.g. the gateway's
  // {"error":"memory service unavailable"} 502 body). Client errors (4xx) and
  // logic bugs (other error types/messages) remain reportable. Shared by
  // chat.js and app.js via window.MemoryChatHost.
  function isTransientError(err) {
    if (!err) return false;
    // Explicit marker callers set for a known transport failure (e.g. the
    // gateway POST helper's network flag). Preferred over message heuristics:
    // it does not depend on browser/vendor error wording.
    if (err.transient === true) return true;
    // A user-initiated abort (stop button, navigating away, superseding a
    // request) is deliberate, not a bug worth reporting.
    if (err.name === "AbortError") return true;
    if (err.name === "TypeError" && /failed to fetch|failed fetch|network\s?error|network request failed|load failed|the network connection was lost/i.test(String(err.message || ""))) return true;
    if (err.status === 502 || err.status === 503 || err.status === 504) return true;
    // Only a bare "HTTP 502" (exactly what callers throw) counts — anchoring
    // avoids matching unrelated messages that merely mention an HTTP status.
    var m = /^HTTP\s+(\d{3})$/.exec(String(err.message || ""));
    if (!m) return false;
    var code = parseInt(m[1], 10);
    return code === 502 || code === 503 || code === 504;
  }

  // Copy text to the clipboard using the same pattern as the API-token copy
  // script (api_tokens.templ): navigator.clipboard when available, otherwise a
  // hidden textarea + execCommand fallback for older/insecure contexts. A
  // rejected clipboard write (permission denied / clipboard unavailable) also
  // falls back to the textarea path rather than silently swallowing the error.
  function copyTextFallback(value) {
    var ta = document.createElement("textarea");
    ta.value = value;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "-1000px";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); } catch (e) {}
    document.body.removeChild(ta);
  }

  function copyText(text) {
    var value = text == null ? "" : String(text);
    if (navigator.clipboard && navigator.clipboard.writeText) {
      try {
        var p = navigator.clipboard.writeText(value);
        if (p && typeof p.then === "function") {
          // Only skip the fallback after a successful write: a rejected promise
          // (permission denied / clipboard unavailable) must still fall back to
          // the textarea + execCommand path.
          p.then(
            function () {},
            function () { copyTextFallback(value); }
          );
        }
        return;
      } catch (e) { /* fall through to the textarea fallback */ }
    }
    copyTextFallback(value);
  }

  // formatDuration renders a millisecond span as a compact wall-clock string
  // ("0:07", "1:32", "1:02:03") for turn footers. Null/invalid → "".
  function formatDuration(ms) {
    if (typeof ms !== "number" || !isFinite(ms) || ms < 0) return "";
    var total = Math.round(ms / 1000);
    var h = Math.floor(total / 3600);
    var m = Math.floor((total % 3600) / 60);
    var s = total % 60;
    var pad = function (n) { return n < 10 ? "0" + n : String(n); };
    if (h > 0) return h + ":" + pad(m) + ":" + pad(s);
    return m + ":" + pad(s);
  }

  // POST a JSON body to a gateway endpoint and resolve {ok, data}. Network
  // failures and non-ok JSON responses resolve {ok:false, error:message}; the
  // `network` flag distinguishes a transport failure (for captureError) from an
  // HTTP/gateway rejection.
  async function postJSON(path, payload) {
    var res;
    try {
      res = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: payload === undefined ? undefined : JSON.stringify(payload),
      });
    } catch (err) {
      return { ok: false, error: err.message, network: true };
    }
    var data = null;
    try { data = await res.json(); } catch (e) {}
    if (!res.ok || !data || data.ok !== true) {
      return { ok: false, error: (data && data.error) || ("Gateway error " + res.status), network: false };
    }
    return { ok: true, data: data };
  }

  /* ---------- composer helpers ---------- */

  // scrollToBottom / hideEmpty / setStreaming, bound to a surface's element and
  // state accessors. The getters return the live element so they survive the
  // shell re-rendering #chat-root / #sidepanel-panel on HTMX swaps.
  function createComposer(h) {
    // h = { getLog, getEmpty, getSendBtn, getStopBtn, getStreaming, setStreaming }
    function scrollToBottom(force) {
      var log = h.getLog();
      if (!log) return;
      var nearBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 90;
      if (force || nearBottom) log.scrollTop = log.scrollHeight;
    }
    function hideEmpty() {
      var empty = h.getEmpty();
      if (empty) empty.classList.add("hidden");
    }
    function setStreaming(on) {
      h.setStreaming(on);
      var sendBtn = h.getSendBtn();
      var stopBtn = h.getStopBtn();
      if (sendBtn) {
        sendBtn.classList.toggle("hidden", on);
        sendBtn.disabled = on;
      }
      if (stopBtn) stopBtn.classList.toggle("hidden", !on);
    }
    return { scrollToBottom: scrollToBottom, hideEmpty: hideEmpty, setStreaming: setStreaming };
  }

  /* ---------- drag-to-resize width subsystem ---------- */

  function createResizeGrip(cfg) {
    // cfg = {
    //   handle, el,
    //   storageKey, defaultWidth, minWidth,
    //   maxWidthFn(),  // -> px upper bound
    //   direction,     // 1 = right edge (drag right widens); -1 = left edge
    //   apply(width),  // -> void, called after every width change
    //   onWidthSet(),  // optional, called after width changes + on init
    // }
    var width = cfg.defaultWidth;

    function maxW() { return cfg.maxWidthFn(); }

    function apply() { cfg.apply(width); }

    function persist() {
      try { localStorage.setItem(cfg.storageKey, String(width)); } catch (e) {}
    }

    function setWidth(w) {
      width = Math.max(cfg.minWidth, Math.min(w, maxW()));
      apply();
      if (cfg.onWidthSet) cfg.onWidthSet();
    }

    function init() {
      var stored = null;
      try { stored = parseInt(localStorage.getItem(cfg.storageKey), 10); } catch (e) {}
      width = (stored && stored > 0) ? stored : cfg.defaultWidth;
      width = Math.max(cfg.minWidth, Math.min(width, maxW()));
      apply();
      if (cfg.onWidthSet) cfg.onWidthSet();
    }

    // Drag the grip. Pointer capture keeps move/up events flowing even when the
    // cursor leaves the handle.
    function onPointerDown(e) {
      if (e.button !== 0) return;
      // The configured handle can be null when the grip was re-rendered away
      // between binding and the drag (shell HTMX swaps), or when the caller
      // wired the listener before assigning .handle. Fall back to the event
      // target so the drag still works, and bail safely if neither exists
      // instead of dereferencing null (MEMORY-UI-F/R).
      var handle = cfg.handle || e.currentTarget;
      if (!handle) return;
      // capture the element reference; e.currentTarget is only valid during dispatch
      e.preventDefault();
      var startX = e.clientX;
      var startW = width;
      var limit = maxW();
      if (handle.setPointerCapture) {
        try { handle.setPointerCapture(e.pointerId); } catch (err) {}
      }
      function move(ev) {
        var w = startW + cfg.direction * (ev.clientX - startX);
        width = Math.max(cfg.minWidth, Math.min(w, limit));
        apply();
        if (cfg.onWidthSet) cfg.onWidthSet();
      }
      function up() {
        handle.removeEventListener("pointermove", move);
        handle.removeEventListener("pointerup", up);
        handle.removeEventListener("pointercancel", up);
        persist();
      }
      handle.addEventListener("pointermove", move);
      handle.addEventListener("pointerup", up);
      handle.addEventListener("pointercancel", up);
    }

    // Viewport shrunk below the current width (or grew past md): re-clamp.
    function onWindowResize() {
      if (window.innerWidth < 768) { apply(); return; }
      if (width > maxW()) {
        width = Math.max(cfg.minWidth, maxW());
        persist();
      }
      apply();
    }

    return {
      init: init,
      onPointerDown: onPointerDown,
      onWindowResize: onWindowResize,
      getWidth: function () { return width; },
      setWidth: setWidth,
      // Callers assign `.handle` after init() resolves the DOM node; route that
      // write through to cfg.handle so onPointerDown/up see the live element
      // (the returned object itself does not otherwise expose the handle).
      get handle() { return cfg.handle; },
      set handle(v) { cfg.handle = v; },
    };
  }

  /* ---------- SSE dispatcher ---------- */

  // Shared handleEvent switch. The meta frame and (chat-only) thinking frame
  // are page-local; the rest is identical across surfaces.
  function makeHandleEvent(h) {
    // h = {
    //   getStreaming, setBubbleHTML, updateBubbleText, scrollToBottom,
    //   handleToolEvent, renderQuestion, renderApproval, failStream,
    //   onMeta(evt),     // page-local meta handling (required)
    //   onThinking(evt), // optional (chat only)
    //   onToken(evt),    // optional (raw delta append; hosts without it skip)
    //   isAborted(),     // optional (sidepanel drops frames from aborted streams)
    // }
    return function (raw) {
      var evt;
      try { evt = JSON.parse(raw); } catch (e) { return; }
      if (h.isAborted && h.isAborted()) return;
      switch (evt.type) {
        case "meta":
          h.onMeta(evt);
          break;
        case "token":
          if (h.onToken) h.onToken(evt);
          break;
        case "html":
          h.setBubbleHTML(evt.html || "");
          if (h.getStreaming()) h.updateBubbleText();
          h.scrollToBottom();
          break;
        case "mcp_tool":
          h.handleToolEvent(evt);
          break;
        case "thinking":
          if (h.onThinking) h.onThinking(evt);
          break;
        case "question":
          if (evt.questionId) h.renderQuestion(evt);
          break;
        case "approval":
          if (evt.questionId) h.renderApproval(evt);
          break;
        case "error":
          h.failStream(evt.error || "The agent hit an error.");
          break;
        case "done":
          break;
      }
    };
  }

  window.MemoryChatHost = {
    sortTimeline: sortTimeline,
    isTransientError: isTransientError,
    copyText: copyText,
    formatDuration: formatDuration,
    postJSON: postJSON,
    createComposer: createComposer,
    createResizeGrip: createResizeGrip,
    makeHandleEvent: makeHandleEvent,
  };
})();
