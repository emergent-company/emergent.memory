/* Memory web UI — global agent conversation side panel.
   A persistent right-hand drawer hosted by the app shell. It lives OUTSIDE
   #main-content, so HTMX sidebar navigation never destroys it: open/closed
   state and any in-flight stream survive in-app navigation. On a full page
   reload the conversationId + transcript are restored from localStorage.

   Streaming reuses the same /api/chat SSE contract as chat.js (meta / html /
   mcp_tool / question / error / done frames) — no backend changes needed.

   Public API: window.MemorySidepanel = { open, close, toggle, seed }. */
(function () {
  "use strict";

  var LS_KEY = "memory.sidepanel.v1";

  var panel = null, backdrop = null, log = null, messages = null, empty = null;
  var input = null, sendBtn = null, stopBtn = null, form = null;
  var toggleBtn = null;
  var picker = null, pickerLabel = null, sessionMenu = null;
  var assistantAgent = "";
  var aborter = null, streaming = false;
  var bubble = null, bubbleHTML = "", conversationId = "";
  // Persisted transcript: {role:"user",text} | {role:"assistant",html}
  var history = [];
  // Session picker: the assistant agent's conversations from the backend
  // (never localStorage), newest first. sessionsExpanded lifts the "last 3"
  // default to the full list.
  var sessions = [];
  var sessionsExpanded = false;

  // Drawer width: single source of truth is the shared resize grip
  // (chat-host.js), applied as an inline max-width (the drawer keeps `w-full`
  // behavior on small screens). The double-width header button just sets the
  // preset; dragging overrides it.
  var resizeHandle = null, widthToggle = null;
  var PANEL_WIDTH_DOUBLE = 896;  // 56rem — exactly double the default (448)
  var panelGrip = MemoryChatHost.createResizeGrip({
    handle: null, // assigned in init() once #sidepanel-resize is resolved
    el: panel,
    storageKey: "memory.sidepanel.width.v1",
    defaultWidth: 448, // 28rem — matches the drawer's max-w-md default
    minWidth: 320,     // 20rem — narrowest the drawer can go
    maxWidthFn: function () { return Math.min(window.innerWidth - 48, 1120); },
    direction: -1,
    apply: function (w) {
      if (!panel) return;
      // In modal mode the aside fills #sidepanel-modal-box via the
      // .sidepanel-in-modal stylesheet rule; an inline max-width would beat that
      // rule (inline styles win over non-!important CSS), so never set it here
      // while the panel is in the modal.
      if (modalMode) { panel.style.maxWidth = ""; return; }
      // Below md the drawer is full-width; the persisted width only caps it on
      // screens wide enough for a side drawer to make sense.
      if (window.innerWidth >= 768) panel.style.maxWidth = w + "px";
      else panel.style.maxWidth = "";
    },
    onWidthSet: setWidthPressed,
  });

  // Modal window mode: the <aside> is moved into a centered dialog; closing
  // it (X / Escape / backdrop) returns the aside to the drawer.
  var modal = null, modalBox = null, modalMode = false;

  // Composer helpers (scrollToBottom / hideEmpty / setStreaming) come from the
  // shared MemoryChatHost module, bound to this page's live elements. The
  // getters return the current DOM node/state, so they survive the shell
  // re-rendering #main-content on HTMX swaps (the panel itself persists).
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

  // Shared badge renderers (chat-components.js) take a context object instead
  // of reaching into this IIFE. The getter keeps the live #sidepanel-messages
  // element; hideEmpty/scrollToBottom come from the composer above.
  var badgeCtx = {
    get messages() { return messages; },
    hideEmpty: hideEmpty,
    scrollToBottom: scrollToBottom,
  };

  // SSE dispatcher from the shared MemoryChatHost module. The meta frame is
  // page-local (persist + session picker refresh); frames from an aborted
  // stream are dropped via isAborted (newSession()/loadSession() abort the
  // in-flight controller, but frames already buffered in the reader would
  // otherwise land after the state reset and clobber the fresh session). The
  // panel never renders thinking, so no onThinking is wired.
  var handleEvent = MemoryChatHost.makeHandleEvent({
    getStreaming: function () { return streaming; },
    setBubbleHTML: function (v) { bubbleHTML = v; },
    updateBubbleText: function () { updateBubbleText(); },
    scrollToBottom: scrollToBottom,
    handleToolEvent: function (evt) { handleToolEvent(evt); },
    renderQuestion: function (evt) { renderQuestion(evt); },
    renderApproval: function (evt) { renderApproval(evt); },
    failStream: function (msg) { failStream(msg); },
    isAborted: function () { return !!(aborter && aborter.signal.aborted); },
    onMeta: function (evt) {
      if (evt.conversationId) {
        conversationId = evt.conversationId;
        persist();
        // A brand-new conversation should appear in the session picker
        // immediately — no full reload needed.
        refreshSessions();
      }
    },
  });

  // Shared streaming engine (chat-stream.js) context: the engine reads/writes
  // page state through these getters/setters so it survives re-binding, and
  // calls back into page-local functions (answerQuestion/postDecision/
  // handleEvent) that are intentionally different from the /chat page's.
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
    onStreamStart: function () {},
    onStreamFinish: recordHistory,
    onStreamFail: function () {},
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

  // Called by the shared stream engine on stream finish: persist the finished
  // assistant reply (partial on abort) so a reload can restore it.
  function recordHistory() {
    if (bubble && !bubble._recorded) {
      bubble._recorded = true;
      if (bubbleHTML) {
        history.push({ role: "assistant", html: bubbleHTML });
        persist();
      }
    }
  }

  /* ---------- boot ---------- */

  function init() {
    panel = document.getElementById("sidepanel-panel");
    if (!panel || panel.dataset.ready === "1") return;
    panel.dataset.ready = "1";
    assistantAgent = panel.getAttribute("data-agent-id") || "";

    backdrop = document.getElementById("sidepanel-backdrop");
    log = document.getElementById("sidepanel-log");
    messages = document.getElementById("sidepanel-messages");
    empty = document.getElementById("sidepanel-empty");
    input = document.getElementById("sidepanel-input");
    sendBtn = document.getElementById("sidepanel-send");
    stopBtn = document.getElementById("sidepanel-stop");
    form = document.getElementById("sidepanel-form");
    toggleBtn = document.getElementById("sidepanel-toggle");
    picker = document.getElementById("sidepanel-session-picker");
    pickerLabel = document.getElementById("sidepanel-session-label");
    sessionMenu = document.getElementById("sidepanel-session-menu");
    resizeHandle = document.getElementById("sidepanel-resize");
    widthToggle = document.getElementById("sidepanel-width-toggle");
    modal = document.getElementById("sidepanel-modal");
    modalBox = document.getElementById("sidepanel-modal-box");

    if (resizeHandle) {
      panelGrip.handle = resizeHandle;
      resizeHandle.addEventListener("pointerdown", panelGrip.onPointerDown);
    }
    if (modal) {
      // Native close (Escape / backdrop form) always returns to the drawer.
      // backToDrawer() flips modalMode first, so this never recurses.
      modal.addEventListener("close", function () {
        if (modalMode) backToDrawer();
      });
    }
    window.addEventListener("resize", panelGrip.onWindowResize);
    panelGrip.init();

    if (form && input) {
      form.addEventListener("submit", onSend);
      input.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter" && !ev.shiftKey) {
          ev.preventDefault();
          if (!streaming && input.value.trim()) form.requestSubmit();
        }
      });
      input.addEventListener("input", autoGrow);
      autoGrow();
    }
    if (stopBtn) {
      stopBtn.addEventListener("click", function () {
        if (aborter) aborter.abort();
      });
    }

    restore();
    refreshSessions();
  }

  /* ---------- open / close / toggle ---------- */

  function isOpen() {
    return !panel.classList.contains("translate-x-full");
  }

  // The toggle button lives in the topbar; flip its aria-expanded and paint it
  // primary-ish while the panel is open so the active state reads at a glance.
  function setToggleState(on) {
    if (!toggleBtn) return;
    toggleBtn.setAttribute("aria-expanded", on ? "true" : "false");
    toggleBtn.classList.toggle("text-primary", on);
    toggleBtn.classList.toggle("bg-primary/15", on);
  }

  function open() {
    if (!panel) return;
    panel.classList.remove("translate-x-full");
    if (backdrop) backdrop.classList.remove("hidden");
    panel.setAttribute("aria-hidden", "false");
    setToggleState(true);
    if (input && !input.value) input.focus();
  }

  function close() {
    if (!panel) return;
    if (modalMode) backToDrawer();
    panel.classList.add("translate-x-full");
    if (backdrop) backdrop.classList.add("hidden");
    panel.setAttribute("aria-hidden", "true");
    setToggleState(false);
  }

  function toggle() {
    if (!panel) return;
    if (modalMode) { backToDrawer(); return; }
    if (isOpen()) close();
    else open();
  }

  /* ---------- drawer width (resize handle + double-width toggle) ---------- */

  function setWidthPressed() {
    if (widthToggle) {
      widthToggle.setAttribute("aria-pressed", panelGrip.getWidth() === PANEL_WIDTH_DOUBLE ? "true" : "false");
    }
  }

  // Drag + clamp + persist live in the shared resize grip (chat-host.js); the
  // double-width header button just sets the preset. Persist here explicitly
  // (setWidth applies but doesn't persist) so a reload keeps the toggled width.
  function toggleWidth() {
    var next = panelGrip.getWidth() === PANEL_WIDTH_DOUBLE ? 448 : PANEL_WIDTH_DOUBLE;
    panelGrip.setWidth(next);
    try { localStorage.setItem("memory.sidepanel.width.v1", String(panelGrip.getWidth())); } catch (e) {}
    setWidthPressed();
  }

  /* ---------- modal window mode ---------- */

  // "Open in modal" promotes the drawer to a centered dialog window. The SAME
  // <aside> (header, log, form) is moved into the dialog's modal-box, so the
  // conversation state, listeners, in-flight stream, and scroll position are
  // all preserved — nothing is forked. Closing the modal (X / Escape /
  // backdrop) moves the aside back into the drawer, which stays open.
  function openModal() {
    if (!modal || !modalBox || !panel || modalMode) return;
    modalMode = true;
    // Drop the drawer's inline width cap so .sidepanel-in-modal's width:100%
    // actually fills the modal box (inline style would beat the stylesheet
    // rule and keep the drawer width, e.g. 448px).
    panel.style.maxWidth = "";
    panel.style.width = "";
    if (panel.parentNode) panel.parentNode.removeChild(panel);
    modalBox.appendChild(panel);
    panel.classList.add("sidepanel-in-modal");
    if (backdrop) backdrop.classList.add("hidden"); // dialog has its own backdrop
    modal.showModal();
    if (input) input.focus();
  }

  function backToDrawer() {
    if (!modalMode) return;
    modalMode = false;
    if (modal && modal.open && typeof modal.close === "function") {
      modal.close(); // 'close' listener is guarded by modalMode
    }
    if (panel.parentNode) panel.parentNode.removeChild(panel);
    panel.classList.remove("sidepanel-in-modal");
    var root = document.getElementById("sidepanel-root");
    if (root) root.appendChild(panel);
    panelGrip.onWindowResize(); // restore the drawer's persisted width cap
    if (isOpen() && backdrop) backdrop.classList.remove("hidden");
    if (input) input.focus();
  }

  // seed({ prompt, send }): open the panel, prefill the composer, and
  // auto-submit when send=true. A contextual launch always starts a NEW
  // session: the previous conversationId + transcript are discarded so the
  // prompt lands in a fresh conversation, never the old one.
  function seed(opts) {
    opts = opts || {};
    newSession();
    if (input) {
      input.value = opts.prompt || "";
      autoGrow();
    }
    open();
    if (input) input.focus();
    if (opts.send && opts.prompt && form && !streaming) {
      setTimeout(function () { form.requestSubmit(); }, 150);
    }
  }

  /* ---------- session picker ---------- */

  // Fetches the assistant agent's conversations from the backend, newest
  // first. The list is a server truth — localStorage only caches the in-flight
  // transcript of the current session.
  async function refreshSessions() {
    if (!assistantAgent) return;
    try {
      var r = await fetch("/api/conversations");
      if (!r.ok) throw new Error("HTTP " + r.status);
      var data = await r.json();
      var convs = (data && data.conversations) || [];
      var next = [];
      for (var i = 0; i < convs.length; i++) {
        var c = convs[i];
        if (!c || !c.id || c.agentDefinitionId !== assistantAgent) continue;
        next.push(c);
      }
      next.sort(function (a, b) {
        return new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime();
      });
      sessions = next;
    } catch (err) {
      // keep the stale list; the next refresh retries
    }
    renderSessionPicker();
  }

  // Rebuilds the picker trigger label and the dropdown menu. The menu always
  // leads with "New session"; the session rows mirror the /chat page's rail
  // (title or "Untitled" + relative time), capped at the 3 most recent until
  // the "Show all" footer is clicked. The active session is marked.
  function renderSessionPicker() {
    if (!pickerLabel || !sessionMenu) return;

    var title = "";
    if (conversationId) {
      for (var i = 0; i < sessions.length; i++) {
        if (sessions[i].id === conversationId) { title = sessions[i].title || "Untitled"; break; }
      }
      if (!title) title = "Untitled";
    } else {
      title = "New session";
    }
    pickerLabel.textContent = title;

    var html =
      '<li role="none">' +
      '<a role="menuitem" data-action="sidepanel-new-session" class="flex items-center gap-2 py-1.5">' +
      '<span class="iconify lucide--plus size-3.5 text-primary" aria-hidden="true"></span>' +
      '<span class="text-xs font-medium">New session</span></a></li>' +
      '<li role="separator" class="pointer-events-none mx-2 my-1 border-t border-base-content/10"></li>';

    if (!sessions.length) {
      html +=
        '<li role="none"><span class="block px-3 py-2 text-[11px] text-base-content/40">No sessions yet — chat once and it lands here.</span></li>';
    } else {
      var shown = sessionsExpanded ? sessions.length : Math.min(3, sessions.length);
      for (var i = 0; i < shown; i++) {
        var c = sessions[i];
        var active = c.id === conversationId;
        // The daisyUI menu-active class paints the row's background + text
        // color; the row content inherits so it adapts in both states.
        html +=
          '<li role="none">' +
          '<a role="menuitem" data-action="sidepanel-resume-session" data-id="' + escapeHTML(c.id) + '" class="flex items-center gap-2 py-1.5' + (active ? " menu-active" : "") + '">' +
          '<span class="iconify lucide--messages-square size-3.5 shrink-0 text-base-content/40" aria-hidden="true"></span>' +
          '<span class="min-w-0 grow">' +
          '<span class="block truncate text-xs font-medium">' + escapeHTML(c.title || "Untitled") + '</span>' +
          '<span class="block truncate text-[10px] opacity-60">' + escapeHTML(relTime(c.updatedAt)) + '</span>' +
          '</span>' +
          (active ? '<span class="iconify lucide--check size-3.5 shrink-0 text-base-content/50" aria-hidden="true"></span>' : "") +
          '</a></li>';
      }
      if (!sessionsExpanded && sessions.length > 3) {
        html +=
          '<li role="none"><a role="menuitem" data-action="sidepanel-sessions-more" class="flex items-center justify-center gap-1.5 py-1.5 text-[11px] text-base-content/50">' +
          '<span class="iconify lucide--chevrons-down size-3.5" aria-hidden="true"></span>' +
          'Show all ' + sessions.length + ' sessions</a></li>';
      }
    }
    sessionMenu.innerHTML = html;
  }

  // Starts a fresh conversation: clears the conversationId, the transcript
  // cache, and the rendered log, then persists the cleared state so a reload
  // lands on a new session too. Aborts any in-flight stream first.
  function newSession() {
    if (aborter) aborter.abort();
    streaming = false;
    conversationId = "";
    history = [];
    bubble = null; bubbleHTML = "";
    setStreaming(false);
    if (messages) messages.innerHTML = "";
    if (empty) empty.classList.remove("hidden");
    persist();
    renderSessionPicker();
    if (input) input.focus();
  }

  // Loads a past conversation: aborts the stream, fetches the server
  // transcript, renders it into the log, and sets conversationId so subsequent
  // messages continue that session. Only the conversationId is persisted — the
  // transcript already lives on the backend.
  async function loadSession(id) {
    if (!id) return;
    if (aborter) aborter.abort();
    streaming = false;
    bubble = null; bubbleHTML = "";
    setStreaming(false);

    var items = [];
    try {
      var r = await fetch("/api/conversations/" + encodeURIComponent(id) + "/history");
      if (!r.ok) throw new Error("HTTP " + r.status);
      var data = await r.json();
      items = data.items || [];
    } catch (err) {
      notify("error", "Could not load session: " + err.message);
      return;
    }

    conversationId = id;
    history = [];
    if (messages) messages.innerHTML = "";
    if (empty) empty.classList.add("hidden");
    renderHistoryItems(items);
    persist();
    renderSessionPicker();
    scrollToBottom(true);
    if (input) input.focus();
  }

  // Blurs the dropdown's focused element so the daisyUI focus-based menu
  // closes (it stays open while the trigger or menu holds focus). Only blurs
  // when focus is actually inside the dropdown — never steals focus from the
  // composer or anything else.
  function closeSessionMenu() {
    var el = document.activeElement;
    if (!el || typeof el.blur !== "function") return;
    var inDropdown =
      (picker && picker.contains(el)) ||
      (sessionMenu && sessionMenu.contains(el));
    if (inDropdown) el.blur();
  }

  /* ---------- transcript rendering (history) ---------- */

  // Renders a conversation's server timeline into the log — the same item
  // vocabulary chat.js's renderHistory uses (message / tool_call / question /
  // run lifecycle), adapted to this panel's bubble + chip renderers. Lifecycle
  // items anchor ordering but render nothing; operator planning monologues and
  // synthetic pause/resume notices are skipped, mirroring what the panel's
  // live stream shows.
  function renderHistoryItems(items) {
    // Stable-sort by effective time, then step_number (a per-run counter, so
    // it can't order across runs on its own), preserving array order on ties.
    // run_end items carry the run's start time in created_at and the real end
    // time in completed_at — the shared sortTimeline (MemoryChatHost) sorts by
    // the latter so "Run complete" lands after the run's content.
    items = MemoryChatHost.sortTimeline(items);

    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      if (!item || typeof item !== "object") continue;
      switch (item.kind) {
        case "run_start":
        case "run_end":
          break;
        case "tool_call":
          // ask_user renders as a question card (answered → static); anything
          // else surfaces as a tool chip in the same language as live tools.
          if (item.tool_name === "ask_user") {
            var qout = item.tool_output || {};
            if (qout.question_id) renderHistoryQuestion(item.tool_input || {}, qout.question_id, qout.response);
            break;
          }
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
          });
          break;
        case "message":
          var content = item.content || {};
          var text = content.text || "";
          var html = content.html || "";
          if (item.role === "user") {
            if (text && !isResumePrompt(text)) addUserMessage(text, true);
          } else if (item.role === "tool") {
            // Tool result — already shown by the preceding tool_call chip.
            break;
          } else if (item.role === "operator" && isThinkingMessage(content)) {
            // Planning monologue — the panel's live stream never renders
            // thinking, so history stays consistent and skips it too.
            break;
          } else if (isPauseNotice(text)) {
            // Synthetic pause notice — the question/approval card conveys it.
            break;
          } else if (text || html) {
            addAssistantMessage(html || escapeHTML(text), currentAgentName(), true);
          }
          break;
      }
    }
    if (!items.length && empty) empty.classList.remove("hidden");
    scrollToBottom(true);
  }

  // renderHistoryQuestion / isThinkingMessage / isPauseNotice / isResumePrompt
  // live in chat-stream.js (shared with the /chat page).

  /* ---------- composer ---------- */

  function onSend(ev) {
    ev.preventDefault();
    var text = input.value.trim();
    if (!text || streaming) return;
    if (!currentAgent()) {
      notify("error", "Pick an agent first.");
      return;
    }
    input.value = "";
    autoGrow();
    input.focus();
    addUserMessage(text);
    history.push({ role: "user", text: text });
    persist();
    setStreaming(true); // before the bubble: it shows the typing indicator
    openAssistantBubble();
    streamChat(currentAgent(), text);
  }

  function currentAgent() {
    return assistantAgent;
  }

  function currentAgentName() {
    return "Assistant";
  }

  /* ---------- localStorage persistence ---------- */

  function persist() {
    try {
      localStorage.setItem(LS_KEY, JSON.stringify({
        conversationId: conversationId,
        history: history.slice(-80),
      }));
    } catch (e) { /* storage full / unavailable — conversation continues in memory */ }
  }

  function restore() {
    var st = null;
    try {
      st = JSON.parse(localStorage.getItem(LS_KEY) || "null");
    } catch (e) { st = null; }
    if (!st) return;

    conversationId = st.conversationId || "";
    history = Array.isArray(st.history) ? st.history : [];
    for (var i = 0; i < history.length; i++) {
      var m = history[i];
      if (!m) continue;
      if (m.role === "user" && m.text) addUserMessage(m.text, true);
      else if (m.role === "assistant" && m.html) addAssistantMessage(m.html, currentAgentName(), true);
    }
    if (history.length) scrollToBottom(true);

    // A session loaded from the picker persists only its conversationId (the
    // transcript lives on the backend). After a reload there is no local
    // history to render — re-fetch the server transcript so the session
    // reappears. In-progress conversations (local history present) are left
    // untouched: the local copy is newer than what the backend has flushed.
    if (conversationId && history.length === 0) loadSession(conversationId);
  }

  /* ---------- bubbles ---------- */

  // addUserMessage / addAssistantMessage / openAssistantBubble /
  // updateBubbleText live in chat-stream.js (shared with the /chat page).

  /* ---------- tool chips (identical to chat.js) ---------- */

  // The badge shell + detail renderer come from chat-components.js and the
  // chip builders (toolChip / setToolStatus / handleToolEvent) from
  // chat-stream.js — both shared with the /chat page, so sidebar tool calls
  // render exactly like /chat's: slim Paseo-style row (wrench icon +
  // humanized label + chevron) that expands inline to reveal summary / error /
  // input / output.

  /* ---------- SSE handling (mirrors chat.js's contract) ---------- */

  // Tool-result normalization and tool-chip live updates
  // (normalizeStatus / hasFailureSignal / firstError / summarizeOutput /
  // classifyTool / handleToolEvent) live in chat-stream.js, shared with the
  // /chat page.

  /* ---------- question cards ---------- */

  // renderQuestion / renderApproval / renderHistoryQuestion live in
  // chat-stream.js (shared with the /chat page); they call back into this
  // page's answerQuestion()/postDecision() below.

  async function answerQuestion(questionId, answerValue) {
    if (streaming || !questionId) return;
    hideEmpty();
    setStreaming(true);
    openAssistantBubble();

    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/respond", { response: answerValue });
    if (!result.ok) {
      failStream("Could not send answer: " + result.error);
      return;
    }

    // Memory resumes the run in the background and returns JSON, not an SSE
    // stream. The side panel has no full-transcript renderer, so surface the
    // completion as a plain note rather than streaming a continuation.
    finishStream("done");
    addAssistantMessage("Answer sent — the agent is continuing. Reopen the chat to see the result.", currentAgentName());
  }

  /* ---------- approval cards ---------- */

  // renderApproval lives in chat-stream.js (shared with the /chat page); it
  // calls back into this page's postDecision() below.

  // POSTs an approval decision to the gateway: payload is the respond body
  // ({response, message?}) or null to cancel. Memory resumes the run in the
  // background and returns JSON, so completion is surfaced as a note.
  async function postDecision(questionId, payload, note) {
    var action = payload ? "respond" : "cancel";
    var result = await MemoryChatHost.postJSON("/api/chat/questions/" + encodeURIComponent(questionId) + "/" + action, payload);
    if (!result.ok) {
      failStream("Could not send decision: " + result.error);
      return;
    }
    finishStream("done");
    addAssistantMessage(note || "Decision sent — the agent is continuing.", currentAgentName());
  }

  // The stream engine (streamChat / finishStream / failStream / notify /
  // escapeHTML) lives in chat-stream.js, bound above via MemoryChatStream.
  // On stream finish the engine calls recordHistory() (bound as
  // streamCtx.onStreamFinish) so the assistant reply persists.

  /* ---------- document-level wiring (survives htmx swaps) ---------- */

  // data-action buttons: the topbar toggle, the drawer close/backdrop, and the
  // session picker (new session / resume / show all). Delegated so it survives
  // htmx swaps of #main-content.
  document.addEventListener("click", function (ev) {
    var t = ev.target.closest("[data-action]");
    if (!t) return;
    var a = t.getAttribute("data-action");
    if (a === "toggle-sidepanel") toggle();
    else if (a === "close-sidepanel") {
      // In modal mode the close button returns to the drawer; only the drawer
      // close actually closes the panel.
      if (modalMode) backToDrawer();
      else close();
    } else if (a === "sidepanel-toggle-width") {
      toggleWidth();
    } else if (a === "sidepanel-open-modal") {
      openModal();
    } else if (a === "sidepanel-new-session") {
      newSession();
      closeSessionMenu();
    } else if (a === "sidepanel-resume-session") {
      var id = t.getAttribute("data-id");
      if (id && id !== conversationId) loadSession(id);
      closeSessionMenu();
    } else if (a === "sidepanel-sessions-more") {
      // Rebuild in place with the full list; the trigger keeps focus, so the
      // daisyUI focus-based menu stays open.
      sessionsExpanded = true;
      renderSessionPicker();
    }
  });

  // Escape closes the drawer — but only when focus is inside it, so we never
  // hijack Escape from the /chat page's composer or question cards. A question
  // card inside the panel dismisses itself via its own handler. If the session
  // dropdown is open, Escape closes just the dropdown first.
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    // In modal mode Escape returns to the drawer. stopImmediatePropagation
    // keeps a question/approval card inside the modal from also dismissing —
    // the drawer keeps its pending question. preventDefault stops the native
    // dialog cancel so the 'close' event doesn't re-run backToDrawer.
    if (modalMode && modal && modal.open) {
      ev.preventDefault();
      ev.stopImmediatePropagation();
      backToDrawer();
      return;
    }
    if (!panel || !isOpen()) return;
    if (sessionMenu && sessionMenu.contains(document.activeElement)) {
      ev.preventDefault();
      closeSessionMenu();
      return;
    }
    if (picker && picker.contains(document.activeElement)) {
      ev.preventDefault();
      closeSessionMenu();
      return;
    }
    if (panel.contains(document.activeElement) && !panel.querySelector(".memory-question")) {
      close();
    }
  });

  /* ---------- boot ---------- */

  MemoryChatComponents.ensureBadgeStyle();

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.MemorySidepanel = { open: open, close: close, toggle: toggle, seed: seed };
})();
