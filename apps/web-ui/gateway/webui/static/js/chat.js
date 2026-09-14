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
  var bubbleHTML = "";      // accumulated assistant rendered HTML
  var conversationId = "";  // threaded back into /api/chat turns
  var activeRunId = "";     // scheduled-run transcript open in the pane ("" = conversation/fresh chat)
  var transcriptRefreshPending = false; // one history re-render at a time
  var thinkingMap = {};     // live thinking segments: id -> {details, body, setLive, done}
  var thinkingOrder = [];   // thinking segment ids in insertion order

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
    renderQuestion: function (evt) { renderQuestion(evt); },
    renderApproval: function (evt) { renderApproval(evt); },
    failStream: function (msg) { failStream(msg); },
    onThinking: handleThinkingEvent,
    onMeta: function (evt) {
      if (evt.conversationId) {
        // A brand-new conversation (id not yet in the rail) should appear
        // in the session list immediately — no full reload needed.
        var isNew = evt.conversationId !== conversationId && !railHas(evt.conversationId);
        conversationId = evt.conversationId;
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
    get conversationId() { return conversationId; },
    currentAgent: currentAgent,
    currentAgentName: currentAgentName,
    setStreaming: setStreaming,
    scrollToBottom: scrollToBottom,
    hideEmpty: hideEmpty,
    onStreamStart: clearThinking,
    onStreamFinish: finishTurn,
    onStreamFail: finalizeThinking,
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
      if (ev.key === "Enter" && !ev.shiftKey) {
        ev.preventDefault();
        if (!streaming && input.value.trim()) form.requestSubmit();
      }
    });

    input.addEventListener("input", autoGrow);
    autoGrow();

    if (stopBtn) {
      stopBtn.addEventListener("click", function () {
        if (aborter) aborter.abort();
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
    bubble = null; bubbleHTML = "";
    if (empty) empty.classList.remove("hidden");
    setStreaming(false);
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
    } catch (err) {
      captureError(err);
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
      captureError(err);
      notify("error", "Could not load conversation: " + err.message);
      return;
    }

    conversationId = id;
    activeRunId = "";
    updateUrl();

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
      if (m && m.type === "refresh" && !streaming) renderHistory(id);
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

    await renderHistory(id);
    markRailItem(id);
    if (item) item.setAttribute("data-active", "true");
    scrollToBottom(true); // opening a conversation: jump to the latest message
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
      captureError(err);
      notify("error", "Could not load transcript: " + err.message);
      return null;
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

    var runStatus = ""; // status of the run being rendered (set by run_start)
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
          runStatus = item.run_status || "";
          break;
        case "run_end":
          break;
        case "tool_call":
          // ask_user never renders as a tool chip. A pending question renders
          // as an interactive card; an answered one (the gateway annotates
          // tool_output.response) renders as a static card with the chosen
          // answer highlighted.
          if (item.tool_name === "ask_user") {
            var qout = item.tool_output || {};
            if (qout.question_id) {
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
            meta: meta,
          });
          break;
        case "message":
          var content = item.content || {};
          var text = content.text || "";
          var html = content.html || "";
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
            addAssistantMessage(html || escapeHTML(text), name, false, meta);
          }
          break;
      }
    }

    // Pending tool approvals (run paused awaiting a human decision) render as
    // the same interactive Approve/Reject/Cancel cards as live approval
    // events, reusing renderApproval → postDecision. On refresh these drop
    // away automatically once the decision lands (pending_approvals empties).
    for (var p = 0; p < pendingApprovals.length; p++) {
      var pa = pendingApprovals[p];
      if (!pa || !pa.questionId) continue;
      renderApproval({ questionId: pa.questionId, tool: pa.tool, input: pa.input });
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
      // The assistant bubble is opened before the stream begins (it shows the
      // typing indicator), so a freshly appended thinking badge would land
      // BELOW the answer. Reposition it above the bubble so reasoning reads
      // before the reply, not after.
      if (bubble && rec.details && rec.details.parentElement) {
        messages.insertBefore(rec.details.parentElement, bubble);
      }
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
  // streamed DOM with the authoritative history render.
  function finishTurn() {
    finalizeThinking();
    refreshActiveTranscript();
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
    if (streaming || !questionId) return;
    hideEmpty();
    setStreaming(true);
    openAssistantBubble();

    var baseline = await countRunEnds(currentScopeId());

    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/respond", { response: answerValue });
    if (!result.ok) {
      if (result.network) captureError(new Error(result.error));
      failStream("Could not send answer: " + result.error);
      return;
    }

    await waitForResume(currentScopeId(), baseline);
    finishStream("done");
    // Re-render the transcript so the resumed run's continuation appears.
    refreshActiveTranscript();
  }

  /* ---------- approval cards ---------- */

  // renderApproval lives in chat-stream.js (shared with the side panel); it
  // calls back into this page's postDecision() below.

  // POSTs an approval decision to the gateway: payload is the respond body
  // ({response, message?}) or null to cancel. The resumed run is polled until
  // it progresses, then the transcript re-renders.
  async function postDecision(questionId, payload) {
    var baseline = await countRunEnds(currentScopeId());
    var action = payload ? "respond" : "cancel";
    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/" + action, payload);
    if (!result.ok) {
      if (result.network) captureError(new Error(result.error));
      failStream("Could not send decision: " + result.error);
      return;
    }
    await waitForResume(currentScopeId(), baseline);
    finishStream("done");
    refreshActiveTranscript();
  }

  // countRunEnds returns the number of completed runs in a conversation's
  // transcript — or, for a scheduled run scope (which has no run_end items),
  // the number of transcript items (messages + tool calls). -1 when the
  // history can't be read.
  async function countRunEnds(id) {
    try {
      var r = await fetch(scopeHistoryUrl(id));
      if (!r.ok) return -1;
      var data = await r.json();
      var items = data.items || [];
      if (currentScopeIsRun()) return items.length;
      var n = 0;
      for (var i = 0; i < items.length; i++) {
        if (items[i].kind === "run_end") n++;
      }
      return n;
    } catch (e) {
      captureError(e);
      return -1;
    }
  }

  // waitForResume polls until a new run_end appears (conversation scope) or the
  // run transcript grows (run scope) — the resumed run finished/progressed — or
  // a timeout elapses (~60s). Resolves either way: the caller re-renders.
  function waitForResume(id, baseline) {
    return new Promise(function (resolve) {
      var attempts = 0;
      var maxAttempts = 40; // 40 * 1.5s
      var timer = setInterval(function () {
        attempts++;
        countRunEnds(id)
          .then(function (n) {
            if ((baseline >= 0 && n > baseline) || attempts >= maxAttempts) {
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
    var text = input.value.trim();
    var agent = currentAgent();
    if (!text || streaming) return;
    if (!agent) {
      notify("error", "Pick an agent first.");
      return;
    }
    // Sending from a run transcript starts a fresh chat: a run is not an
    // /api/chat conversation, so abandon the run scope and clear its bubbles.
    if (activeRunId) {
      activeRunId = "";
      clearActiveRailItem();
      clearThinking();
      updateUrl();
      if (messages) messages.innerHTML = "";
      bubble = null; bubbleHTML = "";
    }
    input.value = "";
    autoGrow();
    input.focus();
    addUserMessage(text);
    setStreaming(true); // before the bubble: it shows the typing indicator
    openAssistantBubble();
    streamChat(agent, text);
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
  // window-level: re-clamp the session rail width when the viewport shrinks.
  // Registered once here (not in init()) so re-renders of #chat-root never
  // stack duplicate listeners.
  window.addEventListener("resize", railGrip.onWindowResize);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.MemoryChat = { init: init, _debugThinking: debugThinking, refreshSessionRail: refreshSessionRail };
})();
