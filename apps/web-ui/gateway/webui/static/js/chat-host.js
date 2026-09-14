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
      e.preventDefault();
      var startX = e.clientX;
      var startW = width;
      var limit = maxW();
      if (cfg.handle && cfg.handle.setPointerCapture) {
        try { cfg.handle.setPointerCapture(e.pointerId); } catch (err) {}
      }
      function move(ev) {
        var w = startW + cfg.direction * (ev.clientX - startX);
        width = Math.max(cfg.minWidth, Math.min(w, limit));
        apply();
        if (cfg.onWidthSet) cfg.onWidthSet();
      }
      function up() {
        cfg.handle.removeEventListener("pointermove", move);
        cfg.handle.removeEventListener("pointerup", up);
        cfg.handle.removeEventListener("pointercancel", up);
        persist();
      }
      cfg.handle.addEventListener("pointermove", move);
      cfg.handle.addEventListener("pointerup", up);
      cfg.handle.addEventListener("pointercancel", up);
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
    postJSON: postJSON,
    createComposer: createComposer,
    createResizeGrip: createResizeGrip,
    makeHandleEvent: makeHandleEvent,
  };
})();
