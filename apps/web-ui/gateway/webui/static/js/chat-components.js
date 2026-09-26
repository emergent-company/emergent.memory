/* Memory web UI — shared chat components (tool-call badges, thinking blocks).
   Loaded by BOTH the /chat page (chat.js) and the global side panel
   (sidepanel.js) so tool-call rendering is pixel-identical everywhere and
   there is exactly one implementation to keep in sync.

   Pure string builders (humanizeToolName, detail/error/summary sections,
   renderToolDetails) are self-contained. The DOM builders (expandableBadge,
   createThinkingBlock) take a ctx object { messages, hideEmpty,
   scrollToBottom } supplied by the host page's IIFE — each page owns its own
   message stream container, so nothing is shared mutable state. */
(function () {
  "use strict";

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function safeJSON(v) {
    try { return JSON.stringify(v, null, 2); } catch (e) { return String(v); }
  }

  // humanizeToolName turns a raw tool id ("schema-list-installed") into a quiet
  // one-line label ("Schema list installed"), matching Paseo's presentation.
  function humanizeToolName(name) {
    var t = String(name == null ? "" : name).trim();
    if (!t) return name;
    return t
      .replace(/[._-]+/g, " ")
      .replace(/\s+/g, " ")
      .trim()
      .toLowerCase()
      .replace(/^./, function (c) { return c.toUpperCase(); });
  }

  // formatDurationMs renders a tool call's execution duration compactly:
  // sub-second in "ms", then "1.2s", "5m", "2h 15m".
  function formatDurationMs(ms) {
    if (typeof ms !== "number" || !isFinite(ms) || ms < 0) return "";
    if (ms < 1000) return Math.round(ms) + "ms";
    var s = ms / 1000;
    if (s < 60) return (Math.round(s * 10) / 10) + "s";
    var m = Math.floor(s / 60);
    if (m < 60) return m + "m";
    return Math.floor(m / 60) + "h " + (m % 60) + "m";
  }

  // toolMetaLine summarises a tool call's identity/duration (id + execution
  // duration) as a quiet mono line above its I/O, or "" when neither exists.
  function toolMetaLine(p) {
    var parts = [];
    var dur = formatDurationMs(p && p.durationMs);
    if (dur) parts.push(dur);
    if (p && p.id) parts.push("#" + p.id);
    if (!parts.length) return "";
    return (
      '<p class="mb-3 font-mono text-[11px] break-all text-base-content/40">' +
      escapeHTML(parts.join(" · ")) +
      "</p>"
    );
  }

  // Inline detail for an expanded tool badge — the same sections the legacy
  // side panel used to show (identity/duration, summary / error / input /
  // output), now rendered directly under the row.
  function renderToolDetails(detailEl, payload) {
    var p = payload || {};
    var body = toolMetaLine(p);
    if (p.summary) body += summarySection(p.summary);
    if (p.error) body += errorSection(p.error);
    body += detailSection("Input", p.input, p.inputHtml);
    body += detailSection("Output", p.output, p.outputHtml);
    if (!body) {
      body = '<p class="text-base-content/40 text-sm">No details yet — the tool is still running.</p>';
    }
    detailEl.innerHTML = body;
  }

  function detailSection(label, value, html) {
    if (value === undefined || value === null) return "";
    // Server may pre-highlight JSON (tool I/O); when present it is trusted
    // (chroma escapes all content) and rendered verbatim. Otherwise fall back
    // to escaped pretty-printed JSON.
    var inner = html || (function () {
      var text = typeof value === "string" ? value : safeJSON(value);
      return escapeHTML(text);
    })();
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-base-content/50">' + label + "</p>" +
      '<pre class="memory-scroll max-h-72 overflow-auto rounded-box border border-base-content/10 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
      inner +
      "</pre>" +
      "</section>"
    );
  }

  function errorSection(err) {
    var text = typeof err === "string" ? err : safeJSON(err);
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-error">Error</p>' +
      '<pre class="memory-scroll max-h-72 overflow-auto rounded-box border border-error/20 bg-error/5 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
      escapeHTML(text) +
      "</pre>" +
      "</section>"
    );
  }

  function summarySection(text) {
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-base-content/50">Result</p>' +
      '<p class="text-sm text-base-content/90 leading-relaxed whitespace-pre-wrap break-words">' +
      escapeHTML(text) +
      "</p>" +
      "</section>"
    );
  }

  function rawSection(input, output) {
    var body = detailSection("Input", input) + detailSection("Output", output);
    if (!body) return "";
    return (
      '<details open class="collapse collapse-arrow border border-base-content/10 bg-base-200/30 rounded-box">' +
      '<summary class="collapse-title text-xs font-semibold tracking-wider uppercase text-base-content/50">Raw input / output</summary>' +
      '<div class="collapse-content">' + body + "</div>" +
      "</details>"
    );
  }

  /* ---------- expandable badges (thinking + tool calls) ---------- */

  // Shared builder for the Paseo-style expandable badge: a slim, quiet row
  // (small icon + dimmed label + optional dimmed secondary + chevron) that
  // expands inline to reveal detail. Collapsed = borderless and muted;
  // expanded = subtle border + background, label brightens, chevron flips.
  // Live (streaming) badges shimmer the label instead of spinning.
  function expandableBadge(cfg, ctx) {
    ctx = ctx || {};
    var messages = ctx.messages;
    if (!messages) return null;
    if (ctx.hideEmpty) ctx.hideEmpty();
    var scrollToBottom = ctx.scrollToBottom || function () {};

    var root = document.createElement("div");
    root.className = "memory-badge memory-rise" + (cfg.className ? " " + cfg.className : "");
    if (cfg.live) root.classList.add("memory-badge-live");
    if (cfg.dataStatus !== undefined) root.setAttribute("data-status", cfg.dataStatus);

    root.innerHTML =
      '<button type="button" class="memory-badge-toggle flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left" aria-expanded="false">' +
      '<span class="memory-badge-icon flex size-[22px] shrink-0 items-center justify-center">' +
      '<span class="iconify ' + cfg.icon + ' size-4 text-base-content/40" aria-hidden="true"></span></span>' +
      '<span class="memory-badge-label min-w-0 truncate text-xs text-base-content/50">' + escapeHTML(cfg.label) + "</span>" +
      (cfg.afterLabel || "") +
      '<span class="memory-badge-secondary hidden min-w-0 max-w-[40%] shrink truncate text-xs text-base-content/40"></span>' +
      '<span class="memory-badge-chevron ml-auto flex shrink-0 transition-transform duration-200">' +
      '<span class="iconify lucide--chevron-down size-3.5 text-base-content/40" aria-hidden="true"></span></span>' +
      "</button>" +
      '<div class="memory-badge-detail hidden"></div>';

    var toggle = root.querySelector(".memory-badge-toggle");
    var detail = root.querySelector(".memory-badge-detail");

    toggle.addEventListener("click", function () {
      var open = root.classList.toggle("memory-badge-open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
      if (open) {
        detail.classList.remove("hidden");
        if (cfg.renderDetails) cfg.renderDetails(detail, root);
        scrollToBottom();
      } else {
        detail.classList.add("hidden");
      }
    });

    (cfg.container || messages).appendChild(root);
    if (cfg.live) scrollToBottom();

    return {
      root: root,
      toggle: toggle,
      detail: detail,
      label: root.querySelector(".memory-badge-label"),
      secondary: root.querySelector(".memory-badge-secondary"),
      setLive: function (on) {
        root.classList.toggle("memory-badge-live", !!on);
      },
    };
  }

  // Inject the badge + shimmer CSS once. Unlayered, so it sits above the
  // daisyUI/Tailwind layers in app.css; runs after the stylesheet link, so it
  // also wins the cascade against app.css's unlayered tool-chip rules.
  //
  // This is the ONLY runtime-injected stylesheet, and it owns just the
  // expandable-badge shell + shimmer that app.css deliberately does not define.
  // Single source of truth: nothing here re-declares an app.css rule except the
  // two documented `.memory-tool-chip[data-status]` border overrides below. The
  // `.memory-badge` `border-radius: .625rem` literal is a documented JS-only
  // exception (no theme token maps to 0.625rem) — see the "Radius exception
  // list" note in webui/css/app.css.
  function ensureBadgeStyle() {
    if (document.getElementById("memory-badge-style")) return;
    var st = document.createElement("style");
    st.id = "memory-badge-style";
    st.textContent =
      ".memory-badge{" +
      "border:1px solid transparent;border-radius:.625rem;" +
      "transition:border-color .15s ease,background-color .15s ease}" +
      ".memory-badge:hover{background-color:color-mix(in oklab,var(--color-base-200) 45%,transparent)}" +
      ".memory-badge-open{" +
      "border-color:color-mix(in oklab,var(--color-base-content) 10%,transparent);" +
      "background-color:color-mix(in oklab,var(--color-base-200) 30%,transparent)}" +
      ".memory-badge-open .memory-badge-toggle{border-bottom-left-radius:0;border-bottom-right-radius:0}" +
      ".memory-badge-open .memory-badge-label{color:var(--color-base-content)}" +
      ".memory-badge-open .memory-badge-chevron{transform:rotate(180deg)}" +
      ".memory-badge-detail{" +
      "border-top:1px solid color-mix(in oklab,var(--color-base-content) 10%,transparent);" +
      "padding:.625rem .875rem .75rem}" +
      // thinking collapses the right chevron — its leading icon swaps brain→chevron on hover
      ".memory-thinking .memory-badge-chevron{display:none}" +
      // tool chips likewise: the leading icon swaps wrench→chevron on hover
      ".memory-tool-chip .memory-badge-chevron{display:none}" +
      // agent prompt card: leading icon swaps scroll→chevron on hover
      ".memory-agent-prompt .memory-badge-chevron{display:none}" +
      // auxiliary rows (thinking + tool calls) group tightly — reduce the gap
      // between consecutive aux rows while keeping the gap after a real bubble
      ".memory-aux + .memory-aux{margin-top:-.625rem}" +
      // app.css tints tool chips by status — keep the row borderless (status lives at the end)
      ".memory-tool-chip[data-status='ok'],.memory-tool-chip[data-status='error']{border-color:transparent!important}" +
      ".memory-tool-chip.memory-badge-open[data-status='ok'],.memory-tool-chip.memory-badge-open[data-status='error']{" +
      "border-color:color-mix(in oklab,var(--color-base-content) 10%,transparent)!important}" +
      // streaming shimmer: gradient sweep clipped to the label text
      // streaming shimmer: gradient sweep clipped to the label text. The bright
      // stop is a deliberate white — the theme has no "brighter than
      // base-content" token — so it is documented as decorative rather than
      // tokenised.
      ".memory-badge-live .memory-badge-label{" +
      "opacity:.72;color:transparent;" +
      "background-image:linear-gradient(90deg,var(--color-base-content) 40%," +
      "color-mix(in oklab,oklch(1 0 0) 50%,transparent) 50%,var(--color-base-content) 60%);" +
      "background-size:200% 100%;background-position:-100% 0;" +
      "-webkit-background-clip:text;background-clip:text;" +
      "animation:memory-shimmer 1.6s linear infinite}" +
      "@keyframes memory-shimmer{0%{background-position:-100% 0}100%{background-position:0 0}}" +
      "@media (prefers-reduced-motion:reduce){.memory-badge-live .memory-badge-label{animation:none}}";
    document.head.appendChild(st);
  }

  // Shared renderer for the agent's "Thinking" segments — a quiet, secondary
  // peek under the hood, visually distinct from the assistant's answer bubble.
  // Used by the live `thinking` SSE event (shimmer while streaming, raw text
  // accumulating in the detail panel) and by transcript history (collapsed +
  // static). Live segments are tracked by stable id so deltas append to the
  // right badge; history segments are one-shot and untracked.
  function createThinkingBlock(role, opts, ctx) {
    ctx = ctx || {};
    var messages = ctx.messages;
    if (!messages) return null;
    var live = !!(opts && opts.live);
    var roleTag = "";
    if (role && role !== "operator") {
      roleTag =
        '<span class="memory-thinking-role badge badge-ghost badge-xs font-normal">' +
        escapeHTML(role) +
        "</span>";
    }

    // Same chat-start shell as assistant bubbles so the badge aligns with (and
    // is constrained to) the chat content column.
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-aux";
    wrap.innerHTML =
      '<div class="chat-image invisible bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
      '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>';
    messages.appendChild(wrap);

    var badge = expandableBadge({
      className: "memory-thinking col-start-2 row-start-2 w-full",
      icon: "lucide--brain",
      label: "Thinking",
      afterLabel: roleTag,
      live: live,
      container: wrap,
    }, ctx);
    if (!badge) return null;
    // reasoning text lives here; history renders markdown, live appends text
    badge.detail.innerHTML =
      '<p class="memory-thinking-body font-mono text-xs leading-relaxed text-base-content/70 whitespace-pre-wrap break-words"></p>';

    // Hover affordance: swap the brain icon for a dropdown chevron (Paseo-style).
    var iconEl = badge.root.querySelector(".memory-badge-icon .iconify");
    if (badge.toggle) {
      badge.toggle.addEventListener("mouseenter", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--chevron-down size-4 text-base-content/40");
      });
      badge.toggle.addEventListener("mouseleave", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--brain size-4 text-base-content/40");
      });
    }

    return {
      details: badge.root,
      body: badge.detail.querySelector(".memory-thinking-body"),
      setLive: badge.setLive,
    };
  }

  // Shared renderer for a conversation's composed agent instruction — the
  // `system` record captured at the start of the run. Rendered as a collapsed,
  // tool-chip-styled card so the prompt the model saw is inspectable without
  // dominating the transcript. History-only: a live stream never carries it.
  function agentPromptCard(ctx, text) {
    ctx = ctx || {};
    var messages = ctx.messages;
    if (!messages || !text) return null;

    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-aux";
    wrap.innerHTML =
      '<div class="chat-image invisible bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
      '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>';
    messages.appendChild(wrap);

    var badge = expandableBadge({
      className: "memory-agent-prompt col-start-2 row-start-2 w-full",
      icon: "lucide--scroll-text",
      label: "Agent prompt",
      afterLabel:
        '<span class="badge badge-ghost badge-xs font-normal">system</span>',
      container: wrap,
      renderDetails: function (detailEl) {
        detailEl.innerHTML =
          '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-base-content/50">System instruction</p>' +
          '<pre class="memory-scroll max-h-72 overflow-auto rounded-box border border-base-content/10 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
          escapeHTML(text) +
          "</pre>";
      },
    }, ctx);
    if (!badge) return null;

    // Hover affordance: swap the scroll icon for a dropdown chevron.
    var iconEl = badge.root.querySelector(".memory-badge-icon .iconify");
    if (badge.toggle) {
      badge.toggle.addEventListener("mouseenter", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--chevron-down size-4 text-base-content/40");
      });
      badge.toggle.addEventListener("mouseleave", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--scroll-text size-4 text-base-content/40");
      });
    }
    return badge.root;
  }

  /* ---------- run-control surfaces (copy, footer, typed run markers) ---------- */

  function clipboardCopy(text) {
    if (window.MemoryChatHost && typeof window.MemoryChatHost.copyText === "function") {
      window.MemoryChatHost.copyText(text);
      return;
    }
    // Host module absent (should not happen — loaded on every chat surface):
    // keep the same navigator.clipboard + textarea fallback inline.
    var value = String(text);
    function copyTextFallback() {
      var ta = document.createElement("textarea");
      ta.value = value;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(ta);
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      try {
        var p = navigator.clipboard.writeText(value);
        if (p && typeof p.then === "function") {
          p.then(
            function () {},
            function () { copyTextFallback(); }
          );
        }
        return;
      } catch (e) { /* fall through to the textarea fallback */ }
    }
    copyTextFallback();
  }

  var COPY_ICON = '<span class="iconify lucide--copy size-3.5" aria-hidden="true"></span>';
  var COPIED_ICON = '<span class="iconify lucide--check size-3.5" aria-hidden="true"></span>';

  // makeCopyButton builds a compact copy affordance. It copies ONLY what the
  // getter returns — never surrounding markup — and confirms non-intrusively by
  // swapping the icon to a check for a moment (no toast, no layout shift).
  function makeCopyButton(opts) {
    var o = opts || {};
    var label = o.label || "Copy";
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = o.className ? "memory-copy-btn " + o.className : "memory-copy-btn";
    btn.setAttribute("aria-label", label);
    btn.title = label;
    btn.innerHTML = COPY_ICON;
    var timer = null;
    btn.addEventListener("click", function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      var text = typeof o.getText === "function" ? o.getText() : (o.text || "");
      clipboardCopy(text);
      if (timer) clearTimeout(timer);
      btn.classList.add("memory-copy-done");
      btn.setAttribute("aria-label", "Copied");
      btn.title = "Copied";
      btn.innerHTML = COPIED_ICON;
      timer = setTimeout(function () {
        btn.classList.remove("memory-copy-done");
        btn.setAttribute("aria-label", label);
        btn.title = label;
        btn.innerHTML = COPY_ICON;
      }, 1400);
    });
    return btn;
  }

  // enhanceMessage adds copy affordances to an already-rendered message block:
  // a whole-message copy button in the header row (never inside the bubble, so
  // reading flow and bubble layout are untouched) and a copy button on every
  // fenced code block. Idempotent: guarded per element, safe to re-run.
  function enhanceMessage(wrap) {
    if (!wrap) return;
    // Whole-message copy — assistant turns only (neutral bubble).
    var header = wrap.querySelector(".chat-header");
    var bubble = wrap.querySelector(".chat-bubble.chat-bubble-neutral");
    var body = bubble ? bubble.querySelector(".memory-md") : null;
    if (header && body && !header._memoryCopyWired) {
      header._memoryCopyWired = true;
      header.appendChild(makeCopyButton({
        className: "memory-copy-msg",
        label: "Copy message",
        getText: function () { return body.innerText || body.textContent || ""; },
      }));
    }
    // Per-code-block copy — the block's source only. Wrapping the <pre> keeps
    // the copy button out of the scroll area and out of the copied text;
    // moving the block margin to the wrapper preserves the original spacing.
    var pres = wrap.querySelectorAll(".memory-md pre");
    for (var i = 0; i < pres.length; i++) {
      var pre = pres[i];
      if (pre._memoryCopyWired) continue;
      pre._memoryCopyWired = true;
      var codeWrap = document.createElement("div");
      codeWrap.className = "memory-code-wrap";
      if (pre.parentNode) pre.parentNode.insertBefore(codeWrap, pre);
      codeWrap.appendChild(pre);
      (function (preEl) {
        codeWrap.appendChild(makeCopyButton({
          className: "memory-copy-code",
          label: "Copy code",
          getText: function () {
            var code = preEl.querySelector("code");
            return (code || preEl).textContent || "";
          },
        }));
      })(pre);
    }
  }

  // turnFooter renders the per-turn footer: model, wall-clock duration (only
  // when the run has ended), an end timestamp revealed on hover/focus, and a
  // copy-turn action. opts = {model, durationMs, endTime, getText}.
  function turnFooter(opts) {
    var o = opts || {};
    var el = document.createElement("div");
    el.className = "memory-turn-footer chat-footer";
    var duration = (typeof o.durationMs === "number") ? (window.MemoryChatHost ? window.MemoryChatHost.formatDuration(o.durationMs) : "") : "";
    var endClock = o.endTime ? formatClock(o.endTime) : "";
    var html = "";
    if (o.model) html += '<span class="memory-turn-model">' + escapeHTML(o.model) + "</span>";
    if (duration !== "") {
      html += '<span class="memory-turn-dur">' + escapeHTML(duration) + "</span>";
    }
    if (endClock) {
      html += '<span class="memory-turn-time" title="' + escapeHTML(o.endTime) + '">' + escapeHTML(endClock) + "</span>";
    }
    el.innerHTML = html;
    if (typeof o.getText === "function") {
      el.appendChild(makeCopyButton({
        className: "memory-copy-turn",
        label: "Copy turn",
        getText: o.getText,
      }));
    }
    return el;
  }

  function formatClock(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return "";
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }

  // runMarker renders a typed run boundary for a run_start/run_end timeline item
  // instead of generic content. opts = {phase, status, model, error}.
  function runMarker(opts) {
    var o = opts || {};
    var phase = o.phase === "start" ? "start" : "end";
    var status = o.status || "";
    var icon = "lucide--play";
    var label = "Run started";
    if (phase === "end") {
      if (status === "failed" || status === "error") { icon = "lucide--circle-alert"; label = "Run failed"; }
      else if (status === "input-required") { icon = "lucide--pause"; label = "Waiting on you"; }
      else if (status === "cancelled" || status === "cancelling") { icon = "lucide--ban"; label = "Run cancelled"; }
      else if (status === "completed" || status === "skipped" || status === "") { icon = "lucide--circle-check"; label = "Run complete"; }
      else if (status === "working" || status === "submitted") { icon = "lucide--loader-circle"; label = "Running"; }
      else { icon = "lucide--circle-dot"; label = "Run ended"; }
    }
    var el = document.createElement("div");
    el.className = "memory-run-marker";
    el.setAttribute("data-phase", phase);
    el.setAttribute("data-status", status);
    // A failed run is not a faint divider line — it is a full-width alert
    // banner (daisyUI alert-error equivalent): an alert icon, a "Run failed"
    // heading, and the error message. role=alert makes screen readers announce
    // it the moment it mounts, so a failure is impossible to overlook.
    if (phase === "end" && (status === "failed" || status === "error")) {
      el.setAttribute("role", "alert");
      var banner = document.createElement("div");
      banner.className = "memory-run-marker-failure";
      banner.innerHTML =
        '<span class="memory-run-marker-failure-icon">' +
        '<span class="iconify lucide--circle-alert size-4" aria-hidden="true"></span></span>' +
        '<div class="memory-run-marker-failure-body">' +
        '<p class="memory-run-marker-failure-title">Run failed</p>' +
        '<p class="memory-run-marker-failure-msg">' + escapeHTML(o.error || "The agent run failed.") + "</p>" +
        "</div>";
      el.appendChild(banner);
      return el;
    }
    var modelChip = (phase === "start" && o.model)
      ? '<span class="memory-run-marker-model">' + escapeHTML(o.model) + "</span>"
      : "";
    el.innerHTML =
      '<span class="memory-run-marker-line" aria-hidden="true"></span>' +
      '<span class="memory-run-marker-icon"><span class="iconify ' + icon + ' size-3.5" aria-hidden="true"></span></span>' +
      '<span class="memory-run-marker-label">' + escapeHTML(label) + "</span>" +
      modelChip +
      '<span class="memory-run-marker-line" aria-hidden="true"></span>';
    return el;
  }

  /* ---------- A2UI declarative cards ---------- */

  // A2UI (v0.9.1) surface renderer. The agent streams declarative "cards" via
  // the gateway's `ui` SSE event; this walks the message list, accumulates
  // components from updateComponents, and renders one card per component into
  // the chat message list (ctx.messages — the same container question/approval
  // cards append to). createSurface / updateDataModel / deleteSurface are
  // tolerated (ignored for now — this is a Phase-1 read-only render). Unknown
  // component ids fall back to a summary card and never throw. All user/model
  // text is escaped (textContent / escapeHTML), never injected via innerHTML.

  // Bot avatar byte-identical to the assistant-bubble fallback (chat-stream.js
  // agentAvatarHTML with no agent appearance), so A2UI cards align with chat.
  var A2UI_AVATAR =
    '<div class="chat-image bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
    '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>';

  function renderA2UISurface(surfaceId, messages, ctx) {
    ctx = ctx || {};
    var container = ctx.messages;
    if (!container) return;
    var hideEmpty = ctx.hideEmpty;
    var scrollToBottom = ctx.scrollToBottom || function () {};

    // Accumulate components from updateComponents; ignore other envelope kinds.
    var components = [];
    var list = Array.isArray(messages) ? messages : [];
    for (var i = 0; i < list.length; i++) {
      var msg = list[i];
      if (!msg || typeof msg !== "object") continue;
      var upd = msg.updateComponents;
      if (upd && Array.isArray(upd.components)) {
        for (var j = 0; j < upd.components.length; j++) {
          if (upd.components[j]) components.push(upd.components[j]);
        }
      }
    }

    for (var k = 0; k < components.length; k++) {
      var node = buildA2UICard(components[k], surfaceId);
      if (node) container.appendChild(node);
    }
    if (hideEmpty) hideEmpty();
    scrollToBottom();
  }

  function buildA2UICard(comp, surfaceId) {
    switch (comp && comp.component) {
      case "proposal": return a2uiProposal(comp, surfaceId);
      case "approval": return a2uiApproval(comp, surfaceId);
      case "question": return a2uiQuestion(comp, surfaceId);
      case "code": return a2uiCode(comp, surfaceId);
      case "entity": return a2uiEntity(comp, surfaceId);
      case "object-form": return a2uiObjectForm(comp, surfaceId);
      case "todo": return a2uiTodo(comp, surfaceId);
      case "result": return a2uiResult(comp, surfaceId);
      default: return a2uiSummary(comp, surfaceId);
    }
  }

  // a2uiShell builds the chat-grid wrapper + card shell every card lives in:
  // the same `chat chat-start` + `card` structure the question/approval cards
  // use, so A2UI cards read as the agent speaking.
  function a2uiShell() {
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise";
    wrap.innerHTML = A2UI_AVATAR;
    var card = document.createElement("div");
    card.className =
      "card card-border bg-base-100 col-start-2 row-start-2 w-fit min-w-[min(100%,30rem)] max-w-[85%] shadow-sm";
    wrap.appendChild(card);
    var body = document.createElement("div");
    body.className = "card-body gap-3 p-4";
    card.appendChild(body);
    return { wrap: wrap, card: card, body: body };
  }

  // a2uiText coerces any prop value to a display string (objects → pretty JSON).
  function a2uiText(v) {
    if (v === undefined || v === null) return "";
    if (typeof v === "string") return v;
    if (typeof v === "number" || typeof v === "boolean") return String(v);
    return safeJSON(v);
  }

  function a2uiHeader(title, badge) {
    var h = document.createElement("div");
    h.className = "flex items-center gap-2";
    if (badge) {
      var b = document.createElement("span");
      b.className = "badge badge-sm badge-outline";
      b.textContent = badge;
      h.appendChild(b);
    }
    var t = document.createElement("span");
    t.className = "text-xs uppercase tracking-wider text-base-content/50";
    t.textContent = title;
    h.appendChild(t);
    return h;
  }

  function a2uiLabel(text) {
    var p = document.createElement("p");
    p.className = "text-sm text-base-content/90 leading-relaxed whitespace-pre-wrap break-words";
    p.textContent = text == null ? "" : String(text);
    return p;
  }

  function a2uiSectionLabel(text) {
    var p = document.createElement("p");
    p.className = "mb-0 mt-1 text-xs font-semibold tracking-wider uppercase text-base-content/50";
    p.textContent = text;
    return p;
  }

  function a2uiPre(value) {
    var pre = document.createElement("pre");
    pre.className =
      "memory-scroll max-h-72 overflow-auto rounded-lg bg-base-200/60 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words";
    pre.textContent = typeof value === "string" ? value : safeJSON(value);
    return pre;
  }

  // a2uiRows renders [[label, value], ...] as stacked key/value rows.
  function a2uiRows(rows) {
    var list = document.createElement("div");
    list.className = "flex flex-col gap-2";
    for (var i = 0; i < rows.length; i++) {
      var pair = rows[i];
      var row = document.createElement("div");
      row.className = "flex flex-col";
      var key = document.createElement("span");
      key.className = "text-[10px] font-semibold tracking-wider uppercase text-base-content/50";
      key.textContent = pair[0];
      var val = document.createElement("span");
      val.className = "text-sm text-base-content/90 leading-relaxed break-words whitespace-pre-wrap";
      val.textContent = pair[1];
      row.appendChild(key);
      row.appendChild(val);
      list.appendChild(row);
    }
    return list;
  }

  function a2uiButton(label, action, primary) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = primary ? "btn btn-primary btn-sm" : "btn btn-ghost btn-sm";
    btn.textContent = label;
    btn.addEventListener("click", function () {
      // TODO(agent-structured-ui): wire resume/respond — for now a click just
      // emits an `a2ui:action` CustomEvent so a follow-up can send the decision
      // back to the agent (resume) / server (respond). No endpoint is invented.
      document.dispatchEvent(new CustomEvent("a2ui:action", {
        detail: { surfaceId: surfaceId, action: action },
      }));
    });
    return btn;
  }

  function a2uiActions(surfaceId, comp, pairs) {
    // pairs = [[label, responseValue, primary], ...]
    var row = document.createElement("div");
    row.className = "flex items-center justify-end gap-2 pt-1";
    for (var i = 0; i < pairs.length; i++) {
      row.appendChild(a2uiButton(
        pairs[i][0],
        { componentId: comp.id, response: pairs[i][1] },
        !!pairs[i][2]
      ));
    }
    return row;
  }

  function a2uiProposal(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Proposal", comp.kind || "proposal"));
    if (comp.summary != null && comp.summary !== "") {
      shell.body.appendChild(a2uiLabel(comp.summary));
    }
    if (comp.body !== undefined && comp.body !== null) {
      shell.body.appendChild(a2uiPre(comp.body));
    }
    shell.body.appendChild(a2uiActions(surfaceId, comp, [
      ["Reject", "reject", false],
      ["Accept", "accept", true],
    ]));
    return shell.wrap;
  }

  function a2uiApproval(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Approval", comp.tool || "tool"));
    if (comp.input !== undefined && comp.input !== null) {
      shell.body.appendChild(a2uiPre(comp.input));
    }
    shell.body.appendChild(a2uiActions(surfaceId, comp, [
      ["Deny", "deny", false],
      ["Approve", "approve", true],
    ]));
    return shell.wrap;
  }

  function a2uiQuestion(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Question", null));
    if (comp.prompt != null && comp.prompt !== "") {
      shell.body.appendChild(a2uiLabel(comp.prompt));
    }
    var options = comp.options;
    if (Array.isArray(options) && options.length) {
      var opts = document.createElement("div");
      opts.className = "flex flex-wrap gap-2";
      for (var i = 0; i < options.length; i++) {
        var o = options[i];
        var isObj = o && typeof o === "object";
        var val = isObj ? (o.value != null ? o.value : o.label) : o;
        var label = isObj ? (o.label != null ? o.label : o.value) : o;
        opts.appendChild(a2uiButton(
          label == null ? "" : String(label),
          { componentId: comp.id, response: val },
          false
        ));
      }
      shell.body.appendChild(opts);
    }
    return shell.wrap;
  }

  function a2uiCode(comp, surfaceId) {
    var shell = a2uiShell();
    if (comp.lang) shell.body.appendChild(a2uiSectionLabel(comp.lang));
    var pre = document.createElement("pre");
    pre.className =
      "memory-scroll max-h-96 overflow-auto rounded-lg bg-base-200/60 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words";
    var code = document.createElement("code");
    code.textContent = comp.code == null ? "" : String(comp.code);
    pre.appendChild(code);
    shell.body.appendChild(pre);
    return shell.wrap;
  }

  function a2uiEntity(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Entity", comp.type || "entity"));
    var props = comp.properties;
    if (props && typeof props === "object" && !Array.isArray(props)) {
      var rows = [];
      for (var key in props) {
        if (Object.prototype.hasOwnProperty.call(props, key)) {
          rows.push([key, a2uiText(props[key])]);
        }
      }
      if (rows.length) {
        shell.body.appendChild(a2uiSectionLabel("Properties"));
        shell.body.appendChild(a2uiRows(rows));
      }
    }
    var rels = comp.relationships;
    if (Array.isArray(rels) && rels.length) {
      shell.body.appendChild(a2uiSectionLabel("Relationships"));
      var relRows = [];
      for (var r = 0; r < rels.length; r++) {
        var rel = rels[r];
        if (typeof rel === "string") {
          relRows.push([rel, ""]);
          continue;
        }
        var relObj = rel && typeof rel === "object" ? rel : {};
        relRows.push([
          relObj.relation || relObj.type || relObj.label || "related",
          relObj.target || relObj.entity || relObj.id || "",
        ]);
      }
      shell.body.appendChild(a2uiRows(relRows));
    }
    return shell.wrap;
  }

  function a2uiObjectForm(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Form", null));
    // TODO(agent-structured-ui): render an editable form from comp's schema/fields.
    shell.body.appendChild(a2uiLabel("Structured input requested — form rendering is not implemented yet."));
    return shell.wrap;
  }

  function a2uiTodo(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Tasks", null));
    var items = comp.items || comp.todos;
    if (Array.isArray(items) && items.length) {
      var list = document.createElement("ul");
      list.className = "flex flex-col gap-1.5";
      for (var i = 0; i < items.length; i++) {
        var item = items[i];
        var isObj = item && typeof item === "object";
        var text = isObj ? (item.label != null ? item.label : item.text) : item;
        var done = isObj && !!item.done;
        var li = document.createElement("li");
        li.className = "flex items-center gap-2";
        var cb = document.createElement("input");
        cb.type = "checkbox";
        cb.className = "checkbox checkbox-xs";
        cb.checked = done;
        cb.disabled = true; // read-only for now
        var span = document.createElement("span");
        span.className = "text-sm text-base-content/90 break-words";
        span.textContent = a2uiText(text);
        li.appendChild(cb);
        li.appendChild(span);
        list.appendChild(li);
      }
      shell.body.appendChild(list);
    }
    return shell.wrap;
  }

  function a2uiResult(comp, surfaceId) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Result", null));
    var rows = [];
    for (var key in comp) {
      if (!Object.prototype.hasOwnProperty.call(comp, key)) continue;
      if (key === "id" || key === "component") continue;
      rows.push([key, a2uiText(comp[key])]);
    }
    if (!rows.length && comp.result !== undefined) {
      rows.push(["result", a2uiText(comp.result)]);
    }
    if (!rows.length) rows.push(["result", "—"]);
    shell.body.appendChild(a2uiRows(rows));
    return shell.wrap;
  }

  function a2uiSummary(comp, surfaceId) {
    var shell = a2uiShell();
    var name = (comp && comp.component) ? comp.component : "component";
    shell.body.appendChild(a2uiHeader(name, null));
    var copy = {};
    for (var key in comp) {
      if (Object.prototype.hasOwnProperty.call(comp, key) && key !== "id" && key !== "component") {
        copy[key] = comp[key];
      }
    }
    shell.body.appendChild(a2uiPre(safeJSON(copy)));
    return shell.wrap;
  }

  // Phase-1 action stub: card buttons emit an `a2ui:action` CustomEvent
  // (detail: {surfaceId, action}); this listener currently just logs it. The
  // real resume/respond wiring (POSTing the decision back to the gateway/agent)
  // is a follow-up.
  document.addEventListener("a2ui:action", function (evt) {
    // TODO(agent-structured-ui): send evt.detail (surfaceId + action) to the
    // server to resume/respond to the paused agent run.
    console.log("a2ui:action", evt.detail);
  });

  window.MemoryChatComponents = {
    escapeHTML: escapeHTML,
    humanizeToolName: humanizeToolName,
    renderToolDetails: renderToolDetails,
    detailSection: detailSection,
    errorSection: errorSection,
    summarySection: summarySection,
    rawSection: rawSection,
    expandableBadge: expandableBadge,
    ensureBadgeStyle: ensureBadgeStyle,
    createThinkingBlock: createThinkingBlock,
    agentPromptCard: agentPromptCard,
    formatDurationMs: formatDurationMs,
    makeCopyButton: makeCopyButton,
    enhanceMessage: enhanceMessage,
    turnFooter: turnFooter,
    runMarker: runMarker,
    renderA2UISurface: renderA2UISurface,
  };
})();
