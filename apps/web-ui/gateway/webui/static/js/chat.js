/* Memory web UI — chat page client.
   Streaming (fetch + ReadableStream over /api/chat SSE), conversation
   persistence (?c= URL param, conversationId threaded back into turns), and
   the resumable session rail (past conversations from /api/conversations,
   transcripts from /api/conversations/{id}/history).

   The shell re-renders #chat-root on HTMX sidebar swaps, so init() is
   re-runnable: it binds to the fresh DOM and aborts any in-flight stream. */
(function () {
  "use strict";

  /* Report an error to Sentry when the browser SDK is active (gated on
     window.Sentry, present only when the loader was injected because a
     Sentry DSN is configured). No-op otherwise. */
  function captureError(err) {
    if (window.Sentry && err) window.Sentry.captureException(err);
  }

  /* Report an unexpected failure, but stay quiet for expected transient
     memory outages (brief restart / 502-504): those still surface as an error
     toast and a local console.warn, just not as Sentry events. The
     classification lives in the shared MemoryChatHost helper (chat-host.js is
     loaded globally in ui.templ alongside this file). */
  function reportError(err, context) {
    var transient = false;
    var host = window.MemoryChatHost;
    if (host && typeof host.isTransientError === "function") {
      transient = !!host.isTransientError(err);
    }
    if (transient) {
      console.warn("chat: " + context + " (transient, not reported):", err);
      return;
    }
    captureError(err || new Error(context || "unknown error"));
  }

  var root = null, log = null, messages = null, empty = null;
  var input = null, sendBtn = null, stopBtn = null, agentSelect = null;
  var agentFilterSelect = null;
  var originFilterSelect = null;
  var filterAgent = "";   // session rail agent filter ("" = all agents)
  var filterOrigin = "";  // session rail origin filter ("" = all types)
  var aborter = null;
  var eventSource = null;   // SSE live-update channel for the active conversation
  var streaming = false;
  var bubble = null;        // current streaming assistant bubble
  var bubbleHTML = "";      // accumulated assistant rendered HTML (snapshot)
  var bubbleText = "";      // accumulated raw token deltas (pre-snapshot)
  var conversationId = "";  // threaded back into /api/chat turns
  var activeRunId = "";     // scheduled-run transcript open in the pane ("" = conversation/fresh chat)
  var transcriptRefreshPending = false; // one history re-render at a time
  var thinkingMap = {};     // live thinking segments: id -> {details, body, setLive, done}
  var thinkingOrder = [];   // thinking segment ids in insertion order

  // --- agentic run-control state ---
  var liveRunId = "";       // active run id (refresh payload runId / newest run lifecycle item)
  var liveRunStatus = "";   // active run status (submitted/working/input-required/failed/…)
  var liveBucket = "";      // derived run bucket from the refresh payload
  var railFingerprint = ""; // last refresh-derived rail state — avoids redundant rail fetches
  var lastTimelineRun = { status: "", ended: false }; // newest run in the rendered transcript
  var queueStore = {};      // conversationId -> {items:[{id,text}], armed:bool}
  var mounts = {};          // created mount points: dock, queue, todos, status
  var dockAvailable = null; // true once /partial/chat-dock answered (dock owns pending work)

  // Session rail width: single source of truth is the shared resize grip
  // (chat-host.js), applied as an inline style.width on #chat-rail (beats the
  // md:w-72 default). Dragging the right-edge grip overrides it; the value
  // survives reloads.
  var rail = null, railResizeHandle = null;
  var railGrip = MemoryChatHost.createResizeGrip({
    handle: null, // assigned in init() once #chat-rail-resize is resolved
    el: rail,
    storageKey: "memory.chat.rail.width.v1",
    defaultWidth: 288, // 18rem — matches the rail's md:w-72 default
    minWidth: 200,     // narrowest the session list can go
    maxWidthFn: function () { return Math.min(window.innerWidth - 48, 560); },
    direction: 1,
    apply: function (w) {
      if (!rail) return;
      if (window.innerWidth >= 768) rail.style.width = w + "px";
      else rail.style.width = "";
    },
  });

  // Composer helpers (scrollToBottom / hideEmpty / setStreaming) come from the
  // shared MemoryChatHost module, bound to this page's live elements. The
  // getters return the current DOM node/state, so they survive the shell
  // re-rendering #chat-root on HTMX swaps.
  var composer = MemoryChatHost.createComposer({
    getLog: function () { return log; },
    getEmpty: function () { return empty; },
    getSendBtn: function () { return sendBtn; },
    getStopBtn: function () { return stopBtn; },
    getStreaming: function () { return streaming; },
    setStreaming: function (v) { streaming = v; },
  });
  var scrollToBottom = composer.scrollToBottom;
  var hideEmpty = composer.hideEmpty;
  var setStreaming = composer.setStreaming;

  // Shared badge/thinking renderers (chat-components.js) take a context object
  // instead of reaching into this IIFE. The getter keeps the live #chat-messages
  // element; hideEmpty/scrollToBottom come from the composer above.
  var badgeCtx = {
    get messages() { return messages; },
    hideEmpty: hideEmpty,
    scrollToBottom: scrollToBottom,
  };

  // SSE dispatcher from the shared MemoryChatHost module. The meta frame is
  // page-local (rail refresh + URL), and the thinking frame is chat-only
  // (onThinking). handleThinkingEvent / railHas / updateUrl /
  // refreshSessionRail are hoisted function declarations, so forward refs are
  // fine.
  var handleEvent = MemoryChatHost.makeHandleEvent({
    getStreaming: function () { return streaming; },
    setBubbleHTML: function (v) { bubbleHTML = v; },
    updateBubbleText: function () { updateBubbleText(); },
    scrollToBottom: scrollToBottom,
    handleToolEvent: function (evt) { handleToolEvent(evt); },
    renderQuestion: function (evt) {
      // With the pending-work dock live, questions are placed there — refresh
      // it rather than dropping a second card into the transcript.
      if (dockAvailable === true) { refreshDock(); return; }
      renderQuestion(evt);
    },
    renderApproval: function (evt) {
      if (dockAvailable === true) { refreshDock(); return; }
      renderApproval(evt);
    },
    renderUI: function (evt) {
      MemoryChatComponents.renderA2UISurface(evt.surfaceId, evt.messages, badgeCtx);
    },
    failStream: function (msg) { failStream(msg); },
    onToken: function (evt) { appendToken(evt.token); },
    onThinking: handleThinkingEvent,
    onMeta: function (evt) {
      if (evt.conversationId) {
        // A brand-new conversation (id not yet in the rail) should appear
        // in the session list immediately — no full reload needed.
        var isNew = evt.conversationId !== conversationId && !railHas(evt.conversationId);
        var prevId = conversationId;
        conversationId = evt.conversationId;
        // A message parked on a brand-new (id-less) conversation moves with it
        // once the server assigns the id, so it is never stranded or delivered
        // to another conversation.
        if (!prevId && queueStore["__new__"]) {
          queueStore[conversationId] = queueStore["__new__"];
          delete queueStore["__new__"];
          try { localStorage.removeItem("memory.chat.queue.v1.__new__"); } catch (e) {}
          persistQueue();
          renderQueue();
        }
        updateUrl();
        if (isNew) refreshSessionRail();
      }
    },
  });

  // Shared streaming engine (chat-stream.js) context: the engine reads/writes
  // page state through these getters/setters so it survives re-binding, and
  // calls back into page-local functions (answerQuestion/postDecision/
  // handleEvent) that are intentionally different from the side panel's.
  var streamCtx = {
    get messages() { return messages; },
    get log()      { return log; },
    get empty()    { return empty; },
    get input()    { return input; },
    get streaming()  { return streaming; },  set streaming(v)  { streaming = v; },
    get aborter()    { return aborter; },    set aborter(v)    { aborter = v; },
    get bubble()     { return bubble; },     set bubble(v)     { bubble = v; },
    get bubbleHTML() { return bubbleHTML; }, set bubbleHTML(v) { bubbleHTML = v; },
    get bubbleText() { return bubbleText; }, set bubbleText(v) { bubbleText = v; },
    get conversationId() { return conversationId; },
    currentAgent: currentAgent,
    currentAgentName: currentAgentName,
    currentAgentUI: currentAgentUI,
    setStreaming: setStreaming,
    scrollToBottom: scrollToBottom,
    hideEmpty: hideEmpty,
    onStreamStart: function () {
      clearThinking();
      // Repaint immediately so the active row shows "Running" on every turn —
      // not just turn 1 of a brand-new conversation (whose only repaint came
      // from the meta→isNew rail refresh). Idempotent; the end/failure paths
      // below clear it.
      applyRailBadges();
    },
    onStreamFinish: finishTurn,
    onStreamFail: function () {
      finalizeThinking();
      // A failed turn must clear the optimistic "Running" badge: the failure
      // path never runs finishTurn, and a brand-new conversation has no
      // EventSource to repaint the row, so re-pull the rail to show the
      // server's authoritative bucket (done/failed) instead of a stuck spinner.
      if (conversationId) refreshSessionRail();
    },
    answerQuestion: answerQuestion,
    postDecision: postDecision,
    badgeCtx: badgeCtx,
    handleEvent: handleEvent,
  };

  var stream = MemoryChatStream.createEngine(streamCtx);
  var addUserMessage = stream.addUserMessage;
  var addAssistantMessage = stream.addAssistantMessage;
  var openAssistantBubble = stream.openAssistantBubble;
  var updateBubbleText = stream.updateBubbleText;
  var appendToken = stream.appendToken;
  var toolChip = stream.toolChip;
  var setToolStatus = stream.setToolStatus;
  var handleToolEvent = stream.handleToolEvent;
  var renderQuestion = stream.renderQuestion;
  var renderApproval = stream.renderApproval;
  var renderHistoryQuestion = stream.renderHistoryQuestion;
  var streamChat = stream.streamChat;
  var finishStream = stream.finishStream;
  var failStream = stream.failStream;
  var autoGrow = stream.autoGrow;
  var escapeHTML = MemoryChatStream.escapeHTML;
  var normalizeStatus = MemoryChatStream.normalizeStatus;
  var hasFailureSignal = MemoryChatStream.hasFailureSignal;
  var firstError = MemoryChatStream.firstError;
  var summarizeOutput = MemoryChatStream.summarizeOutput;
  var classifyTool = MemoryChatStream.classifyTool;
  var relTime = MemoryChatStream.relTime;
  var isPauseNotice = MemoryChatStream.isPauseNotice;
  var isResumePrompt = MemoryChatStream.isResumePrompt;
  var isThinkingMessage = MemoryChatStream.isThinkingMessage;
  var notify = MemoryChatStream.notify;

  function init() {
    root = document.getElementById("chat-root");
    if (!root || root.dataset.ready === "1") return;
    root.dataset.ready = "1";

    log = document.getElementById("chat-log");
    messages = document.getElementById("chat-messages");
    empty = document.getElementById("chat-empty");
    input = document.getElementById("chat-input");
    sendBtn = document.getElementById("chat-send");
    stopBtn = document.getElementById("chat-stop");
    agentSelect = document.getElementById("chat-agent");
    agentFilterSelect = document.getElementById("chat-agent-filter");
    originFilterSelect = document.getElementById("chat-origin-filter");

    // Dedicated read-only run-transcript page: no composer, no session rail,
    // no agent picker. Render the run's history through the exact shared
    // timeline path and stop — all composer/rail/input wiring below is for the
    // /chat page and would no-op (or worse) on this shell. renderRunHistory is
    // a hoisted declaration, and currentAgentName() falls back to "Memory"
    // when #chat-agent is absent.
    if (root.dataset.run) {
      activeRunId = root.dataset.run;
      renderRunHistory(activeRunId);
      return;
    }

    // session rail drag-to-resize: bind to the fresh #chat-rail-resize (the
    // shell re-renders #chat-root, so this re-runs on each swap). The rail is
    // independent of the composer, so wire it before the form guard below.
    rail = document.getElementById("chat-rail");
    railResizeHandle = document.getElementById("chat-rail-resize");
    railGrip.handle = railResizeHandle;
    if (railResizeHandle) {
      railResizeHandle.addEventListener("pointerdown", railGrip.onPointerDown);
    }
    railGrip.init();

    var form = document.getElementById("chat-form");
    if (!form || !input || !sendBtn) return;

    // resume target from the server-rendered ?c= param
    conversationId = root.getAttribute("data-conv") || "";

    form.addEventListener("submit", onSend);

    input.addEventListener("keydown", function (ev) {
      if (ev.key !== "Enter") return;
      // Cmd/Ctrl+Enter always queues, even when idle — the unambiguous way to
      // park a follow-up without changing what Enter means.
      if (ev.metaKey || ev.ctrlKey) {
        ev.preventDefault();
        enqueueFromInput();
        return;
      }
      if (ev.shiftKey) return; // newline
      ev.preventDefault();
      // Enter follows the turn state: send when idle, queue while running.
      if (streaming) enqueueFromInput();
      else if (input.value.trim()) form.requestSubmit();
    });

    input.addEventListener("input", autoGrow);
    autoGrow();

    if (stopBtn) {
      stopBtn.addEventListener("click", function () {
        stopTurn();
      });
    }

    if (agentSelect) {
      agentSelect.addEventListener("change", function () {
        resetConversation();
        updateModelWarning();
      });
    }

    // session rail agent filter: hide rows for other agents, and surface a
    // dedicated empty state when the active filter matches nothing.
    if (agentFilterSelect) {
      agentFilterSelect.addEventListener("change", function () {
        filterAgent = agentFilterSelect.value;
        applyAgentFilter();
      });
    }

    // session rail origin filter (manual / scheduled / all): combined with
    // the agent filter in applyAgentFilter.
    if (originFilterSelect) {
      originFilterSelect.addEventListener("change", function () {
        filterOrigin = originFilterSelect.value;
        applyAgentFilter();
      });
    }

    // Run-control surfaces. The mount points are created at runtime when
    // chat.templ does not already provide them, so this lane ships without
    // depending on the markup lane: #chat-dock + #chat-queue inside the
    // composer bar, #chat-todos in the transcript column, #chat-run-status in
    // the header. Every helper degrades to a no-op when a mount is absent.
    ensureDockMount();
    ensureQueueMount();
    ensureTodosMount();
    ensureHeaderStatus();
    applyRailBadges();
    renderQueue();

    // session rail: resume a conversation (delegated, survives re-init)
    root.addEventListener("click", function (ev) {
      var t = ev.target.closest("[data-action]");
      if (!t) return;
      if (t.getAttribute("data-action") === "resume-session") {
        var id = t.getAttribute("data-id");
        var agent = t.getAttribute("data-agent") || "";
        if (id) {
          closeSessionRail();
          resumeConversation(id, agent, t);
        }
      } else if (t.getAttribute("data-action") === "open-run") {
        // scheduled run rows open the run's transcript as a normal chat
        // session in this workspace (runs are not resumable /api/chat
        // conversations, but their history renders with the same item shape).
        var runId = t.getAttribute("data-id");
        if (runId) {
          closeSessionRail();
          resumeRun(runId, t);
        }
      } else if (t.getAttribute("data-action") === "open-share") {
        // owner-shared session rows open a read-only transcript on a separate
        // page (they are not resumable in this workspace).
        var shareId = t.getAttribute("data-share-id");
        if (shareId) {
          window.location.href = "/share-sessions/" + encodeURIComponent(shareId);
        }
      } else if (t.getAttribute("data-action") === "toggle-chat-rail") {
        var rail = document.getElementById("chat-rail");
        if (rail) rail.classList.toggle("hidden");
      }
    });

    // tool badges expand inline via their own toggle button; the legacy side
    // panel (openToolPanel) stays in the file but is no longer wired to chips.

    // welcome-state suggestion chips fill the input
    root.addEventListener("click", function (ev) {
      var chip = ev.target.closest("[data-suggestion]");
      if (!chip) return;
      input.value = chip.getAttribute("data-suggestion");
      input.focus();
      autoGrow();
    });

    // deep link / refresh on an active conversation → load its transcript
    if (conversationId) {
      resumeConversation(conversationId, "", null);
    }

    // merge deep link: ?agent=<id>&prompt=<msg> auto-sends the prompt once
    // (init() is guarded by root.dataset.ready, so this never double-fires)
    var prompt = root.getAttribute("data-prompt") || "";
    if (prompt && !conversationId && agentSelect && agentSelect.value) {
      input.value = prompt;
      autoGrow();
      form.requestSubmit();
    }

    // initial pre-send model-availability banner state for the selected agent
    updateModelWarning();
  }

  function currentAgent() {
    return agentSelect ? agentSelect.value : "";
  }

  function currentAgentName() {
    return agentSelect && agentSelect.selectedOptions.length
      ? agentSelect.selectedOptions[0].textContent
      : "Memory";
  }

  // currentAgentUI is the selected agent's declared appearance, read from the
  // #chat-agent option's data-icon/data-color attributes (emitted by chat.templ
  // from the agent's uiConfig). Empty strings mean "no appearance" so the
  // shared bubbles fall back to the default bot avatar.
  function currentAgentUI() {
    var opt = agentSelect && agentSelect.selectedOptions.length ? agentSelect.selectedOptions[0] : null;
    if (!opt) return { icon: "", color: "" };
    return {
      icon: opt.getAttribute("data-icon") || "",
      color: opt.getAttribute("data-color") || "",
    };
  }

  // Pre-send model-availability warning: mirror the selected #chat-agent
  // option's data-warn (server-computed "model can't be served" message) into
  // the #chat-model-warning banner, showing it only when a warning applies.
  // No-op when either element is absent (e.g. run-transcript pages).
  function updateModelWarning() {
    var banner = document.getElementById("chat-model-warning");
    var text = document.getElementById("chat-model-warning-text");
    if (!banner || !text) return;
    var warn = agentSelect && agentSelect.selectedOptions.length
      ? (agentSelect.selectedOptions[0].getAttribute("data-warn") || "")
      : "";
    text.textContent = warn;
    if (warn) {
      banner.classList.remove("hidden");
    } else {
      banner.classList.add("hidden");
    }
  }

  /* ---------- conversation persistence ---------- */

  function updateUrl() {
    var url = "/chat";
    if (conversationId) url += "?c=" + encodeURIComponent(conversationId);
    try { history.replaceState(null, "", url); } catch (e) {}
  }

  function resetConversation() {
    if (eventSource) { eventSource.close(); eventSource = null; }
    if (aborter) aborter.abort();
    clearThinking();
    conversationId = "";
    activeRunId = "";
    updateUrl();
    clearActiveRailItem();
    if (messages) messages.innerHTML = "";
    bubble = null; bubbleHTML = ""; bubbleText = "";
    if (empty) empty.classList.remove("hidden");
    setStreaming(false);
    clearDock();
    clearTodos();
    clearHeaderStatus();
    renderQueue();
  }

  // On mobile the rail is an overlay toggled via the "hidden" class (the
  // toggle/close buttons are md:hidden). After picking a session, collapse it
  // so the user lands on the chat. Desktop keeps the rail pinned (md:flex).
  function closeSessionRail() {
    if (window.matchMedia("(min-width: 768px)").matches) return;
    var rail = document.getElementById("chat-rail");
    if (rail) rail.classList.add("hidden");
  }

  // Rail rows come in two flavors — resumable conversations
  // (data-action="resume-session") and scheduled-run transcripts opened in the
  // chat workspace (data-action="open-run"). The active highlight must clear
  // across both so opening one type deselects the other.
  var railRowSelector = "[data-action='resume-session'], [data-action='open-run']";

  function clearActiveRailItem() {
    var rail = document.getElementById("chat-rail");
    if (!rail) return;
    var items = rail.querySelectorAll(railRowSelector);
    for (var i = 0; i < items.length; i++) items[i].removeAttribute("data-active");
  }

  function markRailItem(id) {
    clearActiveRailItem();
    var rail = document.getElementById("chat-rail");
    if (!rail) return;
    var items = rail.querySelectorAll(railRowSelector);
    for (var i = 0; i < items.length; i++) {
      if (items[i].getAttribute("data-id") === id) items[i].setAttribute("data-active", "true");
    }
  }

  /* ---------- session rail live refresh ---------- */

  function railHas(id) {
    var railList = document.getElementById("chat-rail-list");
    if (!railList) return false;
    var rows = railList.querySelectorAll("[data-action='resume-session']");
    for (var i = 0; i < rows.length; i++) {
      if (rows[i].getAttribute("data-id") === id) return true;
    }
    return false;
  }

  // Show/hide session rows according to the active agent + origin filters and
  // toggle the empty states. Rows carry data-agent and data-origin attributes
  // (server + client render), so filtering is a pure DOM pass that works after
  // both the initial server render and refreshSessionRail() rebuilds.
  function applyAgentFilter() {
    var railList = document.getElementById("chat-rail-list");
    if (!railList) return;

    var rows = railList.querySelectorAll("[data-origin]");
    var visible = 0;
    for (var i = 0; i < rows.length; i++) {
      var agent = rows[i].getAttribute("data-agent") || "";
      var origin = rows[i].getAttribute("data-origin") || "";
      var show = (!filterAgent || agent === filterAgent) && (!filterOrigin || origin === filterOrigin);
      rows[i].classList.toggle("hidden", !show);
      if (show) visible++;
    }

    var emptyAll = document.getElementById("chat-rail-empty");
    var emptyFiltered = document.getElementById("chat-rail-empty-filtered");
    if (rows.length === 0) {
      if (emptyAll) emptyAll.classList.remove("hidden");
      if (emptyFiltered) emptyFiltered.classList.add("hidden");
    } else if (visible === 0) {
      if (emptyAll) emptyAll.classList.add("hidden");
      if (emptyFiltered) emptyFiltered.classList.remove("hidden");
    } else {
      if (emptyAll) emptyAll.classList.add("hidden");
      if (emptyFiltered) emptyFiltered.classList.add("hidden");
    }
  }

  // Re-fetch the server-rendered rail list (past conversations + scheduled-run
  // rows, same markup as the initial page) and swap it into #chat-rail-list,
  // so the row markup lives only in chat.templ (ui.ListRow) instead of being
  // re-built here. The ?c= param lets the server mark the active row.
  // Conversation rows carry data-origin="manual"; scheduled-run rows carry
  // data-origin="scheduled" and open their transcript inside this workspace
  // (data-action="open-run" → resumeRun, rendered from /api/runs/{id}/history).
  async function refreshSessionRail() {
    var railList = document.getElementById("chat-rail-list");
    if (!railList) return;
    try {
      var res = await fetch("/partial/chat-rail?c=" + encodeURIComponent(conversationId || ""));
      if (!res.ok) throw new Error("HTTP " + res.status);
      railList.innerHTML = await res.text();
      applyAgentFilter();
      applyRailBadges();
    } catch (err) {
      reportError(err, "session rail refresh failed");
      // keep the stale list; the next refresh retries
    }
  }

  /* ---------- history transcript (issue: full transcript) ---------- */

  async function resumeConversation(id, agentId, item) {
    if (aborter) aborter.abort();
    setStreaming(false);

    var detail = null;
    try {
      var r = await fetch("/api/conversations/" + encodeURIComponent(id));
      if (!r.ok) throw new Error("HTTP " + r.status);
      detail = await r.json();
    } catch (err) {
      reportError(err, "conversation load failed");
      notify("error", "Could not load conversation: " + err.message);
      return;
    }

    conversationId = id;
    activeRunId = "";
    updateUrl();
    // Conversation-scoped surfaces: show this conversation's parked queue and
    // load its pending-work dock + todo card (a rail switch never carries the
    // previous conversation's queue across). The dock fetch settles before the
    // transcript renders so its approval placement is known.
    renderQueue();

    // Live updates for this conversation: the one-shot /api/chat SSE stream
    // ends when the run pauses (tool approval / ask_user), so watch the push
    // channel instead — it fires when the conversation's state changes
    // (run ends, a decision lands from /settings/approvals, …). Reopened on
    // every resume so a stale connection never lingers.
    if (eventSource) eventSource.close();
    eventSource = new EventSource("/api/conversations/" + encodeURIComponent(id) + "/events");
    eventSource.onmessage = function (ev) {
      var m;
      try { m = JSON.parse(ev.data); } catch (e) { return; }
      if (!m || m.type !== "refresh") return;
      handleRefreshPayload(m);
      if (!streaming) renderHistory(id);
    };
    eventSource.onerror = function () { /* EventSource auto-reconnects; no-op */ };

    // switch the agent picker to the conversation's agent (if still known)
    if (agentId && agentSelect) {
      var opts = Array.prototype.slice.call(agentSelect.options);
      var match = null;
      for (var i = 0; i < opts.length; i++) {
        if (opts[i].value === agentId) { match = opts[i]; break; }
      }
      if (match) agentSelect.value = agentId;
      updateModelWarning();
    }

    await refreshDock();
    await refreshTodos();
    await renderHistory(id);
    markRailItem(id);
    if (item) item.setAttribute("data-active", "true");
    scrollToBottom(true); // opening a conversation: jump to the latest message
    // A turn that ended while this conversation was off-screen still releases
    // its parked queue when it is reopened.
    maybeReleaseOnReopen();
  }

  // Open a scheduled run in the chat workspace: fetch its synthesized history
  // (GET /api/runs/{id}/history) and render it with the same item renderer as
  // conversation transcripts. Runs have no /api/chat SSE push channel, so no
  // EventSource here — answering a pending ask_user card re-renders this
  // history via the scope-aware answerQuestion below.
  async function resumeRun(runId, item) {
    if (aborter) aborter.abort();
    setStreaming(false);
    if (eventSource) { eventSource.close(); eventSource = null; }
    conversationId = ""; // a run is not an /api/chat conversation
    activeRunId = runId;
    updateUrl();
    await renderRunHistory(runId);
    markRailItem(runId);
    if (item) item.setAttribute("data-active", "true");
    scrollToBottom(true); // opening a run: jump to the latest message
  }

  async function renderRunHistory(runId) {
    var data = await fetchTimeline("/api/runs/" + encodeURIComponent(runId) + "/history");
    if (!data) return;
    renderTimelineItems(data.items || [], data.pending_approvals || []);
  }

  async function renderHistory(id) {
    var data = await fetchTimeline("/api/conversations/" + encodeURIComponent(id) + "/history");
    if (!data) return;
    renderTimelineItems(data.items || [], data.pending_approvals || []);
  }

  async function fetchTimeline(url) {
    try {
      var r = await fetch(url);
      if (!r.ok) throw new Error("HTTP " + r.status);
      return await r.json();
    } catch (err) {
      reportError(err, "transcript load failed");
      notify("error", "Could not load transcript: " + err.message);
      return null;
    }
  }

  // A run's lifecycle items (run_start / run_end) carry run_status and, on
  // failure, error_message. They render as typed turn boundaries (runMarker in
  // chat-components.js) rather than generic content: run_start carries the
  // model, and run_end distinguishes completed / failed / input-required, with
  // a failed run surfacing its error_message.
  function isFailedRun(status) {
    return status === "failed" || status === "error";
  }

  // isActiveRunStatus reports whether a run status means the run is still in
  // flight (queued, running, or cancelling) rather than stopped (completed,
  // failed, cancelled, skipped, or paused awaiting input).
  function isActiveRunStatus(status) {
    return status === "working" || status === "submitted" || status === "cancelling";
  }

  // runDurationMs derives a turn's wall-clock duration from the run's
  // completed_at − created_at, falling back to the run_end's duration_ms.
  // Returns null when the run has not ended (no duration is shown then).
  function runDurationMs(ctx, endItem) {
    var start = ctx && ctx.createdAt;
    var end = endItem && (endItem.completed_at || "");
    if (start && end) {
      var d = new Date(end).getTime() - new Date(start).getTime();
      if (isFinite(d) && d >= 0) return d;
    }
    if (endItem && typeof endItem.duration_ms === "number" && endItem.duration_ms > 0) {
      return endItem.duration_ms;
    }
    return null;
  }

  // attachTurnFooters appends one footer per completed run window, on the last
  // assistant bubble of that run (records are {el, ctx} in render order).
  function attachTurnFooters(records) {
    if (!records || !records.length) return;
    var order = [];
    var last = [];
    for (var i = 0; i < records.length; i++) {
      var idx = order.indexOf(records[i].ctx);
      if (idx === -1) { order.push(records[i].ctx); last.push(records[i]); }
      else { last[idx] = records[i]; }
    }
    for (var j = 0; j < order.length; j++) {
      var ctx = order[j];
      var el = last[j] && last[j].el;
      if (!ctx || !ctx.ended || !el) continue;
      var body = el.querySelector(".memory-md");
      el.appendChild(MemoryChatComponents.turnFooter({
        model: ctx.model || currentAgentName(),
        durationMs: ctx.durationMs,
        endTime: ctx.completedAt,
        getText: (function (b) {
          return function () { return b ? (b.innerText || b.textContent || "") : ""; };
        })(body),
      }));
    }
  }

  // renderTimelineItems renders a raw history payload (conversation or run) —
  // the item vocabulary, sorting, and rendering are identical for both.
  function renderTimelineItems(items, pendingApprovals) {
    // Guard: never render history over a live stream. Wiping #chat-messages
    // mid-stream would destroy the in-flight assistant bubble, and appending
    // the same turn beside it would duplicate it. Callers re-render only when
    // !streaming; this backstop keeps a refresh that races a fresh send from
    // clobbering the new stream's bubbles.
    if (streaming) return;
    messages.innerHTML = "";
    hideEmpty();
    var name = currentAgentName();

    // The server timeline is already chronological, but be explicit about it:
    // stable-sort by effective time, then step_number (a per-run counter, so
    // it can't order across runs on its own), preserving array order on ties.
    //
    // run_end items carry the run's start time in created_at and the real end
    // time in completed_at — the shared sortTimeline (MemoryChatHost) sorts by
    // the latter so "Run complete" lands after the run's content instead of
    // right after "Run started".
    items = MemoryChatHost.sortTimeline(items);

    // The run's composed system instruction (recorded as a `system` message)
    // renders once, as a collapsed agent-prompt card above the transcript.
    // Later system records are skipped — the instruction is stable across a
    // conversation's runs, so a card per run would just duplicate it.
    for (var pi = 0; pi < items.length; pi++) {
      var pit = items[pi];
      if (pit && pit.kind === "message" && pit.role === "system") {
        var promptText = (pit.content && pit.content.text) || "";
        if (promptText) {
          if (MemoryChatComponents.agentPromptCard) {
            MemoryChatComponents.agentPromptCard(badgeCtx, promptText);
          }
          break;
        }
      }
    }

    var runStatus = ""; // status of the run being rendered (set by run_start)
    var runError = ""; // error_message of that run, if the server attached one
    var runEnded = false; // whether a run_end was seen for the current run
    var runCtx = null; // model/duration window for the run being rendered
    var turnFooters = []; // {el, ctx} — assistant bubbles, for footer attachment
    var newestRunId = ""; // active run id, resolved from the newest lifecycle item
    var newestRunStatus = "";
    var newestRunEnded = false;
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      if (!item || typeof item !== "object") continue;
      // Run-transcript annotation: step number + relative time under each
      // message/tool item. Only on a dedicated run page (currentScopeIsRun);
      // conversation transcripts and the live stream render no meta at all.
      var meta = currentScopeIsRun()
        ? ("step " + (item.step_number || "") + (item.created_at ? " · " + relTime(item.created_at) : ""))
        : "";
      switch (item.kind) {
        case "run_start":
          runCtx = {
            model: item.run_model || "",
            createdAt: item.created_at || "",
            status: item.run_status || "",
            ended: false,
            completedAt: "",
            durationMs: null,
          };
          runStatus = item.run_status || "";
          runError = item.error_message || "";
          runEnded = false;
          if (item.run_id) {
            newestRunId = item.run_id;
            newestRunStatus = runStatus;
            newestRunEnded = false;
          }
          // Typed turn boundary carrying the run's model.
          messages.appendChild(MemoryChatComponents.runMarker({
            phase: "start",
            status: runStatus,
            model: runCtx.model,
          }));
          break;
        case "run_end":
          runEnded = true;
          if (item.run_id) newestRunId = item.run_id;
          newestRunStatus = item.run_status || runStatus;
          newestRunEnded = true;
          if (runCtx) {
            runCtx.ended = true;
            runCtx.status = item.run_status || runCtx.status;
            runCtx.completedAt = item.completed_at || "";
            runCtx.error = item.error_message || runError;
            runCtx.durationMs = runDurationMs(runCtx, item);
          }
          // Status-distinct boundary: completed / failed (with its error) /
          // input-required ("waiting on you").
          messages.appendChild(MemoryChatComponents.runMarker({
            phase: "end",
            status: item.run_status || runStatus,
            error: item.error_message || runError,
          }));
          break;
        case "tool_call":
          // ask_user never renders as a tool chip. A pending question renders
          // as an interactive card; an answered one (the gateway annotates
          // tool_output.response) renders as a static card with the chosen
          // answer highlighted.
          if (item.tool_name === "ask_user") {
            var qout = item.tool_output || {};
            if (qout.question_id) {
              var qAnswered = qout.response !== undefined && qout.response !== null;
              // A still-pending question lives in the dock when it is live; an
              // answered one stays as a static historical card.
              if (dockAvailable === true && !qAnswered) break;
              renderHistoryQuestion(item.tool_input || {}, qout.question_id, qout.response);
            }
            break;
          }
          // Tool activity from history: same chip language as live streaming.
          var out = item.tool_output;
          var cls = classifyTool(item.tool_status, out);
          toolChip(item.tool_name || "tool", cls.status, cls.error || cls.summary || "", {
            tool: item.tool_name || "tool",
            status: cls.status,
            summary: cls.summary,
            error: cls.error,
            input: item.tool_input,
            output: out,
            inputHtml: item.tool_input_html,
            outputHtml: item.tool_output_html,
            id: item.id,
            durationMs: item.duration_ms,
            meta: meta,
          });
          break;
        case "message":
          var content = item.content || {};
          var text = content.text || "";
          var html = content.html || "";
          if (item.role === "system") {
            // The composed system instruction — already rendered once as the
            // agent-prompt card above; never a chat bubble.
            break;
          }
          if (item.role === "user") {
            if (text && !isResumePrompt(text)) addUserMessage(stripContextPreamble(text), false, meta);
          } else if (item.role === "tool") {
            // Tool result — already shown by the preceding tool_call chip.
            break;
          } else if (isThinkingMessage(content)) {
            // Planning/thinking monologue (carries function_calls): a
            // collapsible thinking block, markdown-rendered. Applies to any
            // agent — the role is the agent name, not a fixed "operator".
            renderThinkingBlock(text, html, "reasoning");
          } else if (isPauseNotice(text)) {
            // Synthetic "Execution paused…" message the executor injects when a
            // run pauses on ask_user — the question card already conveys this.
            break;
          } else if (text || html || content.reasoning) {
            // assistant / diane: skip function-call-only and empty messages.
            // The server splits the deepseek-v4 chain-of-thought into
            // content.reasoning (rendered as a Thinking block above) and renders
            // the reply markdown into content.html.
            if (content.reasoning) {
              renderThinkingBlock(content.reasoning, null, "reasoning");
            }
            var turnEl = addAssistantMessage(html || escapeHTML(text), name, false, meta);
            if (turnEl && runCtx) turnFooters.push({ el: turnEl, ctx: runCtx });
          }
          break;
      }
    }

    // A run with no run_end item (partial/older history) still has to surface
    // its failure, so fall back to the run_start's status once the loop ends.
    if (!runEnded && isFailedRun(runStatus)) {
      messages.appendChild(MemoryChatComponents.runMarker({ phase: "end", status: runStatus, error: runError }));
    }

    // One footer per completed turn, on the turn's last assistant bubble.
    attachTurnFooters(turnFooters);

    // Remember the newest run so the stop action can cancel it.
    lastTimelineRun = { status: newestRunStatus, ended: newestRunEnded };
    if (newestRunId) liveRunId = newestRunId;

    // Pending tool approvals (run paused awaiting a human decision) render as
    // the same interactive Approve/Reject/Cancel cards as live approval
    // events, reusing renderApproval → postDecision. When the pending-work dock
    // is live they belong there instead, so the inline path is skipped (the
    // fallback keeps approvals visible if the dock route is unavailable). On
    // refresh these drop away automatically once the decision lands.
    if (dockAvailable !== true) {
      for (var p = 0; p < pendingApprovals.length; p++) {
        var pa = pendingApprovals[p];
        if (!pa || !pa.questionId) continue;
        renderApproval({ questionId: pa.questionId, tool: pa.tool, input: pa.input });
      }
    }
  }

  // memory ACP wraps each user turn with "## Prior conversation context";
  // surface only the current message, not the re-listed prior turns.
  function stripContextPreamble(text) {
    var m = text.match(/## Current user message\s*\n([\s\S]*)$/);
    return m ? m[1].trim() : text;
  }

  /* ---------- bubbles ---------- */

  // Inline tool-detail sections (summary / error / input / output) and
  // humanizeToolName live in chat-components.js, shared with the side panel so
  // tool-call rendering stays identical across surfaces. The chip builders
  // (toolChip / setToolStatus / handleToolEvent) live in chat-stream.js.

  /* ---------- tool detail side panel ---------- */

  var toolPanel = { panel: null, backdrop: null };

  function openToolPanel(chip) {
    if (!chip || !document.body) return;
    var p = chip._payload || {};

    var backdrop = document.createElement("div");
    backdrop.className = "fixed inset-0 z-40 bg-black/40";
    backdrop.setAttribute("data-action", "close-tool-panel");

    var panel = document.createElement("div");
    panel.className =
      "memory-tool-panel fixed top-0 right-0 z-50 flex h-full w-full max-w-md flex-col bg-base-100 shadow-2xl transition-transform duration-300 ease-out translate-x-full";

    var badgeClass = p.status === "error" ? "badge-error" : p.status === "running" ? "badge-warning" : "badge-success";
    var badgeLabel = p.status || "ok";

    panel.innerHTML =
      '<div class="flex items-center justify-between gap-3 border-b border-base-content/10 px-5 py-4">' +
      '<div class="flex min-w-0 items-center gap-2">' +
      '<span class="iconify lucide--wrench size-4 text-base-content/50" aria-hidden="true"></span>' +
      '<h3 class="truncate font-mono text-sm font-semibold">' + escapeHTML(p.tool || "tool") + "</h3>" +
      '<span class="badge badge-ghost badge-xs font-normal ' + badgeClass + '">' + escapeHTML(badgeLabel) + "</span>" +
      "</div>" +
      '<button type="button" class="btn btn-circle btn-ghost btn-sm" data-action="close-tool-panel" aria-label="Close">' +
      '<span class="iconify lucide--x size-4.5" aria-hidden="true"></span></button>' +
      "</div>" +
      '<div class="min-h-0 grow overflow-y-auto p-5">' +
      (p.summary ? MemoryChatComponents.summarySection(p.summary) : "") +
      (p.error ? MemoryChatComponents.errorSection(p.error) : "") +
      MemoryChatComponents.rawSection(p.input, p.output) +
      (!p.summary && !p.error && p.input === undefined && p.output === undefined
        ? '<p class="text-base-content/40 text-sm">No details yet — the tool is still running.</p>'
        : "") +
      "</div>";

    closeToolPanel(true);
    document.body.appendChild(backdrop);
    document.body.appendChild(panel);
    toolPanel.panel = panel;
    toolPanel.backdrop = backdrop;
    // slide in on the next frame so the transition runs
    requestAnimationFrame(function () {
      panel.classList.remove("translate-x-full");
    });

    var esc = function (ev) {
      if (ev.key === "Escape") closeToolPanel();
    };
    document.addEventListener("keydown", esc);
    panel._esc = esc;
  }

  // detailSection / errorSection / summarySection / rawSection / safeJSON live
  // in chat-components.js — shared with the side panel (see MemoryChatComponents).

  function closeToolPanel(instant) {
    var panel = toolPanel.panel;
    if (!panel) return;
    if (panel._esc) document.removeEventListener("keydown", panel._esc);
    var backdrop = toolPanel.backdrop;
    toolPanel.panel = null;
    toolPanel.backdrop = null;
    if (instant) {
      panel.remove();
      if (backdrop) backdrop.remove();
      return;
    }
    panel.classList.add("translate-x-full");
    setTimeout(function () {
      panel.remove();
      if (backdrop) backdrop.remove();
    }, 300);
  }

  /* ---------- expandable badges (thinking + tool calls) ---------- */

  // The badge builder, its CSS injection, and the thinking-block renderer all
  // live in chat-components.js (shared with sidepanel.js). Call sites below
  // reference them via window.MemoryChatComponents with this page's badgeCtx.

  // Live `thinking` SSE events (and the _debugThinking test hook) land here.
  // A new id starts a fresh badge in stream position; `text` is appended as a
  // plain-text delta; `done:true` stops the shimmer and leaves the badge
  // collapsible.
  function handleThinkingEvent(evt) {
    var id = evt && evt.id;
    if (!id) return;
    var text = evt.text == null ? "" : String(evt.text);
    var done = !!evt.done;
    var role = evt.role || "operator";

    var rec = thinkingMap[id];
    if (!rec) {
      rec = MemoryChatComponents.createThinkingBlock(role, { live: true }, badgeCtx);
      if (!rec) return;
      thinkingMap[id] = rec;
      thinkingOrder.push(id);
      // Keep the badge in arrival order (below the in-progress assistant
      // bubble), like tool chips — chronological, not hoisted above the reply.
      // createThinkingBlock already appended it to the end of the stream.
    }

    if (text) {
      rec.body.appendChild(document.createTextNode(text));
      scrollToBottom();
    }

    if (done && !rec.done) {
      rec.done = true;
      if (rec.setLive) rec.setLive(false);
    }
  }

  // History path: an operator message carrying a non-empty function_calls
  // array is the planning monologue, not the final answer — render it as a
  // collapsed thinking badge using raw text (never markdown-rendered).
  // (isThinkingMessage itself lives in chat-stream.js.)

  function renderThinkingBlock(text, html, role) {
    var rec = MemoryChatComponents.createThinkingBlock(role, { live: false }, badgeCtx);
    if (!rec) return null;
    if (html) {
      // Server rendered the markdown already (content.html) — drop the mono
      // raw-text classes and render it like assistant markdown, but dimmed.
      rec.body.className = "memory-thinking-body memory-md break-words opacity-70";
      rec.body.innerHTML = html;
    } else if (text) {
      rec.body.textContent = text;
    }
    return rec;
  }

  function clearThinking() {
    thinkingMap = {};
    thinkingOrder = [];
  }

  // The stream ended before a segment sent done:true — stop its shimmer so
  // the badge doesn't look stuck, then stop tracking the segments.
  function finalizeThinking() {
    for (var i = 0; i < thinkingOrder.length; i++) {
      var rec = thinkingMap[thinkingOrder[i]];
      if (rec && rec.setLive) rec.setLive(false);
    }
    clearThinking();
  }

  // onStreamFinish: a completed (or aborted) turn is always re-pulled from
  // history so the transcript ends up showing the persisted run — even when
  // the live stream delivered no text deltas (reasoning-model final answers)
  // and/or the EventSource refresh frame was dropped while streaming (the hub
  // consumes the fingerprint, so it is never re-sent). finalizeThinking first
  // stops any live thinking shimmer; refreshActiveTranscript then replaces the
  // streamed DOM with the authoritative history render. The live status clears
  // (a paused run re-arms it via the next refresh payload) and the parked queue
  // releases in order — but NOT after a mid-turn abort.
  function finishTurn(reason) {
    finalizeThinking();
    refreshActiveTranscript();
    clearHeaderStatus();
    liveRunStatus = "";
    // A brand-new conversation has no EventSource subscription (one is opened
    // only in resumeConversation), so no refresh payload fires to repaint its
    // rail row after the turn. Re-pull the rail so the optimistic "Running"
    // badge clears once the turn ends — the run's run_end item is now in
    // history, so the server's re-derived bucket is authoritative. Fire-and-
    // forget, like the other post-turn refresh calls.
    if (conversationId) refreshSessionRail();
    // Release the parked queue only after the engine finishes its own teardown
    // (streaming flag, bubble update) — otherwise the fresh turn this starts
    // would be reset by finishStream's remaining lines.
    if (reason !== "aborted") setTimeout(pumpQueue, 0);
  }

  // Test hook: inject a synthetic thinking event straight into the live code
  // path so streaming can be verified in the browser. Harmless in production.
  function debugThinking(payload) {
    var p = payload || {};
    handleThinkingEvent({
      id: p.id,
      role: p.role,
      text: p.text,
      done: p.done,
    });
  }

  // Test hook: inject a synthetic tool event into the live code path so tool
  // chips can be verified in the browser alongside thinking (DOM order).
  // Harmless in production.
  function debugTool(payload) {
    var p = payload || {};
    handleToolEvent({
      tool: p.tool || "tool",
      status: p.status || "running",
      result: p.result,
      error: p.error,
      resultHtml: p.resultHtml,
    });
  }

  // Test hook: open the in-progress assistant bubble directly (mirrors the
  // streaming open path) so DOM-order tests can position badges relative to it.
  // Harmless in production.
  function debugOpenBubble() {
    openAssistantBubble();
  }

  /* ---------- SSE handling ---------- */

  // Tool-result normalization and tool-chip live updates
  // (normalizeStatus / hasFailureSignal / firstError / summarizeOutput /
  // summarizeText / tryParseJSON / classifyTool / handleToolEvent) live in
  // chat-stream.js, shared with the side panel.

  /* ---------- question cards (opencode-style prompt) ---------- */

  // renderQuestion / renderApproval / renderHistoryQuestion live in
  // chat-stream.js (shared with the side panel); they call back into this
  // page's answerQuestion()/postDecision() below.

  // The transcript currently in the pane is either a conversation
  // (conversationId) or a scheduled run (activeRunId). These scope helpers
  // keep the answer/decision/resume-poll flows working for both: runs have no
  // run_end items, so "resumed" is detected as transcript growth instead.
  function currentScopeId() { return activeRunId || conversationId; }
  function currentScopeIsRun() { return activeRunId !== ""; }

  function scopeHistoryUrl(id) {
    return currentScopeIsRun()
      ? "/api/runs/" + encodeURIComponent(id) + "/history"
      : "/api/conversations/" + encodeURIComponent(id) + "/history";
  }

  // Re-render the transcript that is currently in the pane after a decision
  // lands or a turn's stream finishes. Guarded two ways: never re-render while
  // a stream is live (the wipe in renderTimelineItems would destroy the
  // in-flight bubble), and never stack concurrent refreshes — finishStream
  // already triggers one via onStreamFinish → finishTurn, so the answer/
  // decision paths' explicit calls here collapse into that in-flight fetch
  // instead of double-rendering.
  async function refreshActiveTranscript() {
    if (streaming || transcriptRefreshPending) return;
    transcriptRefreshPending = true;
    try {
      if (activeRunId) await renderRunHistory(activeRunId);
      else if (conversationId) await renderHistory(conversationId);
      // A finished turn may have only just appeared in the transcript (the
      // no-text-delta case) — land the viewport on the newest message.
      scrollToBottom(true);
    } finally {
      transcriptRefreshPending = false;
    }
  }

  // POSTs an answer back to the gateway, then polls the transcript until the
  // resumed run progresses. Memory resumes the run in the background and returns
  // JSON (not an SSE stream), so the assistant's continuation is read back from
  // history rather than streamed live.
  async function answerQuestion(questionId, answerValue) {
    if (streaming || !questionId) return false;
    hideEmpty();
    setStreaming(true);
    openAssistantBubble();

    var baseline = await resumeBaseline(currentScopeId());

    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/respond", { response: answerValue });
    if (!result.ok) {
      // result.network === true is a fetch transport failure; postJSON has
      // already discarded the original error, so mark the rebuilt error
      // explicitly as transient instead of relying on message heuristics.
      if (result.network) {
        var netErr = new Error(result.error || "Failed to fetch");
        netErr.transient = true;
        reportError(netErr, "answer send failed");
      }
      failStream("Could not send answer: " + result.error);
      return false;
    }

    await waitForResume(currentScopeId(), baseline);
    finishStream("done");
    // Pull the answered question out of the dock and re-render the transcript
    // so the resumed run's continuation appears.
    refreshDock();
    refreshActiveTranscript();
    return true;
  }

  /* ---------- approval cards ---------- */

  // renderApproval lives in chat-stream.js (shared with the side panel); it
  // calls back into this page's postDecision() below.

  // POSTs an approval decision to the gateway: payload is the respond body
  // ({response, message?}) or null to cancel. The resumed run is polled until
  // it progresses, then the dock drops the decided item and the transcript
  // re-renders. Returns true when the decision was recorded (the dock's
  // delegated handler uses this to re-enable its controls on failure). There is
  // deliberately no `streaming` guard here: the inline approval cards set the
  // streaming flag before calling in (to show the progress bubble), so guarding
  // on it would silently drop every inline decision.
  async function postDecision(questionId, payload) {
    if (!questionId) return false;
    var baseline = await resumeBaseline(currentScopeId());
    var action = payload ? "respond" : "cancel";
    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/" + action, payload);
    if (!result.ok) {
      // see answerQuestion: result.network is a transient fetch transport failure.
      if (result.network) {
        var netErr = new Error(result.error || "Failed to fetch");
        netErr.transient = true;
        reportError(netErr, "decision send failed");
      }
      failStream("Could not send decision: " + result.error);
      return false;
    }
    await waitForResume(currentScopeId(), baseline);
    finishStream("done");
    // Drop the decided item from the dock and re-render the transcript.
    refreshDock();
    refreshActiveTranscript();
    return true;
  }

  // resumeBaseline snapshots the conversation's resume-progress watermark: the
  // newest run id in the transcript for a conversation, or the transcript item
  // count for a scheduled run scope (which has no run_end items). null when the
  // history can't be read.
  async function resumeBaseline(id) {
    try {
      var r = await fetch(scopeHistoryUrl(id));
      if (!r.ok) return null;
      var data = await r.json();
      var items = data.items || [];
      if (currentScopeIsRun()) return items.length;
      var newest = "";
      for (var i = 0; i < items.length; i++) {
        if (items[i] && items[i].run_id) newest = items[i].run_id;
      }
      return newest;
    } catch (e) {
      reportError(e, "resume baseline failed");
      return null;
    }
  }

  // resumeProgressed reports whether a conversation has moved past its baseline
  // watermark: a run other than the baseline's newest run has stopped (completed,
  // failed, cancelled, skipped, or paused on input-required). A still-"working"
  // run does not count — the server emits a run_end for the pre-created resume
  // run the moment an answer lands, and the prior paused run is flipped back to
  // "working" on resume, neither of which is actual progress.
  async function resumeProgressed(id, baseline) {
    if (baseline === null) return false;
    try {
      var r = await fetch(scopeHistoryUrl(id));
      if (!r.ok) return false;
      var data = await r.json();
      var items = data.items || [];
      if (currentScopeIsRun()) return items.length > baseline;
      var newestEnd = "";
      var newestEndRun = "";
      for (var i = 0; i < items.length; i++) {
        var it = items[i];
        if (!it || it.kind !== "run_end") continue;
        newestEnd = it.run_status || "";
        newestEndRun = it.run_id || "";
      }
      if (!newestEndRun || newestEndRun === baseline) return false;
      return !isActiveRunStatus(newestEnd);
    } catch (e) {
      reportError(e, "resume progress check failed");
      return false;
    }
  }

  // waitForResume polls until the resumed run progresses (conversation scope) or
  // the run transcript grows (run scope) — or a timeout elapses (~60s).
  // Resolves either way: the caller re-renders.
  function waitForResume(id, baseline) {
    return new Promise(function (resolve) {
      var attempts = 0;
      var maxAttempts = 40; // 40 * 1.5s
      var timer = setInterval(function () {
        attempts++;
        resumeProgressed(id, baseline)
          .then(function (progressed) {
            if (progressed || attempts >= maxAttempts) {
              clearInterval(timer);
              resolve();
            }
          })
          .catch(function () {
            if (attempts >= maxAttempts) { clearInterval(timer); resolve(); }
          });
      }, 1500);
    });
  }

  function onSend(ev) {
    ev.preventDefault();
    var text = input ? input.value.trim() : "";
    if (!text) return;
    // A submit while a turn is running parks the message instead of dropping it.
    if (streaming) {
      enqueue(text);
      input.value = "";
      autoGrow();
      input.focus();
      return;
    }
    dispatchMessage(text);
  }

  // dispatchMessage runs one turn through the shared engine. Returns false when
  // it could not start (so a queue pump can put the message back).
  function dispatchMessage(text) {
    if (!text || streaming) return false;
    var agent = currentAgent();
    if (!agent) {
      notify("error", "Pick an agent first.");
      return false;
    }
    // Sending from a run transcript starts a fresh chat: a run is not an
    // /api/chat conversation, so abandon the run scope and clear its bubbles.
    if (activeRunId) {
      activeRunId = "";
      clearActiveRailItem();
      clearThinking();
      updateUrl();
      if (messages) messages.innerHTML = "";
      bubble = null; bubbleHTML = ""; bubbleText = "";
    }
    if (input) {
      input.value = "";
      autoGrow();
      input.focus();
    }
    addUserMessage(text);
    setStreaming(true); // before the bubble: it shows the typing indicator
    openAssistantBubble();
    streamChat(agent, text);
    return true;
  }

  /* ---------- run-control surfaces (dock, todos, rail, queue, stop) ---------- */

  // --- mount points -------------------------------------------------------
  // Each ensure* creates its mount at runtime when chat.templ does not already
  // provide it, and returns null (never throws) when the surrounding page does
  // not have the anchor it needs.

  function composerWrapper() {
    var l = document.getElementById("chat-log");
    return l ? l.nextElementSibling : null;
  }

  function isAtBottom() {
    if (!log) return true;
    return log.scrollHeight - log.scrollTop - log.clientHeight < 90;
  }

  // withAnchoredScroll keeps the transcript pinned to the composer across a
  // height change (dock appearing, todo card expanding, queue growing): if the
  // viewport was at the bottom it stays there, otherwise the reading position
  // is left alone.
  function withAnchoredScroll(fn) {
    var wasBottom = isAtBottom();
    if (typeof fn === "function") fn();
    if (wasBottom) scrollToBottom(true);
  }

  function ensureDockMount() {
    if (mounts.dock && document.contains(mounts.dock)) return mounts.dock;
    var wrap = composerWrapper();
    if (!wrap) return null;
    var el = document.getElementById("chat-dock");
    if (!el) {
      el = document.createElement("div");
      el.id = "chat-dock";
      el.className = "hidden";
      el.setAttribute("aria-live", "polite");
      var form = document.getElementById("chat-form");
      if (form && form.parentNode === wrap) wrap.insertBefore(el, form);
      else wrap.insertBefore(el, wrap.firstChild);
    }
    mounts.dock = el;
    return el;
  }

  function ensureQueueMount() {
    if (mounts.queue && document.contains(mounts.queue)) return mounts.queue;
    var wrap = composerWrapper();
    if (!wrap) return null;
    var el = document.getElementById("chat-queue");
    if (!el) {
      el = document.createElement("div");
      el.id = "chat-queue";
      el.className = "memory-queue hidden";
      el.setAttribute("aria-live", "polite");
      var form = document.getElementById("chat-form");
      if (form && form.parentNode === wrap) wrap.insertBefore(el, form);
      else wrap.appendChild(el);
      el.addEventListener("click", function (ev) {
        var btn = ev.target.closest("[data-queue-action]");
        if (!btn) return;
        var id = btn.getAttribute("data-queue-id");
        if (btn.getAttribute("data-queue-action") === "send") sendNow(id);
        else removeQueued(id);
      });
    }
    mounts.queue = el;
    return el;
  }

  function ensureTodosMount() {
    if (mounts.todos && document.contains(mounts.todos)) return mounts.todos;
    var msgs = document.getElementById("chat-messages");
    if (!msgs || !msgs.parentNode) return null;
    var el = document.getElementById("chat-todos");
    if (!el) {
      el = document.createElement("div");
      el.id = "chat-todos";
      el.className = "hidden";
      // Collapse state is owned by mount.dataset.expanded (set by the delegated
      // .todo-card-toggle bridge in chat.templ, reapplied by applyTodoCollapse
      // on refresh). No separate listener here — one mechanism owns it.
      msgs.parentNode.insertBefore(el, msgs);
    }
    mounts.todos = el;
    return el;
  }

  function ensureHeaderStatus() {
    if (mounts.status && document.contains(mounts.status)) return mounts.status;
    var el = document.getElementById("chat-run-status");
    if (!el) {
      var h1 = document.querySelector("#chat-root h1");
      var header = (h1 && h1.closest) ? h1.closest('[class*="justify-between"]') : null;
      if (!header) return null;
      el = document.createElement("div");
      el.id = "chat-run-status";
      el.className = "memory-run-status hidden";
      el.setAttribute("role", "status");
      el.setAttribute("aria-live", "polite");
      header.appendChild(el);
    }
    mounts.status = el;
    return el;
  }

  // --- pending-work dock --------------------------------------------------

  function clearDock() {
    var el = mounts.dock || document.getElementById("chat-dock");
    if (el) { el.innerHTML = ""; el.classList.add("hidden"); }
    dockAvailable = null;
    liveRunId = "";
    liveRunStatus = "";
    liveBucket = "";
  }

  function refreshDock() {
    var el = ensureDockMount();
    if (!el) return Promise.resolve();
    if (!conversationId) {
      dockAvailable = null;
      withAnchoredScroll(function () { el.innerHTML = ""; el.classList.add("hidden"); });
      return Promise.resolve();
    }
    return fetch("/partial/chat-dock?c=" + encodeURIComponent(conversationId))
      .then(function (res) {
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.text();
      })
      .then(function (html) {
        dockAvailable = true;
        withAnchoredScroll(function () {
          if (html && html.trim()) { el.innerHTML = html; el.classList.remove("hidden"); }
          else { el.innerHTML = ""; el.classList.add("hidden"); }
        });
      })
      .catch(function (err) {
        // Best-effort: the transcript's inline approval path stays in charge
        // when the dock is unavailable, so the chat never blocks on this.
        dockAvailable = false;
        console.warn("chat: dock refresh failed (using inline approvals):", err);
      });
  }

  // --- dock decision controls ---------------------------------------------
  // The dock is a server-rendered fragment swapped in by refreshDock, so its
  // controls are wired by ONE delegated handler (bound on document at boot)
  // rather than per-render listeners. Decisions reuse the inline path's exact
  // request code: approvals go through postDecision (respond/cancel), questions
  // through answerQuestion — both posting the existing /api/chat/questions/…
  // routes. After success the dock + transcript refresh, which is what makes
  // the decided item leave the dock (an empty fragment hides the mount).

  function dockCard(el) {
    return el && el.closest ? el.closest("[data-testid='dock-approval'], [data-testid='dock-question']") : null;
  }

  // setDockBusy disables a card's controls while its decision is in flight so a
  // double-click can't post twice. On failure the handler re-enables them.
  function setDockBusy(card, busy) {
    if (!card) return;
    var controls = card.querySelectorAll("button, input");
    for (var i = 0; i < controls.length; i++) controls[i].disabled = !!busy;
    if (busy) card.setAttribute("aria-busy", "true");
    else card.removeAttribute("aria-busy");
  }

  // selectDockOption toggles one option button. Multi-select keeps independent
  // checks; single-select is exclusive. aria-checked drives both the styling and
  // the value read back on submit.
  function selectDockOption(card, btn) {
    var multi = card.getAttribute("data-dock-type") === "multi_select";
    if (multi) {
      btn.setAttribute("aria-checked", btn.getAttribute("aria-checked") === "true" ? "false" : "true");
    } else {
      var opts = card.querySelectorAll(".dock-question-option");
      for (var i = 0; i < opts.length; i++) {
        opts[i].setAttribute("aria-checked", opts[i] === btn ? "true" : "false");
      }
    }
    updateDockSubmitState(card);
  }

  // dockOptionValue resolves the answer value for one checked option button:
  // the option's value (data-dock-option-value) when present, else the button's
  // label text. This mirrors the inline renderQuestion, which keys selection on
  // opt.value and only falls back to the label for a valueless option — the dock
  // must never submit a display label as the answer when a value exists.
  function dockOptionValue(btn) {
    var v = btn.getAttribute("data-dock-option-value");
    if (v !== null && v !== "") return v;
    return btn.textContent ? btn.textContent.trim() : "";
  }

  // dockAnswerValue mirrors the inline renderQuestion's answerValue(): free text
  // for a text question, a JSON array for multi-select, and the single chosen
  // option otherwise. Null when nothing valid is selected yet.
  function dockAnswerValue(card) {
    if (!card) return null;
    var input = card.querySelector(".dock-question-input");
    if (input) return input.value.trim();
    var checked = card.querySelectorAll(".dock-question-option[aria-checked='true']");
    if (!checked.length) return null;
    var type = card.getAttribute("data-dock-type") || "";
    if (type === "multi_select") {
      var vals = [];
      for (var i = 0; i < checked.length; i++) vals.push(dockOptionValue(checked[i]));
      return JSON.stringify(vals);
    }
    return dockOptionValue(checked[0]) || null;
  }

  function updateDockSubmitState(card) {
    var submit = card.querySelector("[data-dock-action='answer']");
    if (!submit) return;
    var val = dockAnswerValue(card);
    submit.disabled = val === null || val === "";
  }

  // runDockDecision kicks off the decision (showing the same progress bubble the
  // inline cards use) and re-enables the controls if it did not go through.
  function runDockDecision(card, decision) {
    setDockBusy(card, true);
    Promise.resolve(decision).then(
      function (ok) { if (ok !== true) setDockBusy(card, false); },
      function () { setDockBusy(card, false); }
    );
  }

  function onDockClick(ev) {
    var t = ev.target;
    if (!t || !t.closest) return;
    var option = t.closest(".dock-question-option");
    if (option) {
      var optCard = dockCard(option);
      if (optCard) selectDockOption(optCard, option);
      return;
    }
    var control = t.closest("[data-dock-action]");
    if (!control) return;
    var card = dockCard(control);
    if (!card) return;
    var qid = control.getAttribute("data-question-id") || card.getAttribute("data-question-id") || "";
    if (!qid) return;
    var action = control.getAttribute("data-dock-action");
    if (action === "answer") {
      var answer = dockAnswerValue(card);
      if (answer === null || answer === "") return;
      runDockDecision(card, answerQuestion(qid, answer));
      return;
    }
    // approve / reject / cancel all begin the resumed turn the same way the
    // inline approval card does (progress bubble + streaming flag).
    hideEmpty();
    setStreaming(true);
    openAssistantBubble();
    if (action === "approve") {
      runDockDecision(card, postDecision(qid, { response: "approve" }));
    } else if (action === "reject") {
      var msgEl = card.querySelector(".dock-approval-msg");
      runDockDecision(card, postDecision(qid, { response: "reject", message: msgEl ? msgEl.value.trim() : "" }));
    } else if (action === "cancel") {
      runDockDecision(card, postDecision(qid, null));
    }
  }

  // Enter in a dock free-text answer submits it, matching the inline card.
  function onDockKeydown(ev) {
    if (ev.key !== "Enter" || ev.shiftKey) return;
    var el = ev.target;
    if (!el || !el.classList || !el.classList.contains("dock-question-input")) return;
    var card = dockCard(el);
    if (!card) return;
    var answer = dockAnswerValue(card);
    if (answer === null || answer === "") return;
    ev.preventDefault();
    runDockDecision(card, answerQuestion(card.getAttribute("data-question-id") || "", answer));
  }

  function onDockInput(ev) {
    var el = ev.target;
    if (!el || !el.classList || !el.classList.contains("dock-question-input")) return;
    var card = dockCard(el);
    if (card) updateDockSubmitState(card);
  }


  // --- session todo card --------------------------------------------------

  function clearTodos() {
    var el = mounts.todos || document.getElementById("chat-todos");
    if (el) {
      el.innerHTML = "";
      el.classList.add("hidden");
      // Expansion is per-conversation, so a reset drops the remembered choice.
      if (el.dataset) delete el.dataset.expanded;
    }
  }

  function refreshTodos() {
    var el = ensureTodosMount();
    if (!el) return Promise.resolve();
    if (!conversationId) {
      withAnchoredScroll(function () { el.innerHTML = ""; el.classList.add("hidden"); });
      return Promise.resolve();
    }
    return fetch("/partial/chat-todos?c=" + encodeURIComponent(conversationId))
      .then(function (res) {
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.text();
      })
      .then(function (html) {
        withAnchoredScroll(function () {
          if (html && html.trim()) {
            var expanded = el.dataset.expanded === "1";
            el.innerHTML = html;
            el.classList.remove("hidden");
            applyTodoCollapse(el, expanded);
          } else {
            el.innerHTML = "";
            el.classList.add("hidden");
          }
        });
      })
      .catch(function (err) {
        console.warn("chat: todo refresh failed (ignored):", err);
      });
  }

  // applyTodoCollapse reapplies the user's expand/collapse choice to the freshly
  // swapped-in todo card. TodoCard renders a plain <button class="todo-card-toggle">
  // whose aria-expanded drives the list's visibility (app.css), so this is the
  // whole mechanism: the delegated click bridge in chat.templ flips the button
  // and records the choice on the mount's data-expanded, and this function is the
  // only writer on refresh — a refresh therefore never re-collapses a card the
  // user expanded. The default (no remembered choice) stays collapsed.
  function applyTodoCollapse(mount, expanded) {
    var toggles = mount.querySelectorAll(".todo-card-toggle");
    for (var i = 0; i < toggles.length; i++) {
      toggles[i].setAttribute("aria-expanded", expanded ? "true" : "false");
    }
  }

  // --- live status + refresh payload --------------------------------------

  function handleRefreshPayload(m) {
    if (m.runId) liveRunId = m.runId;
    if (m.runStatus) liveRunStatus = m.runStatus;
    if (m.bucket) liveBucket = m.bucket;
    renderHeaderStatus(liveRunStatus, liveBucket, m.pendingApprovals || 0, m.pendingQuestions || 0);
    refreshDock();
    refreshTodos();
    // The rail's per-row badge needs the server's fresh row attributes, so
    // re-pull it only when the state actually changed.
    var fp = [m.bucket || "", m.runId || "", m.runStatus || "", m.pendingApprovals || 0, m.pendingQuestions || 0].join("|");
    if (fp !== railFingerprint) {
      railFingerprint = fp;
      refreshSessionRail();
    } else {
      applyRailBadges();
    }
  }

  function renderHeaderStatus(status, bucket, pendingApprovals, pendingQuestions) {
    var el = ensureHeaderStatus();
    if (!el) return;
    var pending = (pendingApprovals || 0) + (pendingQuestions || 0);
    var state = "";
    var label = "";
    if (bucket === "needs_input" || status === "input-required") {
      state = "waiting";
      label = pending > 0 ? ("Waiting on you · " + pending) : "Waiting on you";
    } else if (status === "submitted" || status === "working" || status === "running" || status === "cancelling") {
      state = "working";
      label = "Working…";
    } else if (isFailedRun(status)) {
      state = "failed";
      label = "Run failed";
    } else {
      clearHeaderStatus();
      return;
    }
    el.className = "memory-run-status";
    el.setAttribute("data-state", state);
    el.innerHTML =
      '<span class="memory-run-status-dot" aria-hidden="true"></span>' +
      '<span class="memory-run-status-label">' + escapeHTML(label) + "</span>";
  }

  function clearHeaderStatus() {
    var el = mounts.status || document.getElementById("chat-run-status");
    if (!el) return;
    el.className = "memory-run-status hidden";
    el.removeAttribute("data-state");
    el.innerHTML = "";
  }

  // --- rail badges --------------------------------------------------------

  // applyRailBadges reads the server-rendered data-bucket /
  // data-pending-approvals / data-pending-questions attributes off each rail row
  // and paints (or updates) one badge per row. Idempotent: an existing badge is
  // reused, so re-running after a re-render never duplicates it, and a row whose
  // state returns to idle has any previously-painted badge cleared.
  function applyRailBadges() {
    var railList = document.getElementById("chat-rail-list");
    if (!railList) return;
    var rows = railList.querySelectorAll(railRowSelector);
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var bucket = row.getAttribute("data-bucket") || "";
      var pa = parseInt(row.getAttribute("data-pending-approvals") || "0", 10) || 0;
      var pq = parseInt(row.getAttribute("data-pending-questions") || "0", 10) || 0;
      var pending = pa + pq;

      // Optimistic waiting indicator: a just-started turn on the active
      // conversation shows "Running" before the server has flipped the row's
      // bucket off done/empty. It must not clobber needs_input/failed or a row
      // with pending work, and it clears on its own once streaming ends.
      var id = row.getAttribute("data-id") || "";
      var optimisticRunning =
        streaming && id === conversationId && conversationId !== "" &&
        bucket !== "needs_input" && bucket !== "failed" && pending === 0;

      var label = optimisticRunning ? "Running" : bucketLabel(bucket);
      var show = pending > 0 || label !== "";

      var badge = row.querySelector("[data-rail-badge], .memory-rail-badge");
      if (!badge) {
        if (!show) continue; // nothing to show — leave the row plain
        badge = document.createElement("span");
        badge.className = "memory-rail-badge";
        badge.setAttribute("data-rail-badge", "");
        badge.setAttribute("aria-hidden", "true");
        // Sit before the trailing chevron so the badge reads as status, not as
        // the row's actionable affordance.
        if (row.lastElementChild) row.insertBefore(badge, row.lastElementChild);
        else row.appendChild(badge);
      }
      if (!show) {
        // Idle/`done` row (or unknown bucket with no pending): clear any badge
        // painted by an earlier pass and hide it.
        badge.className = "memory-rail-badge hidden";
        badge.innerHTML = "";
        badge.title = "";
        continue;
      }
      badge.className = "memory-rail-badge";
      badge.setAttribute("data-bucket", optimisticRunning ? "running" : (bucket || "done"));
      // Build the label span only when there is a label (an unknown bucket with
      // pending work has no label), and never emit a leading separator in the
      // title for that label-less case.
      var html = "";
      if (label !== "") html += '<span class="memory-rail-label">' + escapeHTML(label) + "</span>";
      if (pending > 0) html += '<span class="memory-rail-count">' + pending + "</span>";
      badge.innerHTML = html;
      badge.title = pending > 0 ? (label !== "" ? label + " · " : "") + pending + " pending" : label;
    }
  }

  // bucketLabel maps a run bucket to its human rail-badge label. This MUST stay
  // in lockstep with railBucketLabel in chat_dock.go: `done` (idle) and unknown
  // buckets have no label, so an idle row reads as a plain row with no badge.
  function bucketLabel(bucket) {
    switch (bucket) {
      case "needs_input": return "Needs input";
      case "failed": return "Failed";
      case "running": return "Running";
      default: return "";
    }
  }

  // --- composer queue (client-side, per conversation) ---------------------

  function activeQueueKey() { return conversationId || "__new__"; }

  function activeQueue() {
    var k = activeQueueKey();
    if (!queueStore[k]) queueStore[k] = loadQueue(k);
    return queueStore[k];
  }

  function loadQueue(k) {
    var items = [];
    var armed = false;
    try {
      var raw = localStorage.getItem("memory.chat.queue.v1." + k);
      if (raw) {
        var d = JSON.parse(raw);
        if (d && Array.isArray(d.items)) {
          items = d.items
            .filter(function (x) { return x && typeof x.text === "string" && x.text.trim(); })
            .map(function (x) { return { id: x.id || newQueueId(), text: x.text }; });
        }
        armed = !!(d && d.armed);
      }
    } catch (e) {}
    return { items: items, armed: armed };
  }

  function persistQueue() {
    var k = activeQueueKey();
    var q = queueStore[k];
    if (!q) return;
    try { localStorage.setItem("memory.chat.queue.v1." + k, JSON.stringify(q)); } catch (e) {}
  }

  function newQueueId() {
    return "q" + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  }

  function autoGrowEl(el) {
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 112) + "px";
  }

  function enqueueFromInput() {
    if (!input) return;
    var text = input.value.trim();
    if (!text) return;
    enqueue(text);
    input.value = "";
    autoGrow();
    input.focus();
  }

  function enqueue(text) {
    var q = activeQueue();
    q.items.push({ id: newQueueId(), text: text });
    // Only a message parked during a running turn is armed for auto-release;
    // a Cmd/Ctrl+Enter when idle waits for an explicit action.
    if (streaming) q.armed = true;
    persistQueue();
    renderQueue();
    notify("info", "Queued — it will send when this turn finishes.");
  }

  function removeQueued(id) {
    var q = activeQueue();
    for (var i = 0; i < q.items.length; i++) {
      if (q.items[i].id === id) { q.items.splice(i, 1); break; }
    }
    persistQueue();
    renderQueue();
  }

  // sendNow promotes a queued row to the head of the queue ("Send next"). When
  // the composer is idle it sends immediately; while a turn is running the
  // shared engine cannot start a second turn without clobbering the live bubble
  // (steer is deferred by design), so the row is promoted to the head and
  // released the moment the current turn ends — ahead of every other queued row.
  function sendNow(id) {
    var q = activeQueue();
    var idx = -1;
    var item = null;
    for (var i = 0; i < q.items.length; i++) {
      if (q.items[i].id === id) { idx = i; item = q.items[i]; break; }
    }
    if (idx === -1 || !item) return;
    q.items.splice(idx, 1);
    if (!streaming && !activeRunId) {
      persistQueue();
      renderQueue();
      if (!dispatchMessage(item.text)) {
        q.items.unshift(item);
        persistQueue();
        renderQueue();
      }
      return;
    }
    q.items.unshift(item);
    q.armed = true;
    persistQueue();
    renderQueue();
    notify("info", "Will send next, as soon as this turn finishes.");
  }

  // pumpQueue releases an armed queue in order, one turn at a time: each
  // finished turn calls back here (finishTurn) and the next message goes out.
  // It refuses to release an unarmed queue so a Cmd/Ctrl+Enter parked while idle
  // is not silently released when a later unrelated turn ends.
  function pumpQueue() {
    if (streaming || activeRunId) return;
    var q = activeQueue();
    if (!q.items.length || !q.armed) return;
    var item = q.items.shift();
    // The last queued row clearing also disarms the queue, so a later idle
    // enqueue is not released by the next turn end.
    if (!q.items.length) q.armed = false;
    persistQueue();
    renderQueue();
    if (!dispatchMessage(item.text)) {
      q.items.unshift(item);
      persistQueue();
      renderQueue();
    }
  }

  // maybeReleaseOnReopen releases an armed queue when a conversation is reopened
  // after its turn already ended off-screen.
  function maybeReleaseOnReopen() {
    if (streaming || activeRunId || !conversationId) return;
    var q = activeQueue();
    if (!q.items.length || !q.armed) return;
    if (lastTimelineRun && lastTimelineRun.ended) {
      persistQueue();
      pumpQueue();
    }
  }

  function renderQueue() {
    var mount = ensureQueueMount();
    if (!mount) return;
    var q = activeQueue();
    mount.innerHTML = "";
    if (!q.items.length) {
      mount.classList.add("hidden");
      return;
    }
    mount.classList.remove("hidden");
    var head = document.createElement("div");
    head.className = "memory-queue-head";
    head.innerHTML = '<span class="iconify lucide--list-todo size-3.5" aria-hidden="true"></span>';
    var title = document.createElement("span");
    title.textContent = "Queued · " + q.items.length;
    head.appendChild(title);
    mount.appendChild(head);
    var list = document.createElement("div");
    list.className = "memory-queue-list";
    list.setAttribute("role", "list");
    for (var i = 0; i < q.items.length; i++) list.appendChild(queueRow(q.items[i]));
    mount.appendChild(list);
  }

  function queueRow(item) {
    var row = document.createElement("div");
    row.className = "memory-queue-row";
    row.setAttribute("role", "listitem");
    row.setAttribute("data-queue-id", item.id);

    var ta = document.createElement("textarea");
    ta.className = "memory-queue-input";
    ta.rows = 1;
    ta.value = item.text;
    ta.setAttribute("aria-label", "Queued message — edit before sending");
    ta.addEventListener("input", function () {
      item.text = ta.value;
      persistQueue();
      autoGrowEl(ta);
    });
    ta.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" && !ev.shiftKey) {
        ev.preventDefault();
        sendNow(item.id);
      }
    });

    var send = document.createElement("button");
    send.type = "button";
    send.className = "memory-queue-send";
    send.setAttribute("data-queue-action", "send");
    send.setAttribute("data-queue-id", item.id);
    send.textContent = "Send next";

    var rm = document.createElement("button");
    rm.type = "button";
    rm.className = "memory-queue-remove";
    rm.setAttribute("data-queue-action", "remove");
    rm.setAttribute("data-queue-id", item.id);
    rm.setAttribute("aria-label", "Remove queued message");
    rm.title = "Remove";
    rm.innerHTML = '<span class="iconify lucide--x size-3.5" aria-hidden="true"></span>';

    row.appendChild(ta);
    row.appendChild(send);
    row.appendChild(rm);
    return row;
  }

  // --- stop = real interrupt ---------------------------------------------

  // requestRunCancel POSTs the frozen cancel route and preserves the {"ok":
  // false,"reason":"…"} body (postJSON's {ok,error} contract would drop the
  // reason).
  async function requestRunCancel(runId) {
    try {
      var res = await fetch("/api/chat/runs/" + encodeURIComponent(runId) + "/cancel", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      var data = null;
      try { data = await res.json(); } catch (e) {}
      if (data && data.ok === true) return { ok: true, data: data };
      var reason = (data && (data.reason || data.error)) || ("Gateway error " + res.status);
      return { ok: false, error: reason, network: false };
    } catch (err) {
      return { ok: false, error: err.message, network: true };
    }
  }

  // stopTurn aborts the local stream and cancels the server-side run. It never
  // claims success when no run id could be resolved.
  function stopTurn() {
    var runId = liveRunId;
    if (aborter) aborter.abort();
    if (!runId) {
      notify("info", "Stopped — no active run id, so the server run could not be cancelled.");
      return;
    }
    requestRunCancel(runId).then(function (result) {
      if (!result.ok) {
        notify("error", "Could not cancel the server run: " + result.error);
        return;
      }
      notify("info", "Run cancelled.");
      liveRunStatus = "cancelling";
      renderHeaderStatus("cancelling", liveBucket, 0, 0);
    });
  }

  // The stream engine (streamChat / finishStream / failStream / notify /
  // escapeHTML) lives in chat-stream.js, bound above via MemoryChatStream.

  /* ---------- boot ---------- */
  MemoryChatComponents.ensureBadgeStyle();

  // document-level: close the tool panel via backdrop / X / Escape (survives
  // htmx swaps of #chat-root)
  document.addEventListener("click", function (ev) {
    var t = ev.target.closest("[data-action='close-tool-panel']");
    if (t) closeToolPanel();
  });
  // document-level: the pending-work dock is a swapped-in fragment, so its
  // decision controls are delegated (and survive every refresh). Registered
  // once here, like the tool panel above.
  document.addEventListener("click", onDockClick);
  document.addEventListener("keydown", onDockKeydown);
  document.addEventListener("input", onDockInput);
  // window-level: re-clamp the session rail width when the viewport shrinks.
  // Registered once here (not in init()) so re-renders of #chat-root never
  // stack duplicate listeners.
  window.addEventListener("resize", railGrip.onWindowResize);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.MemoryChat = {
    init: init,
    _debugThinking: debugThinking,
    _debugTool: debugTool,
    _debugOpenBubble: debugOpenBubble,
    refreshSessionRail: refreshSessionRail,
    applyRailBadges: applyRailBadges,
    refreshDock: refreshDock,
    refreshTodos: refreshTodos,
    renderQueue: renderQueue,
  };
})();
