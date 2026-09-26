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
  // The `border-radius` literals in this sheet are a documented exception to
  // the "radius derives from theme variables" rule — see the "Radius exception
  // list" note in webui/css/app.css. Migrating them to var(--radius-*) is
  // deferred to the stylesheet-consolidation follow-up unit (task 5.1).
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

  // Inject the run-control styles once. Mirrors ensureBadgeStyle: unlayered so
  // it sits above the daisyUI/Tailwind layers. The same rules live in
  // webui/css/app.css (the compiled source); this injection keeps the surfaces
  // styled even before the CSS bundle is rebuilt.
  //
  // The `border-radius` literals in this sheet are a documented exception to
  // the "radius derives from theme variables" rule — see the "Radius exception
  // list" note in webui/css/app.css. Migrating them to var(--radius-*) is
  // deferred to the stylesheet-consolidation follow-up unit (task 5.1).
  function ensureChatControlStyle() {
    if (document.getElementById("memory-chat-control-style")) return;
    var st = document.createElement("style");
    st.id = "memory-chat-control-style";
    st.textContent =
      /* live run status (chat header) */
      ".memory-run-status{display:inline-flex;align-items:center;gap:.4rem;padding:.2rem .6rem;" +
      "border-radius:9999px;border:1px solid transparent;font-size:.75rem;font-weight:500;white-space:nowrap}" +
      ".memory-run-status[data-state='working']{color:var(--color-base-content);" +
      "background:color-mix(in oklab,var(--color-primary) 10%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-primary) 28%,transparent)}" +
      ".memory-run-status[data-state='waiting']{color:var(--color-warning);" +
      "background:color-mix(in oklab,var(--color-warning) 12%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-warning) 34%,transparent)}" +
      ".memory-run-status[data-state='failed']{color:var(--color-error);" +
      "background:color-mix(in oklab,var(--color-error) 12%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-error) 34%,transparent)}" +
      ".memory-run-status-dot{width:.5rem;height:.5rem;border-radius:9999px;background:currentColor}" +
      ".memory-run-status[data-state='working'] .memory-run-status-dot{animation:memory-pulse 1.4s ease-in-out infinite}" +
      "@keyframes memory-pulse{0%,100%{opacity:.35}50%{opacity:1}}" +
      /* typed run markers */
      ".memory-run-marker{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem;margin:.35rem 0;" +
      "font-size:.6875rem;letter-spacing:.05em;text-transform:uppercase;color:color-mix(in oklab,var(--color-base-content) 42%,transparent)}" +
      ".memory-run-marker-line{flex:1 1 2rem;height:1px;background:color-mix(in oklab,var(--color-base-content) 10%,transparent)}" +
      ".memory-run-marker-icon{display:inline-flex}" +
      ".memory-run-marker-model{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;" +
      "text-transform:none;letter-spacing:0;font-size:.6875rem;padding:.05rem .4rem;border-radius:.375rem;" +
      "background:color-mix(in oklab,var(--color-base-content) 8%,transparent);color:color-mix(in oklab,var(--color-base-content) 60%,transparent)}" +
      ".memory-run-marker[data-status='failed']{display:block;margin:.5rem 0}" +
      ".memory-run-marker[data-status='input-required']{color:var(--color-warning)}" +
      ".memory-run-marker[data-status='cancelled']{color:color-mix(in oklab,var(--color-base-content) 55%,transparent)}" +
      ".memory-run-marker-failure{display:flex;align-items:flex-start;gap:.6rem;padding:.7rem .85rem;" +
      "border-radius:.6rem;border:1px solid color-mix(in oklab,var(--color-error) 42%,transparent);" +
      "border-left:3px solid var(--color-error);" +
      "background:color-mix(in oklab,var(--color-error) 18%,var(--color-base-100));" +
      "text-transform:none;letter-spacing:normal}" +
      ".memory-run-marker-failure-icon{display:inline-flex;flex:0 0 auto;margin-top:.05rem;color:var(--color-error)}" +
      ".memory-run-marker-failure-body{min-width:0}" +
      ".memory-run-marker-failure-title{margin:0;font-size:.8125rem;font-weight:600;line-height:1.3;color:var(--color-error)}" +
      ".memory-run-marker-failure-msg{margin:.2rem 0 0;font-size:.8125rem;line-height:1.45;white-space:pre-wrap;" +
      "word-break:break-word;color:color-mix(in oklab,var(--color-base-content) 80%,transparent)}" +
      /* turn footer */
      ".memory-turn-footer{display:flex;align-items:center;gap:.5rem;margin-top:.25rem;" +
      "font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.6875rem;" +
      "color:color-mix(in oklab,var(--color-base-content) 35%,transparent)}" +
      ".memory-turn-footer .memory-turn-time{max-width:0;overflow:hidden;opacity:0;white-space:nowrap;" +
      "transition:opacity .15s ease,max-width .2s ease}" +
      ".memory-turn-footer:hover .memory-turn-time,.memory-turn-footer:focus-within .memory-turn-time{max-width:12rem;opacity:1}" +
      /* copy affordances */
      ".memory-copy-btn{display:inline-flex;align-items:center;gap:.25rem;padding:.15rem;border-radius:.375rem;" +
      "color:color-mix(in oklab,var(--color-base-content) 40%,transparent);background:transparent;" +
      "border:1px solid transparent;cursor:pointer;opacity:0;" +
      "transition:opacity .15s ease,color .15s ease,background-color .15s ease}" +
      ".memory-copy-btn:hover{color:var(--color-base-content);background:color-mix(in oklab,var(--color-base-content) 8%,transparent)}" +
      ".memory-copy-btn:focus-visible{opacity:1;outline:2px solid var(--color-primary);outline-offset:2px}" +
      ".memory-copy-btn.memory-copy-done{opacity:1;color:var(--color-success)}" +
      ".memory-copy-msg{margin-left:.15rem;vertical-align:middle}" +
      ".chat-start:hover .memory-copy-msg,.chat-start:focus-within .memory-copy-msg," +
      ".memory-code-wrap:hover .memory-copy-code,.memory-code-wrap:focus-within .memory-copy-code{opacity:1}" +
      ".memory-code-wrap{position:relative;margin:.8em 0}" +
      ".memory-md .memory-code-wrap > pre{margin:0}" +
      ".memory-code-wrap .memory-copy-code{position:absolute;top:.4rem;right:.4rem;" +
      "background:color-mix(in oklab,var(--color-base-300) 80%,transparent)}" +
      "@media (hover:none){.memory-copy-btn{opacity:.55}}" +
      "@media (prefers-reduced-motion:reduce){.memory-run-status-dot{animation:none}}" +
      /* composer queue lane */
      ".memory-queue{border-bottom:1px solid color-mix(in oklab,var(--color-base-content) 8%,transparent);" +
      "background:color-mix(in oklab,var(--color-base-200) 45%,transparent)}" +
      ".memory-queue-head{display:flex;align-items:center;gap:.4rem;padding:.45rem .75rem .1rem;" +
      "font-size:.625rem;font-weight:600;letter-spacing:.09em;text-transform:uppercase;" +
      "color:color-mix(in oklab,var(--color-base-content) 45%,transparent)}" +
      ".memory-queue-list{display:flex;flex-direction:column;gap:.25rem;padding:0 .75rem .5rem}" +
      ".memory-queue-row{display:flex;align-items:flex-start;gap:.5rem;padding:.3rem .5rem;border-radius:.5rem;" +
      "border:1px solid color-mix(in oklab,var(--color-base-content) 10%,transparent);background:var(--color-base-100)}" +
      ".memory-queue-input{flex:1 1 auto;min-width:0;resize:none;background:transparent;border:0;outline:none;" +
      "color:inherit;font-size:.8125rem;line-height:1.45;max-height:7rem;overflow-y:auto}" +
      ".memory-queue-input:focus-visible{outline:2px solid color-mix(in oklab,var(--color-primary) 60%,transparent);outline-offset:2px;border-radius:.25rem}" +
      ".memory-queue-send,.memory-queue-remove{display:inline-flex;align-items:center;gap:.25rem;flex:0 0 auto;" +
      "padding:.2rem .5rem;border-radius:.375rem;border:1px solid transparent;background:transparent;cursor:pointer;" +
      "font-size:.6875rem;font-weight:600;color:color-mix(in oklab,var(--color-base-content) 55%,transparent)}" +
      ".memory-queue-send{color:var(--color-primary);border-color:color-mix(in oklab,var(--color-primary) 30%,transparent)}" +
      ".memory-queue-send:hover{background:color-mix(in oklab,var(--color-primary) 12%,transparent)}" +
      ".memory-queue-remove:hover{color:var(--color-error);background:color-mix(in oklab,var(--color-error) 12%,transparent)}" +
      ".memory-queue-send:focus-visible,.memory-queue-remove:focus-visible{outline:2px solid var(--color-primary);outline-offset:2px}" +
      /* pending-work dock */
      "#chat-dock{border-bottom:1px solid color-mix(in oklab,var(--color-base-content) 8%,transparent);" +
      "background:color-mix(in oklab,var(--color-base-200) 40%,transparent)}" +
      "#chat-dock :is(button,a,input,select,textarea):focus-visible{outline:2px solid var(--color-primary);outline-offset:2px}" +
      ".dock-head{display:flex;align-items:center;gap:.4rem;margin:.45rem .75rem .1rem;font-size:.625rem;" +
      "font-weight:600;letter-spacing:.09em;text-transform:uppercase;" +
      "color:color-mix(in oklab,var(--color-base-content) 65%,transparent)}" +
      ".dock-count{display:inline-flex;align-items:center;justify-content:center;min-width:1.05rem;height:1.05rem;" +
      "padding:0 .3rem;border-radius:9999px;background:var(--color-warning);color:var(--color-warning-content);font-size:.5625rem}" +
      ".dock-card-controls{display:flex;flex-basis:100%;flex-wrap:wrap;align-items:center;justify-content:flex-end;gap:.4rem}" +
      ".dock-approval-msg{flex:1 1 12rem;min-width:0;margin-right:auto}" +
      ".dock-question-input{flex-basis:100%}" +
      ".dock-card-options{display:flex;flex-basis:100%;flex-wrap:wrap;gap:.35rem;margin:.1rem 0 0;padding:0}" +
      ".dock-question-option{border:1px solid color-mix(in oklab,var(--color-base-content) 18%,transparent);" +
      "border-radius:.5rem;padding:.25rem .6rem;background:var(--color-base-100);color:inherit;font-size:.75rem;cursor:pointer}" +
      ".dock-question-option:hover{background:color-mix(in oklab,var(--color-base-content) 6%,transparent)}" +
      ".dock-question-option[aria-checked='true']{border-color:var(--color-primary);" +
      "background:color-mix(in oklab,var(--color-primary) 10%,transparent);color:var(--color-primary)}" +
      /* session todo card */
      "#chat-todos{margin-bottom:.25rem}" +
      "#chat-todos :is(summary,button,input,select,a):focus-visible{outline:2px solid var(--color-primary);outline-offset:2px}" +
      /* rail status badge */
      ".memory-rail-badge{display:flex;align-items:center;gap:.25rem;width:fit-content;padding:.1rem .4rem;border-radius:9999px;" +
      "font-size:.625rem;font-weight:600;letter-spacing:.04em;text-transform:uppercase;white-space:nowrap;" +
      "border:1px solid transparent;margin-bottom:.25rem}" +
      /* A two-class selector hides the badge: this sheet is appended to <head>
         after the compiled Tailwind sheet, so at equal specificity the injected
         `.memory-rail-badge { display:inline-flex }` beats Tailwind's
         `.hidden { display:none }`. Keep this rule identical to webui/css/app.css. */
      ".memory-rail-badge.hidden{display:none}" +
      ".memory-rail-badge[data-bucket='needs_input']{color:var(--color-warning);" +
      "background:color-mix(in oklab,var(--color-warning) 15%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-warning) 40%,transparent)}" +
      ".memory-rail-badge[data-bucket='failed']{color:color-mix(in oklab,var(--color-error) 60%,var(--color-base-content));" +
      "background:color-mix(in oklab,var(--color-error) 14%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-error) 38%,transparent)}" +
      ".memory-rail-badge[data-bucket='running']{color:var(--color-primary);" +
      "background:color-mix(in oklab,var(--color-primary) 13%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-primary) 34%,transparent)}" +
      ".memory-rail-badge[data-bucket='done']{color:color-mix(in oklab,var(--color-base-content) 65%,transparent);" +
      "background:color-mix(in oklab,var(--color-base-content) 7%,transparent);" +
      "border-color:color-mix(in oklab,var(--color-base-content) 12%,transparent)}" +
      ".memory-rail-badge .memory-rail-count{display:inline-flex;align-items:center;justify-content:center;" +
      "min-width:1rem;height:1rem;padding:0 .2rem;border-radius:9999px;font-size:.5625rem;" +
      "background:var(--color-base-content);color:var(--color-base-100)}" +
      ".memory-rail-badge[data-bucket='needs_input'] .memory-rail-count{background:var(--color-warning);" +
      "color:var(--color-warning-content)}" +
      ".memory-rail-badge[data-bucket='failed'] .memory-rail-count{background:var(--color-error);" +
      "color:var(--color-error-content)}" +
      ".memory-rail-badge[data-bucket='running'] .memory-rail-count{background:var(--color-primary);" +
      "color:var(--color-primary-content)}" +
      ".memory-rail-badge[data-bucket='done'] .memory-rail-count{background:var(--color-base-content);" +
      "color:var(--color-base-100)}";
    document.head.appendChild(st);
  }

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
    ensureChatControlStyle: ensureChatControlStyle,
  };
})();
