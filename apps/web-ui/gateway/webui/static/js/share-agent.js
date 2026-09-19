/* Memory web UI — public share page client.
 *
 * Loaded only by the standalone ShareAgentPage (share_page.templ). It is
 * deliberately self-contained: the public page does not load the app shell,
 * htmx, or the shared chat engine, because the visitor is anonymous and the
 * surface is minimal.
 *
 * Key handling
 * ------------
 * The share key arrives in the URL fragment (#<key>). Fragments are never sent
 * to the server, and this client never writes the key into the DOM or storage.
 * It reads window.location.hash once, POSTs the key to /share/api/exchange,
 * then scrubs it from the address bar. Every later call rides the httpOnly
 * cookie the exchange sets.
 *
 * Endpoint contract (the server lane implements these; this file only calls):
 *   POST /share/api/exchange                          body {key}      -> 200 + Set-Cookie, or {error, code}
 *   GET  /share/api/config                                            -> 200 + sanitized config, or {error, code} (cookie-gated)
 *   POST /share/api/chat                              body {message, sessionId?} -> SSE stream
 *   GET  /share/api/sessions?filter=active|all|archived                -> {sessions:[...]}
 *   GET  /share/api/sessions/:id                                       -> {id,title,messages:[...]}
 *   POST /share/api/sessions/:id/archive                               -> 204
 *   GET  /share/api/partial/sessions                                   -> HTML fragment for the rail
 *   GET  /share/api/questions                                          -> {questions:[...]}
 *   POST /share/api/sessions/:id/approvals/:questionId                 body {decision, message?}
 *
 * SSE frames are JSON, one per `data:` line, mirroring the app's chat engine
 * (see webui/static/js/chat-stream.js): the frame types handled here are
 * `meta`, `token`, `html`, `mcp_tool`, `question`, `approval`, `error`, `done`.
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
    typing: document.getElementById("share-typing"),
    inlineTerminal: document.getElementById("share-inline-terminal"),
    approvals: document.getElementById("share-approvals"),
    rail: document.getElementById("share-session-rail"),
    railList: document.getElementById("share-session-list"),
    filter: document.getElementById("share-archived-filter"),
    form: document.getElementById("share-composer"),
    input: document.getElementById("share-input"),
    send: document.getElementById("share-send"),
    stop: document.getElementById("share-stop"),
  };

  var state = {
    sessionId: root.getAttribute("data-active-session") || "",
    streaming: false,
    aborter: null,
    bubble: null,
    bubbleText: "",
    railOpener: null,
  };

  /* ---------- small helpers ---------- */

  function escapeHTML(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

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

  function scrollToBottom() {
    if (els.log) els.log.scrollTop = els.log.scrollHeight;
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
    var icon = el("div", "share-terminal-icon bg-base-content/5 text-base-content/60 border-base-content/10");
    var span = el("span", "iconify " + copy.icon + " size-7");
    span.setAttribute("aria-hidden", "true");
    icon.appendChild(span);
    var title = el("h1", "text-lg font-semibold tracking-tight", copy.title);
    var body = el("p", "text-base-content/55 mt-2 text-sm leading-relaxed", copy.body);
    var hint = el("p", "text-base-content/35 mt-5 text-xs", "If you think this is a mistake, ask the person who shared the link for a new one.");
    card.appendChild(icon);
    card.appendChild(title);
    card.appendChild(body);
    card.appendChild(hint);
    screen.appendChild(aura);
    screen.appendChild(card);
    document.body.replaceChildren(screen);
  }

  // showInlineTerminal reveals one of the pre-rendered mid-chat limit notices.
  function showInlineTerminal(code) {
    if (!els.inlineTerminal) return;
    var copy = TERMINAL_COPY[code];
    if (!copy) return;
    var match = els.inlineTerminal.querySelector('[data-testid="' + copy.id + '"]');
    if (match) match.classList.remove("hidden");
    show(els.inlineTerminal, true);
    setStreaming(false);
    disableComposer(true);
    scrollToBottom();
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
  // URL fragment is gone. The key itself never leaves the cookie; only the
  // sanitized display config is returned.
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

  // applyConfig applies the sanitized public config returned by the exchange to
  // the already-rendered page (header identity, first-load greeting, composer
  // placeholder, rail visibility, require-email flag). The key itself is never
  // touched here — only the sanitized display fields.
  function applyConfig(config) {
    if (!config) return;
    var name = config.agentName || "";
    var desc = config.agentDescription || "";
    var welcome = config.welcomeMessage || "";

    var nameEl = document.getElementById("share-agent-name");
    if (nameEl) nameEl.textContent = name;

    var descEl = document.getElementById("share-agent-description");
    if (descEl) {
      if (desc) {
        descEl.textContent = desc;
        descEl.classList.remove("hidden");
      } else {
        descEl.classList.add("hidden");
      }
    }

    var titleEl = document.getElementById("share-first-load-title");
    if (titleEl) titleEl.textContent = name ? "Chat with " + name : "Chat";

    var firstDesc = document.getElementById("share-first-load-desc");
    if (firstDesc) {
      firstDesc.textContent = welcome || desc || "Ask a question to get started.";
    }

    if (els.input) {
      els.input.placeholder = name ? "Message " + name + "…" : "Type a message…";
    }

    // Rail visibility: the owner can disable the session list on the link.
    if (config.showSessionList === false && els.rail) {
      els.rail.classList.add("hidden");
    }

    // Record the require-email flag on the root for any email flow / footer.
    root.setAttribute("data-require-email", config.requireEmail ? "true" : "false");
  }

  /* ---------- sessions rail ---------- */

  function loadSessions() {
    var filter = els.filter ? els.filter.value : "active";
    return fetch(API + "/sessions?filter=" + encodeURIComponent(filter), {
      credentials: "include",
      headers: { Accept: "application/json" },
    })
      .then(function (res) { return res.ok ? res.json() : { sessions: [] }; })
      .then(function (body) {
        renderSessions((body && body.sessions) || []);
      })
      .catch(function () { /* keep current rail on failure */ });
  }

  function renderSessions(sessions) {
    if (!els.railList) return;
    els.railList.replaceChildren();
    if (!sessions.length) {
      var empty = el("div", "px-3 py-8 text-center");
      empty.setAttribute("data-testid", "share-sessions-empty");
      empty.appendChild(el("p", "text-base-content/55 text-sm font-medium", "No chats yet"));
      empty.appendChild(el("p", "text-base-content/40 mt-1 text-xs", "Start a new chat and it will show up here."));
      els.railList.appendChild(empty);
      return;
    }
    sessions.forEach(function (s) { els.railList.appendChild(sessionRow(s)); });
  }

  function sessionRow(s) {
    var btn = el("button", "flex w-full items-start gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-base-200/60");
    btn.type = "button";
    btn.setAttribute("data-action", "share-resume-session");
    btn.setAttribute("data-testid", "share-session-row");
    if (s.id === state.sessionId) {
      btn.classList.add("bg-base-200/80", "ring-1", "ring-primary/30");
      btn.setAttribute("aria-current", "true");
    }
    var icon = el("span", "text-base-content/35 mt-0.5 shrink-0");
    var glyph = el("span", "iconify lucide--message-square size-4");
    glyph.setAttribute("aria-hidden", "true");
    icon.appendChild(glyph);
    var main = el("span", "min-w-0 grow");
    main.appendChild(el("span", "block truncate text-sm", s.title || "Untitled chat"));
    var meta = (s.lastActivity || "") + (s.messageCount != null ? " · " + s.messageCount + " messages" : "");
    main.appendChild(el("span", "text-base-content/45 block text-xs", meta));
    btn.appendChild(icon);
    btn.appendChild(main);
    btn.addEventListener("click", function () { openSession(s.id); });
    return btn;
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
      .then(function (html) { if (html) els.railList.innerHTML = html; })
      .catch(function () { loadSessions(); });
  }

  function openSession(id) {
    if (!id) return;
    state.sessionId = id;
    return fetch(API + "/sessions/" + encodeURIComponent(id), {
      credentials: "include",
      headers: { Accept: "application/json" },
    })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (body) {
        if (!body) return;
        renderTranscript(body.messages || []);
        show(els.firstLoad, false);
        highlightActive();
        loadQuestions();
      })
      .catch(function () { /* leave transcript as-is */ });
  }

  function highlightActive() {
    if (!els.railList) return;
    els.railList.querySelectorAll('[data-action="share-resume-session"]').forEach(function (row) {
      var on = row.getAttribute("data-id") === state.sessionId;
      row.classList.toggle("bg-base-200/80", on);
      row.classList.toggle("ring-1", on);
      row.classList.toggle("ring-primary/30", on);
      if (on) row.setAttribute("aria-current", "true"); else row.removeAttribute("aria-current");
    });
  }

  /* ---------- transcript rendering ---------- */

  function renderTranscript(messages) {
    if (!els.messages) return;
    els.messages.replaceChildren();
    messages.forEach(function (m) {
      var role = m.role === "user" ? "user" : "assistant";
      var text = typeof m.content === "string" ? m.content : "";
      appendBubble(role, text, false);
    });
    show(els.firstLoad, messages.length === 0);
    scrollToBottom();
  }

  function appendBubble(role, text, streaming) {
    var wrap = el("div", role === "user" ? "flex justify-end" : "flex justify-start");
    var bubble = el(
      "div",
      role === "user"
        ? "bg-primary text-primary-content max-w-[85%] rounded-2xl rounded-br-sm px-4 py-2.5 text-sm whitespace-pre-wrap break-words"
        : "bg-base-100 border-base-content/10 max-w-[85%] rounded-2xl rounded-bl-sm border px-4 py-2.5 text-sm whitespace-pre-wrap break-words"
    );
    bubble.setAttribute("data-role", role);
    bubble.textContent = text;
    if (streaming) {
      var caret = el("span", "share-caret", "\u258C");
      bubble.appendChild(caret);
    }
    wrap.appendChild(bubble);
    if (els.messages) els.messages.appendChild(wrap);
    scrollToBottom();
    return bubble;
  }

  /* ---------- approvals ---------- */

  function loadQuestions() {
    if (!els.approvals) return;
    fetch(API + "/questions", { credentials: "include", headers: { Accept: "application/json" } })
      .then(function (res) { return res.ok ? res.json() : { questions: [] }; })
      .then(function (body) {
        renderApprovals((body && body.questions) || []);
      })
      .catch(function () { /* non-fatal */ });
  }

  function renderApprovals(items) {
    els.approvals.replaceChildren();
    items.forEach(function (q) {
      if (!q.sessionId || q.sessionId !== state.sessionId) return;
      els.approvals.appendChild(approvalCard(q));
    });
  }

  function approvalCard(q) {
    var card = el("div", "card card-border bg-base-100 border-warning/30 my-3");
    card.setAttribute("data-testid", "share-approval");
    var body = el("div", "card-body gap-2 p-4");
    body.appendChild(el("p", "text-sm font-semibold", q.title || "Approval needed"));
    if (q.tool) {
      var pre = el("pre", "bg-base-200/60 max-h-40 overflow-y-auto rounded-lg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words", q.tool);
      body.appendChild(pre);
    }
    if (q.detail) body.appendChild(el("p", "text-base-content/60 text-xs", q.detail));

    var actions = el("div", "flex justify-end gap-2 pt-1");
    var reject = el("button", "btn btn-ghost btn-sm text-error", "Deny");
    reject.type = "button";
    reject.setAttribute("data-testid", "share-approval-deny");
    var approve = el("button", "btn btn-primary btn-sm", "Approve");
    approve.type = "button";
    approve.setAttribute("data-testid", "share-approval-approve");
    approve.addEventListener("click", function () { postDecision(q.sessionId, q.id, "approve", card); });
    reject.addEventListener("click", function () { postDecision(q.sessionId, q.id, "deny", card); });
    actions.appendChild(reject);
    actions.appendChild(approve);
    body.appendChild(actions);
    card.appendChild(body);
    return card;
  }

  function postDecision(sessionId, questionId, decision, card) {
    fetch(API + "/sessions/" + encodeURIComponent(sessionId) + "/approvals/" + encodeURIComponent(questionId), {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision: decision }),
    })
      .then(function (res) {
        if (res.ok && card) card.remove();
      })
      .catch(function () { /* leave the card for a retry */ });
  }

  /* ---------- streaming ---------- */

  function disableComposer(disabled) {
    if (els.input) els.input.disabled = disabled;
    if (els.send) els.send.disabled = disabled;
  }

  function setStreaming(on) {
    state.streaming = on;
    show(els.stop, on);
    if (els.send) els.send.classList.toggle("hidden", on);
    disableComposer(on);
    if (!on && els.input) els.input.focus();
  }

  function send(text) {
    text = (text || "").trim();
    if (!text || state.streaming) return;
    show(els.firstLoad, false);
    appendBubble("user", text, false);
    state.bubbleText = "";
    state.bubble = appendBubble("assistant", "", true);
    show(els.typing, true);
    setStreaming(true);
    streamChat(text);
  }

  function streamChat(text) {
    state.aborter = new AbortController();
    var payload = { message: text };
    if (state.sessionId) payload.sessionId = state.sessionId;

    fetch(API + "/chat", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify(payload),
      signal: state.aborter.signal,
    })
      .then(function (res) {
        if (!res.ok || !res.body) {
          throw new Error("Gateway error " + res.status);
        }
        var reader = res.body.getReader();
        var decoder = new TextDecoder();
        var buf = "";
        function read() {
          return reader.read().then(function (chunk) {
            if (chunk.done) return finish("done");
            buf += decoder.decode(chunk.value, { stream: true });
            var idx;
            // Manual `data:` line parsing, mirroring chat-stream.js.
            while ((idx = buf.indexOf("\n")) !== -1) {
              var line = buf.slice(0, idx).trim();
              buf = buf.slice(idx + 1);
              if (!line) continue;
              if (line.indexOf("data:") === 0) handleEvent(line.slice(5).trim());
            }
            return read();
          });
        }
        return read();
      })
      .catch(function (err) {
        if (err && err.name === "AbortError") return finish("aborted");
        failStream(err && err.message ? err.message : "Something went wrong.");
      });
  }

  function handleEvent(raw) {
    var evt;
    try { evt = JSON.parse(raw); } catch (e) { return; }
    switch (evt.type) {
      case "meta":
        if (evt.sessionId) state.sessionId = evt.sessionId;
        break;
      case "token":
        state.bubbleText += (evt.delta != null ? evt.delta : evt.text || "");
        updateBubbleText();
        break;
      case "html":
        if (state.bubble) {
          state.bubble.textContent = "";
          state.bubble.innerHTML = evt.html || "";
        }
        break;
      case "question":
      case "approval":
        loadQuestions();
        break;
      case "error":
        if (evt.code && TERMINAL_COPY[evt.code]) {
          finish("error");
          showInlineTerminal(evt.code);
          return;
        }
        failStream(evt.error || "The agent hit an error.");
        return;
      case "done":
        break;
      default:
        break;
    }
  }

  function updateBubbleText() {
    if (!state.bubble) return;
    var caret = state.bubble.querySelector(".share-caret");
    state.bubble.textContent = state.bubbleText;
    if (caret) state.bubble.appendChild(caret);
    scrollToBottom();
  }

  function failStream(message) {
    show(els.typing, false);
    if (state.bubble) {
      state.bubble.textContent = "";
      state.bubble.classList.add("border-error/40", "text-error");
      state.bubble.textContent = message;
    }
    finish("error");
  }

  function finish() {
    show(els.typing, false);
    if (state.bubble) {
      var caret = state.bubble.querySelector(".share-caret");
      if (caret) caret.remove();
    }
    state.bubble = null;
    setStreaming(false);
    refreshRail();
    loadQuestions();
    scrollToBottom();
  }

  function stop() {
    if (state.aborter) state.aborter.abort();
  }

  /* ---------- composer wiring ---------- */

  function resizeInput() {
    if (!els.input) return;
    els.input.style.height = "auto";
    els.input.style.height = Math.min(els.input.scrollHeight, 160) + "px";
  }

  if (els.form && els.input) {
    els.form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var text = els.input.value;
      els.input.value = "";
      resizeInput();
      send(text);
    });
    els.input.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
        ev.preventDefault();
        els.form.requestSubmit();
      }
    });
    els.input.addEventListener("input", resizeInput);
  }
  if (els.stop) els.stop.addEventListener("click", stop);

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
    if (action === "share-open-rail") { ev.preventDefault(); openRail(); }
    else if (action === "share-close-rail") { ev.preventDefault(); closeRail(); }
    else if (action === "share-new-chat") { ev.preventDefault(); newChat(); closeRail(); }
    else if (action === "share-suggestion") {
      ev.preventDefault();
      send(target.getAttribute("data-text") || "");
    }
  });

  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape" && root.getAttribute("data-rail-open") === "true") closeRail();
  });

  function newChat() {
    state.sessionId = "";
    if (els.messages) els.messages.replaceChildren();
    if (els.approvals) els.approvals.replaceChildren();
    show(els.inlineTerminal, false);
    disableComposer(false);
    show(els.firstLoad, true);
    if (els.input) els.input.focus();
    highlightActive();
  }

  if (els.filter) els.filter.addEventListener("change", loadSessions);

  /* ---------- boot ---------- */

  function boot() {
    var key = readAndScrubKey();
    if (key) {
      exchange(key).then(function (config) {
        applyConfig(config);
        return loadSessions();
      }).then(function () {
        if (state.sessionId) openSession(state.sessionId);
        loadQuestions();
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
      return loadSessions();
    }).then(function () {
      if (state.sessionId) openSession(state.sessionId);
      loadQuestions();
    }).catch(function (err) {
      showTerminal(err && err.code ? err.code : "invalid");
    });
  }

  boot();
})();
