/* Memory web UI — public share page client.
 *
 * A third host of the shared chat engine (MemoryChatStream.createEngine +
 * MemoryChatHost.makeHandleEvent + MemoryChatComponents), mirroring sidepanel.js,
 * so the anonymous share surface renders identically to the authenticated /chat
 * workspace. The page loads the shared modules (chat-transport.js,
 * chat-components.js, chat-host.js, chat-stream.js) before this file.
 *
 * Genuinely share-local concerns stay here: the URL-fragment key exchange, the
 * sealed-cookie transport against /share/api/*, the email gate, terminal states
 * (invalid/revoked/expired/rate-limited/budget-exceeded via an injected error
 * `code`), the session rail, answerQuestion/postDecision against the share
 * approval route, and currentAgentName/currentAgentUI from the sanitized config.
 *
 * Key handling
 * ------------
 * The share key arrives in the URL fragment (#<key>). Fragments are never sent
 * to the server, and this client never writes the key into the DOM or storage.
 * It reads window.location.hash once, POSTs the key to /share/api/exchange,
 * then scrubs it from the address bar. Every later call rides the httpOnly
 * cookie the exchange sets.
 *
 * Email gate
 * ----------
 * When the sanitized config reports requireEmail, the visitor must supply an
 * email before the first message can be sent. The email is captured inline by
 * the gate in share_page.templ (#share-email-form) and kept ONLY in the
 * in-memory `state.email` variable for the life of this page — never the URL,
 * localStorage/sessionStorage, or the sanitized config.
 *
 * SSE frames are JSON, one per `data:` line, matching the app's chat engine
 * byte-for-byte (see chat-stream.js): `meta` (sessionId), `token`, `html`,
 * `thinking`, `mcp_tool`, `question`, `approval`, `citations`, `ui`, `error`
 * (optionally carrying a terminal `code`), `done`.
 */
(function () {
  "use strict";

  var root = document.getElementById("share-root");
  if (!root) return;

  var API = "/share/api";

  var els = {
    log: document.getElementById("share-chat-log"),
    messages: document.getElementById("share-messages"),
    firstLoad: document.getElementById("share-first-load"),
    inlineTerminal: document.getElementById("share-inline-terminal"),
    rail: document.getElementById("share-session-rail"),
    railList: document.getElementById("share-session-list"),
    filter: document.getElementById("share-archived-filter"),
    form: document.getElementById("share-composer"),
    input: document.getElementById("share-input"),
    send: document.getElementById("share-send"),
    stop: document.getElementById("share-stop"),
    emailGate: document.getElementById("share-email-gate"),
    emailForm: document.getElementById("share-email-form"),
    emailInput: document.getElementById("share-email-input"),
    emailError: document.getElementById("share-email-error"),
  };

  var state = {
    sessionId: root.getAttribute("data-active-session") || "",
    streaming: false,
    aborter: null,
    bubble: null,
    bubbleHTML: "",
    bubbleText: "",
    // agentName / agentUI label the assistant bubbles + avatar from the
    // sanitized config (mirrors the app chat's per-agent identity).
    agentName: "",
    agentUI: { icon: "", color: "" },
    // email holds the visitor-supplied address for this page session only.
    // It is never persisted or written to the URL/DOM.
    email: "",
    // placeholder is the config-derived composer placeholder, stored so the
    // email gate can temporarily override it.
    placeholder: "",
    // terminalShown is latched once a terminal notice is revealed, so the
    // engine's unavoidable trailing finishStream("done") (its `streamFailed`
    // flag is internal) can never re-enable the composer afterwards.
    terminalShown: false,
    // liveTurn is the DOM-ownership guard: history re-renders stand down while
    // THIS page owns a live /api/chat-equivalent stream (see chat.js).
    liveTurn: false,
  };

  var thinkingMap = {};   // live thinking segments: id -> {body, setLive, done}
  var thinkingOrder = []; // thinking segment ids in insertion order
  var railOpener = null;  // element to restore focus to when the drawer closes

  /* ---------- small helpers ---------- */

  var escapeHTML = MemoryChatStream.escapeHTML;
  var isThinkingMessage = MemoryChatStream.isThinkingMessage;

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function show(node, on) {
    if (!node) return;
    node.classList.toggle("hidden", !on);
  }

  /* ---------- email gate ---------- */

  // emailRequired reads the requireEmail flag the sanitized config wrote onto
  // the root (applyConfig is the single config→DOM mapper).
  function emailRequired() {
    return root.getAttribute("data-require-email") === "true";
  }

  function hasEmail() {
    return state.email !== "";
  }

  // approvalsEnabled reports whether end users may answer questions / decide
  // tool approvals on this link. It reads the flag the sanitized config wrote
  // onto the root (the server 403s every decision when the link disables
  // end-user approvals), and is handed to the shared chat engine so question
  // and approval cards render read-only instead of showing controls that fail.
  function approvalsEnabled() {
    return root.getAttribute("data-allow-approvals") !== "false";
  }

  // composerBlocked reports whether the composer must stay disabled independent
  // of streaming: the email gate is unsatisfied, or a terminal notice is shown.
  function composerBlocked() {
    return (emailRequired() && !hasEmail()) || state.terminalShown;
  }

  function disableComposer(disabled) {
    if (els.input) els.input.disabled = disabled;
    if (els.send) els.send.disabled = disabled;
  }

  function setStreaming(on) {
    state.streaming = on;
    show(els.stop, on);
    if (els.send) els.send.classList.toggle("hidden", on);
    disableComposer(on || composerBlocked());
    if (!on && !composerBlocked() && els.input) els.input.focus();
  }

  // showEmailError reveals the inline validation message and flags the input.
  function showEmailError(message) {
    if (els.emailError) {
      els.emailError.textContent = message || "";
      show(els.emailError, !!message);
    }
    if (els.emailInput) {
      els.emailInput.setAttribute("aria-invalid", message ? "true" : "false");
    }
  }

  // syncEmailGate reconciles the gate, the composer's disabled state, and the
  // composer placeholder with the current requireEmail + email state. Called
  // after applyConfig, after the email is captured, and by newChat.
  function syncEmailGate() {
    var needs = emailRequired() && !hasEmail();
    show(els.emailGate, needs);
    if (els.input) {
      els.input.placeholder = needs ? "Enter your email to start…" : state.placeholder;
    }
    if (needs) {
      disableComposer(true);
    } else if (!state.streaming) {
      disableComposer(false);
    }
  }

  // revealEmailGate shows the gate and focuses its field, keeping any message
  // the visitor already typed in the composer, and explains why nothing sent.
  function revealEmailGate() {
    show(els.emailGate, true);
    disableComposer(true);
    showEmailError("Enter your email to start chatting.");
    if (els.emailInput) els.emailInput.focus();
  }

  /* ---------- terminal states ---------- */

  var TERMINAL_COPY = {
    invalid: { id: "share-state-invalid", icon: "lucide--link-2-off", title: "This link isn't valid", body: "This share link doesn't work. It may be incomplete, or it may have been copied incorrectly." },
    revoked: { id: "share-state-revoked", icon: "lucide--ban", title: "This link was turned off", body: "The link's owner has stopped sharing this chat." },
    expired: { id: "share-state-expired", icon: "lucide--clock", title: "This link has expired", body: "This share link is no longer active. Ask the owner for a fresh one." },
    "rate-limited": { id: "share-state-rate-limited", icon: "lucide--gauge", title: "Too many messages right now", body: "You've sent messages a bit too quickly. Wait a moment, then try again." },
    "budget-exceeded": { id: "share-state-budget-exceeded", icon: "lucide--wallet", title: "This chat has reached its limit", body: "This shared chat has used up its allowance and can't send more messages." },
  };

  // showTerminal replaces the whole surface with a friendly, leak-free screen.
  // Reached when the exchange itself rejects (bad/expired/revoked key).
  function showTerminal(code) {
    var copy = TERMINAL_COPY[code];
    if (!copy) return;
    var screen = el("div", "share-terminal");
    screen.setAttribute("data-testid", copy.id);
    var aura = el("div", "share-terminal-aura");
    aura.setAttribute("aria-hidden", "true");
    var card = el("main", "share-terminal-card");
    var icon = el("div", "share-terminal-icon bg-base-content/5 text-muted-strong border-base-content/10");
    var span = el("span", "iconify " + copy.icon + " size-7");
    span.setAttribute("aria-hidden", "true");
    icon.appendChild(span);
    var title = el("h1", "text-lg font-semibold tracking-tight", copy.title);
    var body = el("p", "text-muted mt-2 text-sm leading-relaxed", copy.body);
    var hint = el("p", "text-muted-faint mt-5 text-xs", "If you think this is a mistake, ask the person who shared the link for a new one.");
    card.appendChild(icon);
    card.appendChild(title);
    card.appendChild(body);
    card.appendChild(hint);
    screen.appendChild(aura);
    screen.appendChild(card);
    document.body.replaceChildren(screen);
  }

  // showInlineTerminal reveals one of the pre-rendered mid-chat limit notices,
  // latches the terminal state, and releases the busy composer for good.
  function showInlineTerminal(code) {
    if (!els.inlineTerminal) return;
    var copy = TERMINAL_COPY[code];
    if (!copy) return;
    state.terminalShown = true;
    var match = els.inlineTerminal.querySelector('[data-testid="' + copy.id + '"]');
    if (match) match.classList.remove("hidden");
    show(els.inlineTerminal, true);
    setStreaming(false);
    disableComposer(true);
    scrollToBottom();
  }

  // terminalize finalizes the in-flight bubble WITHOUT the red error text
  // (the inline terminal notice owns the failure), then reveals the notice.
  // finishStream first (no trailing onFinish for HTTP/ensureSession failures),
  // with terminalShown set before it so onStreamFinish cannot reload the
  // transcript over the terminal state.
  function terminalize(code) {
    state.terminalShown = true;
    finishStream("error");
    showInlineTerminal(code);
  }

  /* ---------- key exchange ---------- */

  function readAndScrubKey() {
    var hash = window.location.hash || "";
    var key = hash.charAt(0) === "#" ? hash.slice(1) : hash;
    key = key.trim();
    if (key) {
      // Scrub the fragment so the key is not kept in history or copied URLs.
      try {
        window.history.replaceState(null, "", window.location.pathname + window.location.search);
      } catch (err) {
        /* non-fatal */
      }
    }
    return key;
  }

  function exchange(key) {
    return fetch(API + "/exchange", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ key: key }),
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok || (body && body.error)) {
          var e = new Error((body && body.error) || "Could not open this link");
          e.code = (body && body.code) || "invalid";
          throw e;
        }
        return body;
      });
    });
  }

  // fetchConfig rehydrates the sanitized public config from the httpOnly cookie
  // set by a previous exchange — used on a plain refresh/return visit when the
  // URL fragment is gone. The key itself never leaves the cookie.
  function fetchConfig() {
    return fetch(API + "/config", {
      credentials: "include",
      headers: { Accept: "application/json" },
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok || (body && body.error)) {
          var e = new Error((body && body.error) || "Could not load this chat");
          e.code = (body && body.code) || "invalid";
          throw e;
        }
        return body;
      });
    });
  }

  /* ---------- avatar (header + first-load tiles) ---------- */

  // applyAvatar updates the agent avatar tiles (header + first-load) from the
  // sanitized config's icon/color. No declared appearance → keep the
  // server-rendered neutral bot tile.
  function applyAvatar(icon, color) {
    icon = (icon || "").trim();
    color = (color || "").trim();
    if (!icon && !color) return;
    ["share-avatar", "share-first-load-avatar"].forEach(function (id) {
      var holder = document.getElementById(id);
      if (holder) holder.replaceChildren(agentAvatarNode(icon, color));
    });
  }

  function agentAvatarNode(icon, color) {
    var cls = agentIconClass(icon); // "" when icon is a raw glyph (emoji)
    var glyph = cls ? "" : icon;
    var tile = el("div", "grid shrink-0 place-items-center rounded-lg border size-9");
    if (color) {
      tile.style.color = color;
      tile.style.backgroundColor = "color-mix(in oklch," + color + " 10%,transparent)";
      tile.style.borderColor = "color-mix(in oklch," + color + " 15%,transparent)";
    } else {
      tile.classList.add("bg-primary/10", "text-primary", "border-primary/15");
    }
    if (glyph) {
      var g = el("span", "shrink-0 leading-none text-lg");
      g.setAttribute("aria-hidden", "true");
      g.textContent = glyph;
      tile.appendChild(g);
    } else {
      var s = el("span", "iconify " + cls + " size-4.5");
      s.setAttribute("aria-hidden", "true");
      tile.appendChild(s);
    }
    return tile;
  }

  // agentIconClass maps an agent's declared icon to its compiled iconify class,
  // falling back to the bot glyph for empty values and "" for raw glyphs
  // (emoji) — matching the app's agentIconName/typeIconClass contract. The
  // normalization itself is the single implementation in chat-transport.js.
  function agentIconClass(icon) {
    var s = (icon || "").trim();
    if (!s) return "lucide--bot";
    return MemoryChatTransport.agentIconifyClass(s, "");
  }

  /* ---------- config → DOM ---------- */

  // applyConfig applies the sanitized public config returned by the exchange to
  // the already-rendered page (header identity, composer placeholder, rail
  // visibility, require-email flag). The key itself is never touched here —
  // only the sanitized display fields.
  function applyConfig(config) {
    if (!config) return;
    var name = config.agentName || "";
    var desc = config.agentDescription || "";
    var welcome = config.welcomeMessage || "";

    state.agentUI = { icon: config.icon || "", color: config.color || "" };
    applyAvatar(config.icon, config.color);

    var nameEl = document.getElementById("share-agent-name");
    if (nameEl) nameEl.textContent = name;
    state.agentName = name;

    // The server renders the generic "Shared chat — Memory" title because the
    // agent identity only arrives with the exchange; mirror shareDocTitle's
    // "<agent> — Memory" shape once the config reveals the name.
    document.title = name ? name + " — Memory" : "Shared chat — Memory";

    var descEl = document.getElementById("share-agent-description");
    if (descEl) {
      if (desc) {
        descEl.textContent = desc;
        descEl.classList.remove("hidden");
      } else {
        descEl.classList.add("hidden");
      }
    }

    // The first-load hero title is static server-rendered copy ("Chat with
    // Memory") and is never touched here. The subtitle keeps the server's
    // default ("Chat by text.") unless the owner configured a welcome message.
    if (welcome) {
      var firstDesc = document.getElementById("share-first-load-desc");
      if (firstDesc) firstDesc.textContent = welcome;
    }

    state.placeholder = name ? "Message " + name + "…" : "Type a message…";
    if (els.input) {
      els.input.placeholder = state.placeholder;
    }

    // Rail visibility: the owner can disable the session list on the link.
    if (els.rail) {
      els.rail.classList.toggle("hidden", config.showSessionList === false);
    }

    // Record the require-email flag on the root for any email flow / footer.
    root.setAttribute("data-require-email", config.requireEmail ? "true" : "false");

    // Record the approvals flag on the root: approvalsEnabled() reads it and the
    // shared chat engine renders question/approval cards read-only when the link
    // disables end-user approvals (#1892).
    root.setAttribute("data-allow-approvals", config.allowEndUserApprovals === false ? "false" : "true");

    // requireEmail links must collect an email before the first message; a
    // refresh forgets the in-memory value, so the gate prompts again.
    syncEmailGate();
  }

  /* ---------- engine wiring ---------- */

  // Composer helpers (scrollToBottom / hideEmpty) come from the shared
  // MemoryChatHost module, bound to this page's live elements. setStreaming is
  // page-local (email + terminal constraints), so the composer's own is unused.
  var composer = MemoryChatHost.createComposer({
    getLog: function () { return els.log; },
    getEmpty: function () { return els.firstLoad; },
    getSendBtn: function () { return els.send; },
    getStopBtn: function () { return els.stop; },
    getStreaming: function () { return state.streaming; },
    setStreaming: function (v) { state.streaming = v; },
  });
  var scrollToBottom = composer.scrollToBottom;
  var hideEmpty = composer.hideEmpty;

  // Shared badge/thinking renderers (chat-components.js) take a context object.
  // approvalsEnabled gates the decision-bearing A2UI cards too, so the `ui`
  // frame cannot offer a control the link disables (#1892).
  var badgeCtx = {
    get messages() { return els.messages; },
    hideEmpty: hideEmpty,
    scrollToBottom: scrollToBottom,
    approvalsEnabled: approvalsEnabled,
  };

  // Share-local POST helper: the shared MemoryChatHost.postJSON omits
  // credentials, but the public page rides an httpOnly cookie, so every
  // decision POST must send credentials:"include".
  async function sharePostJSON(path, payload) {
    var res;
    try {
      res = await fetch(path, {
        method: "POST",
        credentials: "include",
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

  function currentAgentName() {
    return state.agentName || "Memory";
  }

  function currentAgentUI() {
    return state.agentUI || { icon: "", color: "" };
  }

  /* ---------- thinking blocks (copied from chat.js) ---------- */

  // Live `thinking` SSE events land here: a new id starts a fresh badge in
  // stream position; `text` appends as plain-text deltas; `done` stops the
  // shimmer. renderThinkingBlock renders the history path (non-live).
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

  /* ---------- SSE dispatcher ---------- */

  // Shared dispatcher from MemoryChatHost. The meta frame is page-local
  // (session id); the terminal error frame (with a `code`) is intercepted in
  // the thin wrapper below because makeHandleEvent's `error` case drops `code`.
  var baseHandleEvent = MemoryChatHost.makeHandleEvent({
    getStreaming: function () { return state.streaming; },
    setBubbleHTML: function (v) { state.bubbleHTML = v; },
    updateBubbleText: function () { updateBubbleText(); },
    scrollToBottom: scrollToBottom,
    handleToolEvent: function (evt) { handleToolEvent(evt); },
    renderQuestion: function (evt) { renderQuestion(evt); },
    renderApproval: function (evt) { renderApproval(evt); },
    renderUI: function (evt) {
      stream.noteUISurface();
      MemoryChatComponents.renderA2UISurface(evt.surfaceId, evt.messages, badgeCtx);
    },
    // Grounded citations for the live turn (emitted before `done`): mount the
    // Sources block under the in-progress assistant bubble.
    onCitations: function (list) {
      if (state.bubble) MemoryChatComponents.attachSources(state.bubble, list);
    },
    failStream: function (msg) { failStream(msg); },
    onToken: function (evt) { appendToken(evt.token); },
    onThinking: handleThinkingEvent,
    onMeta: function (evt) {
      // The share wire keeps the leading `meta` frame carrying sessionId (not
      // conversationId — see the unify-share-chat design D2).
      if (evt.sessionId) state.sessionId = evt.sessionId;
    },
  });

  function handleEvent(raw) {
    var evt;
    try { evt = JSON.parse(raw); } catch (e) { return; }
    // Terminal error frame: the gateway annotates known terminal failures with
    // a `code`. Intercept it before the shared dispatcher (which drops `code`),
    // latch the terminal state, and reveal the inline notice — the trailing
    // finishStream("done") is then a no-op for the composer and transcript.
    if (evt && evt.type === "error" && evt.code && TERMINAL_COPY[evt.code]) {
      terminalize(evt.code);
      return;
    }
    baseHandleEvent(raw);
  }

  /* ---------- streaming engine context ---------- */

  // shareStreamPath / shareStreamPayload / shareStreamOptions override the
  // engine's default /api/chat turn (see chat-stream.js streamChat): target the
  // share route, carry the share payload, ride the httpOnly cookie, and handle
  // a terminal code delivered as an HTTP error (stream never opened).
  function shareStreamPath() {
    return API + "/chat";
  }

  function shareStreamPayload(agent, text) {
    var payload = { message: text };
    if (state.sessionId) payload.sessionId = state.sessionId;
    // Defensive: carry the email on the chat request too, so a session created
    // implicitly by the server still satisfies a requireEmail link.
    if (state.email) payload.email = state.email;
    return payload;
  }

  function shareStreamOptions() {
    return {
      credentials: "include",
      onHTTPError: function (res) {
        return res.json().then(
          function (j) {
            if (j && j.code && TERMINAL_COPY[j.code]) {
              terminalize(j.code);
              return;
            }
            failStream(j && j.error ? j.error : "Gateway error " + res.status);
          },
          function () {
            failStream("Gateway error " + res.status);
          }
        );
      },
    };
  }

  var streamCtx = {
    get messages() { return els.messages; },
    get log()      { return els.log; },
    get empty()    { return els.firstLoad; },
    get input()    { return els.input; },
    get streaming()  { return state.streaming; },  set streaming(v)  { state.streaming = v; },
    get aborter()    { return state.aborter; },    set aborter(v)    { state.aborter = v; },
    get bubble()     { return state.bubble; },     set bubble(v)     { state.bubble = v; },
    get bubbleHTML() { return state.bubbleHTML; }, set bubbleHTML(v) { state.bubbleHTML = v; },
    get bubbleText() { return state.bubbleText; }, set bubbleText(v) { state.bubbleText = v; },
    get conversationId() { return state.sessionId; },
    currentAgentName: currentAgentName,
    currentAgentUI: currentAgentUI,
    setStreaming: setStreaming,
    scrollToBottom: scrollToBottom,
    hideEmpty: hideEmpty,
    // Raised by the engine from openAssistantBubble on EVERY live turn start:
    // this page now owns the turn's DOM, so history re-renders stand down.
    onLiveTurnStart: function () { state.liveTurn = true; },
    onStreamStart: function () { clearThinking(); },
    onStreamFinish: function (reason) {
      finalizeThinking();
      state.liveTurn = false;
      // A terminal notice owns the surface now — never reload the transcript
      // over it. Otherwise re-pull the authoritative transcript + rail.
      if (!state.terminalShown) {
        refreshRail();
        reloadTranscript();
      }
    },
    onStreamFail: function () {
      finalizeThinking();
      state.liveTurn = false;
    },
    answerQuestion: answerQuestion,
    postDecision: postDecision,
    approvalsEnabled: approvalsEnabled,
    badgeCtx: badgeCtx,
    handleEvent: handleEvent,
    streamPath: shareStreamPath,
    streamPayload: shareStreamPayload,
    streamOptions: shareStreamOptions,
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

  /* ---------- transcript rendering (history) ---------- */

  // renderTimelineItems renders the full server timeline (all item kinds) into
  // the log through the shared MemoryChatComponents.renderTimeline — the same
  // item vocabulary chat.js's history render uses. Pending approvals render as
  // inline cards; thinking renders (full parity with admin chat).
  function renderTimelineItems(items, pendingApprovals) {
    // Guard: never render history over a live stream THIS page owns.
    if (state.liveTurn) return;
    var list = items || [];
    MemoryChatComponents.renderTimeline(list, {
      container: els.messages,
      badgeCtx: badgeCtx,
      getAgentName: currentAgentName,
      begin: function () {
        if (els.messages) els.messages.innerHTML = "";
        hideEmpty();
      },
      isThinkingItem: function (item, content) { return isThinkingMessage(content); },
      addUserMessage: addUserMessage,
      addAssistantMessage: addAssistantMessage,
      toolChip: toolChip,
      renderHistoryQuestion: renderHistoryQuestion,
      renderThinkingBlock: renderThinkingBlock,
      renderApproval: renderApproval,
      pendingApprovals: pendingApprovals || [],
      afterRender: function () {
        show(els.firstLoad, list.length === 0);
        scrollToBottom(true);
      },
    }, {
      showRunMarkers: false,
      showThinking: true,
      showMeta: false,
      silent: true,
    });
  }

  // reloadTranscript re-fetches the current session's full timeline (items +
  // pending_approvals) and re-renders it authoritatively after a turn finishes
  // or a decision lands.
  function reloadTranscript() {
    if (!state.sessionId) return;
    fetch(API + "/sessions/" + encodeURIComponent(state.sessionId), {
      credentials: "include",
      headers: { Accept: "application/json" },
    })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (body) {
        if (!body) return;
        renderTimelineItems(body.items || [], body.pending_approvals || []);
        highlightActive();
      })
      .catch(function () { /* keep the live transcript on failure */ });
  }

  /* ---------- sessions rail ---------- */

  // isSessionsPartial guards against a 2xx that is not the rail fragment — a
  // soft-404 page or an error body served with status 200. Only the
  // server-rendered partial carries a session row or the empty state, so
  // anything else is treated as a failure instead of being injected into the
  // rail (which would drop the per-row actions).
  function isSessionsPartial(html) {
    if (!html) return false;
    return html.indexOf('data-testid="share-session-row"') !== -1 ||
      html.indexOf('data-testid="share-sessions-empty"') !== -1;
  }

  function refreshRail() {
    if (!els.railList) return;
    var partial = els.railList.getAttribute("data-sessions-partial") || (API + "/partial/sessions");
    var filter = els.filter ? els.filter.value : "active";
    fetch(partial + "?filter=" + encodeURIComponent(filter), {
      credentials: "include",
      headers: { "X-Requested-With": "fetch" },
    })
      .then(function (res) { return res.ok ? res.text() : ""; })
      .then(function (html) {
        if (isSessionsPartial(html)) {
          els.railList.innerHTML = html;
          highlightActive();
          return;
        }
        renderRailError();
      })
      .catch(function () { renderRailError(); });
  }

  // renderRailError replaces the rail with an explicit, recoverable failure
  // state. The rail markup (including each row's archive/unarchive/delete menu)
  // is server-rendered, so a fetch failure cannot be patched up client-side
  // without silently dropping those actions — a previously menu-less JSON
  // fallback hid the failure and left the user unable to manage sessions
  // (#1890). Surfacing the error with a Retry is the recoverable alternative.
  function renderRailError() {
    if (!els.railList) return;
    els.railList.replaceChildren();
    var wrap = el("div", "px-3 py-8 text-center");
    wrap.setAttribute("data-testid", "share-sessions-error");
    wrap.appendChild(el("p", "text-muted text-sm font-medium", "Couldn't load your chats"));
    wrap.appendChild(el("p", "text-muted-faint mt-1 text-xs", "Something went wrong loading this list."));
    var retry = el("button", "btn btn-ghost btn-xs mt-3", "Retry");
    retry.type = "button";
    retry.setAttribute("data-action", "share-retry-rail");
    retry.setAttribute("data-testid", "share-sessions-retry");
    wrap.appendChild(retry);
    els.railList.appendChild(wrap);
  }

  function openSession(id) {
    if (!id) return;
    if (state.aborter) state.aborter.abort();
    state.liveTurn = false;
    state.sessionId = id;
    reloadTranscript();
  }

  // highlightActive marks the active rail row, matching either the existing
  // share spelling or the admin rail's action name (defensive — the rail
  // partial is server-rendered and may carry either).
  function highlightActive() {
    if (!els.railList) return;
    var rows = els.railList.querySelectorAll('[data-action="share-resume-session"], [data-action="resume-session"]');
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var on = row.getAttribute("data-id") === state.sessionId;
      row.classList.toggle("bg-base-200/80", on);
      row.classList.toggle("ring-1", on);
      row.classList.toggle("ring-primary/30", on);
      if (on) row.setAttribute("aria-current", "true"); else row.removeAttribute("aria-current");
    }
  }

  function archiveSession(id) {
    if (!id) return;
    fetch(API + "/sessions/" + encodeURIComponent(id) + "/archive", { method: "POST", credentials: "include" })
      .then(function (r) { if (r.ok) refreshRail(); })
      .catch(function () { /* non-fatal */ });
  }

  function unarchiveSession(id) {
    if (!id) return;
    fetch(API + "/sessions/" + encodeURIComponent(id) + "/unarchive", { method: "POST", credentials: "include" })
      .then(function (r) { if (r.ok) refreshRail(); })
      .catch(function () { /* non-fatal */ });
  }

  function deleteSession(id) {
    if (!id) return;
    fetch(API + "/sessions/" + encodeURIComponent(id), { method: "DELETE", credentials: "include" })
      .then(function (r) {
        if (!r.ok) return;
        if (id === state.sessionId) newChat();
        refreshRail();
      })
      .catch(function () { /* non-fatal */ });
  }

  /* ---------- questions / approvals ---------- */

  // answerQuestion (ask_user) POSTs the chosen answer to the share approval
  // route. Memory resumes the run in the background and returns JSON (not SSE),
  // so completion is surfaced by re-rendering the authoritative transcript.
  async function answerQuestion(questionId, answerValue) {
    if (!questionId || !state.sessionId) return false;
    hideEmpty();
    setStreaming(true);
    openAssistantBubble();

    var result = await sharePostJSON(
      API + "/sessions/" + encodeURIComponent(state.sessionId) + "/approvals/" + encodeURIComponent(questionId),
      { decision: answerValue }
    );
    if (!result.ok) {
      failStream("Could not send answer: " + result.error);
      return false;
    }
    finishStream("done");
    reloadTranscript();
    return true;
  }

  // postDecision (tool approval) maps the shared card's {response, message} onto
  // the share route's {decision, message}: only "approve" approves (the server
  // treats every non-approve as a rejection), reject → "deny", and cancel is a
  // distinct server flow (POST .../cancel, resuming the run with the call not
  // taken). The engine already opened the progress bubble before calling in.
  async function postDecision(questionId, payload) {
    if (!questionId || !state.sessionId) return false;

    var result;
    if (!payload) {
      result = await sharePostJSON(
        API + "/sessions/" + encodeURIComponent(state.sessionId) + "/approvals/" + encodeURIComponent(questionId) + "/cancel",
        undefined
      );
    } else if (payload.response === "approve") {
      result = await sharePostJSON(
        API + "/sessions/" + encodeURIComponent(state.sessionId) + "/approvals/" + encodeURIComponent(questionId),
        { decision: "approve", message: payload.message || "" }
      );
    } else {
      result = await sharePostJSON(
        API + "/sessions/" + encodeURIComponent(state.sessionId) + "/approvals/" + encodeURIComponent(questionId),
        { decision: "deny", message: payload.message || "" }
      );
    }

    if (!result.ok) {
      failStream("Could not send decision: " + result.error);
      return false;
    }
    finishStream("done");
    reloadTranscript();
    return true;
  }

  /* ---------- composer ---------- */

  // ensureSession creates the session before the first message when the link
  // requires an email, forwarding the collected address (POST /share/api/sessions
  // is the create path that carries an email). Links that do not require an email
  // keep the server's implicit creation on the chat call, so no extra round trip.
  function ensureSession(text) {
    if (state.sessionId) return Promise.resolve();
    if (!emailRequired()) return Promise.resolve();
    return fetch(API + "/sessions", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ email: state.email, title: (text || "").slice(0, 60) }),
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok || !body || !body.id) {
          var e = new Error((body && body.error) || "Could not start the chat");
          e.code = (body && body.code) || "";
          throw e;
        }
        state.sessionId = body.id;
      });
    });
  }

  function send(text) {
    text = (text || "").trim();
    if (!text || state.streaming) return;
    if (emailRequired() && !hasEmail()) { revealEmailGate(); return; }

    addUserMessage(text);
    setStreaming(true); // before the bubble: it shows the typing indicator
    openAssistantBubble();

    ensureSession(text).then(function () {
      streamChat(null, text);
    }).catch(function (err) {
      if (err && err.code && TERMINAL_COPY[err.code]) {
        terminalize(err.code);
        return;
      }
      failStream(err && err.message ? err.message : "Could not start the chat.");
    });
  }

  function onSend(ev) {
    ev.preventDefault();
    var text = els.input ? els.input.value.trim() : "";
    if (!text || state.streaming) return;
    // Keep the typed message in place until the email gate is satisfied.
    if (emailRequired() && !hasEmail()) { revealEmailGate(); return; }
    els.input.value = "";
    autoGrow();
    els.input.focus();
    send(text);
  }

  function stop() {
    if (state.aborter) state.aborter.abort();
  }

  if (els.form && els.input) {
    els.form.addEventListener("submit", onSend);
    els.input.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
        ev.preventDefault();
        els.form.requestSubmit();
      }
    });
    els.input.addEventListener("input", autoGrow);
    autoGrow();
  }
  if (els.stop) els.stop.addEventListener("click", stop);

  // Email gate submit: minimal validation (non-empty, contains "@"), an inline
  // error on failure, and capture into in-memory state on success. The input is
  // cleared once captured so the address is not left in the DOM.
  if (els.emailForm && els.emailInput) {
    els.emailForm.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var value = (els.emailInput.value || "").trim();
      if (!value) {
        showEmailError("Enter your email address.");
        els.emailInput.focus();
        return;
      }
      if (value.indexOf("@") === -1) {
        showEmailError("Enter a valid email address.");
        els.emailInput.focus();
        return;
      }
      state.email = value;
      els.emailInput.value = "";
      showEmailError("");
      syncEmailGate();
      if (els.input) els.input.focus();
    });
  }

  /* ---------- rail / drawer wiring ---------- */

  function openRail() {
    state.railOpener = document.activeElement;
    root.setAttribute("data-rail-open", "true");
    var toggle = root.querySelector('[data-action="share-open-rail"]');
    if (toggle) {
      toggle.setAttribute("aria-expanded", "true");
      state.railOpener = toggle;
    }
    var newChat = root.querySelector('[data-action="share-new-chat"]');
    if (newChat) newChat.focus();
  }

  function closeRail() {
    root.setAttribute("data-rail-open", "false");
    var toggle = root.querySelector('[data-action="share-open-rail"]');
    if (toggle) toggle.setAttribute("aria-expanded", "false");
    if (state.railOpener && state.railOpener.focus) state.railOpener.focus();
  }

  root.addEventListener("click", function (ev) {
    var target = ev.target && ev.target.closest ? ev.target.closest("[data-action]") : null;
    if (!target) return;
    var action = target.getAttribute("data-action");
    var id = target.getAttribute("data-id") || target.getAttribute("data-share-id") || "";
    if (action === "share-open-rail") { ev.preventDefault(); openRail(); }
    else if (action === "share-close-rail") { ev.preventDefault(); closeRail(); }
    else if (action === "share-new-chat") { ev.preventDefault(); newChat(); closeRail(); }
    else if (action === "share-retry-rail") { ev.preventDefault(); refreshRail(); }
    else if (action === "share-suggestion") {
      ev.preventDefault();
      send(target.getAttribute("data-text") || "");
    }
    // Resume and the row-menu lifecycle actions accept both the share page's
    // `share-` spelling and the admin rail's unprefixed one (the share menu
    // markup uses `share-archive-session` etc.).
    else if (action === "share-resume-session" || action === "resume-session") {
      if (id) { ev.preventDefault(); closeRail(); openSession(id); }
    } else if (action === "archive-session" || action === "share-archive-session") {
      if (id) { ev.preventDefault(); archiveSession(id); }
    } else if (action === "unarchive-session" || action === "share-unarchive-session") {
      if (id) { ev.preventDefault(); unarchiveSession(id); }
    } else if (action === "delete-session" || action === "share-delete-session") {
      if (id) { ev.preventDefault(); deleteSession(id); }
    }
  });

  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape" && root.getAttribute("data-rail-open") === "true") closeRail();
  });

  function newChat() {
    if (state.aborter) state.aborter.abort();
    state.sessionId = "";
    state.liveTurn = false;
    state.terminalShown = false;
    if (els.messages) els.messages.replaceChildren();
    show(els.inlineTerminal, false);
    syncEmailGate();
    show(els.firstLoad, true);
    if (els.input && !(emailRequired() && !hasEmail())) els.input.focus();
    highlightActive();
  }

  if (els.filter) els.filter.addEventListener("change", refreshRail);

  /* ---------- boot ---------- */

  function boot() {
    var key = readAndScrubKey();
    if (key) {
      exchange(key).then(function (config) {
        applyConfig(config);
        refreshRail();
      }).then(function () {
        if (state.sessionId) openSession(state.sessionId);
      }).catch(function (err) {
        showTerminal(err && err.code ? err.code : "invalid");
      });
      return;
    }
    // No fragment: rehydrate the config from the httpOnly cookie a previous
    // exchange set, then load the sessions. A missing/expired/revoked cookie
    // resolves to the matching terminal state.
    fetchConfig().then(function (config) {
      applyConfig(config);
      refreshRail();
    }).then(function () {
      if (state.sessionId) openSession(state.sessionId);
    }).catch(function (err) {
      showTerminal(err && err.code ? err.code : "invalid");
    });
  }

  MemoryChatComponents.ensureBadgeStyle();
  boot();
})();
