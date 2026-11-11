/* Memory web UI — shared chat components (timeline renderer, tool-call badges,
   thinking blocks, A2UI cards). Loaded by the /chat page (chat.js), the global
   side panel (sidepanel.js) and the public share page (share-agent.js) so the
   transcript is pixel-identical everywhere and there is exactly one
   implementation to keep in sync.

   Pure string builders (humanizeToolName, detail/error/summary sections,
   renderToolDetails) are self-contained. The DOM builders (expandableBadge,
   createThinkingBlock) take a ctx object { messages, hideEmpty,
   scrollToBottom } supplied by the host page's IIFE — each page owns its own
   message stream container, so nothing is shared mutable state.
   createThinkingHost(createThinkingBlock + the live thinking segment map) is
   instantiated once per page for the same reason. */
(function () {
  "use strict";

  /* Scoped-path helper for JS-built URLs. app.js defines window.MemoryScope on
     the shell; fall back to the raw path when this script is loaded standalone
     (the hermetic js-dom specs) or app.js is absent, so a missing global never
     throws a ReferenceError. */
  function scopedPath(p) {
    return window.MemoryScope && typeof window.MemoryScope.path === "function"
      ? window.MemoryScope.path(p)
      : p;
  }

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

  // toolLabel is what a step calls itself: the category's verb, then the subject
  // the call's arguments named, e.g. "Searched memory · swiftui". Both come from
  // the server (tool_category / tool_subject) so the live chip and the recorded
  // row cannot word the same call differently; without them it falls back to the
  // de-slugged wire name, which is all an older server offers.
  function toolLabel(category, name, running, subject) {
    var label = "";
    if (category && typeof category === "object") {
      var verb = running ? category.runningVerb : category.verb;
      if (typeof verb === "string") label = verb;
    }
    if (!label) label = humanizeToolName(name) || String(name || "tool");
    var said = typeof subject === "string" ? subject.trim() : "";
    return said ? label + " · " + said : label;
  }

  // toolIcon is the family's glyph. Colour is reserved for state, so the icon is
  // what tells one kind of work from another.
  function toolIcon(category) {
    if (category && typeof category.icon === "string" && category.icon) return category.icon;
    return "lucide--wrench";
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

  // utf8ByteLength returns the UTF-8 byte length of a string — the wire size a
  // payload occupies, not its UTF-16 length. TextEncoder is available in every
  // browser this app targets; the percent-encoding fallback keeps the helper
  // honest in a non-browser engine.
  function utf8ByteLength(s) {
    var str = String(s == null ? "" : s);
    if (typeof TextEncoder !== "undefined") {
      return new TextEncoder().encode(str).length;
    }
    var encoded = encodeURIComponent(str);
    // Each %XX escape stands for one byte but occupies three characters, so a
    // byte is over-counted by 2 per escape; every other character is one byte.
    var escapes = (encoded.match(/%[0-9A-Fa-f]{2}/g) || []).length;
    return encoded.length - escapes * 2;
  }

  // formatBytes renders a byte count compactly: "512 B", "1.2 KB", "3.4 MB".
  function formatBytes(n) {
    if (typeof n !== "number" || !isFinite(n) || n < 0) return "";
    if (n < 1024) return n + " B";
    var kb = n / 1024;
    if (kb < 1024) return (Math.round(kb * 10) / 10) + " KB";
    var mb = kb / 1024;
    if (mb < 1024) return (Math.round(mb * 10) / 10) + " MB";
    return (Math.round((mb / 1024) * 10) / 10) + " GB";
  }

  // outputByteSize measures a tool call's OUTPUT payload client-side: the
  // serialized form of p.output (pretty JSON when it is not a string, matching
  // detailSection's rendering), falling back to p.outputHtml. Returns null when
  // the payload is absent so the size label can be omitted.
  function outputByteSize(p) {
    if (!p) return null;
    var raw;
    if (p.output !== undefined && p.output !== null) {
      raw = typeof p.output === "string" ? p.output : safeJSON(p.output);
    } else if (p.outputHtml !== undefined && p.outputHtml !== null) {
      raw = typeof p.outputHtml === "string" ? p.outputHtml : safeJSON(p.outputHtml);
    } else {
      return null;
    }
    return utf8ByteLength(raw);
  }

  // toolMetaLine summarises a tool call's identity, execution duration and
  // response size as a quiet labelled mono line above its I/O, or "" when none
  // exists. Labels ("Duration", "response") make the bare numbers readable.
  function toolMetaLine(p) {
    var parts = [];
    var dur = formatDurationMs(p && p.durationMs);
    if (dur) parts.push("Duration " + dur);
    var size = formatBytes(outputByteSize(p));
    if (size) parts.push(size + " response");
    if (p && p.id) parts.push("#" + p.id);
    if (!parts.length) return "";
    return (
      '<p class="mb-3 font-mono text-[11px] break-all text-muted-faint">' +
      escapeHTML(parts.join(" · ")) +
      "</p>"
    );
  }

  // Inline detail for an expanded tool badge — the same sections the legacy
  // side panel used to show (identity/duration, summary / error / input /
  // output), now rendered directly under the row. When the server sent a view of
  // the result, the readable body leads and the raw payload moves behind its own
  // closed disclosure.
  function renderToolDetails(detailEl, payload) {
    var p = payload || {};
    var view = p.view && typeof p.view === "object" ? p.view : null;
    // A call waiting for approval has no result to show: the executor's pause
    // sentinel is not one. Its arguments are still worth reading, so they lead.
    if (p.status === "awaiting_confirmation") {
      // No meta line either: "129 B response" is a lie about a call that never ran.
      detailEl.innerHTML =
        detailSection("Input", p.input, p.inputHtml) +
        '<p class="text-sm text-muted">Waiting for confirmation — the tool has not run yet.</p>';
      return;
    }
    var body = toolMetaLine(p);
    if (p.summary && !view) body += summarySection(p.summary);
    if (p.error) body += errorSection(p.error);
    if (view) body += toolBodySection(view);
    body += detailSection("Input", p.input, p.inputHtml);
    if (view) {
      // A result with no readable shape has no payload to put one tap away: its
      // text is the whole answer, so it is shown directly. Both paths keep the
      // server's highlighted output when present (#2452) — the same chroma
      // fragment Input renders through inputHtml.
      body += hasReadableBody(view)
        ? rawPayloadSection(view, p.tool, p.outputHtml)
        : rawTextSection(view, p.tool, p.outputHtml);
    } else {
      body += detailSection("Output", p.output, p.outputHtml);
    }
    if (!body) {
      body = '<p class="text-muted-faint text-sm">No details yet — the tool is still running.</p>';
    }
    detailEl.innerHTML = body;
  }

  // hasReadableBody reports whether a view holds something to read beyond its raw
  // payload. The server sends a view for every settled result (it also carries the
  // bounded raw text), so "kind" is what distinguishes a body from bare JSON.
  function hasReadableBody(view) {
    return !!view && typeof view.kind === "string" && view.kind !== "";
  }

  // toolBodySection renders the readable part of a result: a list of records, one
  // record's fields, a body of text, or an empty list's copy. A batch create's
  // tally leads, because its list keeps only the records that were created
  // (#2277). Nothing is rendered when the payload has no readable shape — the raw
  // disclosure is the answer there.
  function toolBodySection(view) {
    var more = remainderLine(view);
    var tally = batchTallyLine(view && view.batch);
    switch (view && view.kind) {
      case "records":
        return tally + recordsSection(view.items, more);
      case "fields":
        return tally + fieldsSection(view.fields, more);
      case "text":
        return tally + textBodySection(view.text, more);
      case "empty":
        return tally + emptySection(view.text);
      default:
        return tally;
    }
  }

  // batchTallyLine shows a batch create's counts above its records. A failed item
  // marks the line serious — warning tone and glyph — so a half-failed batch never
  // reads as a clean list (#2277). The wording arrives from the server, the same
  // line the recorded-session page draws.
  function batchTallyLine(batch) {
    if (!batch || typeof batch !== "object" || typeof batch.text !== "string" || !batch.text) return "";
    var failed = typeof batch.failed === "number" ? batch.failed : 0;
    var bad = failed > 0;
    return (
      '<p class="mb-2 flex items-center gap-1 text-xs' + (bad ? " font-medium text-warning" : " text-muted") + '" data-testid="tool-batch-tally"' + (bad ? ' data-batch-failure="true"' : "") + ">" +
      (bad ? '<span class="iconify lucide--triangle-alert size-3.5" aria-hidden="true"></span>' : "") +
      escapeHTML(batch.text) +
      "</p>"
    );
  }

  // emptySection renders a result whose list is empty: the copy is the whole
  // answer, and the envelope stays behind the raw disclosure (#2277).
  function emptySection(text) {
    if (typeof text !== "string" || !text) return "";
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      '<p class="text-sm text-muted" data-testid="tool-result-empty">' + escapeHTML(text) + "</p>" +
      "</section>"
    );
  }

  // remainderLine words what the caps hid, so a prefix never reads as the whole.
  function remainderLine(view) {
    var parts = [];
    function add(n, one, many) {
      if (typeof n !== "number" || n <= 0) return;
      parts.push(n + " " + (n === 1 ? one : many));
    }
    add(view && view.moreRecords, "more record", "more records");
    add(view && view.moreFields, "more field", "more fields");
    add(view && view.moreLines, "more line", "more lines");
    add(view && view.moreCharacters, "character cut", "characters cut");
    return parts.join(" · ");
  }

  function remainderParagraph(text) {
    if (!text) return "";
    return '<p class="mt-2 font-mono text-[11px] text-muted-faint">' + escapeHTML(text) + "</p>";
  }

  function recordsSection(items, more) {
    if (!Array.isArray(items) || !items.length) return "";
    var rows = "";
    for (var i = 0; i < items.length; i++) {
      var item = items[i] || {};
      var title = String(item.title == null ? "" : item.title);
      var href = httpURL(item.link);
      var head = href
        ? '<a class="link link-hover" href="' + escapeHTML(href) + '" target="_blank" rel="noopener noreferrer">' + escapeHTML(title) + "</a>"
        : '<span class="text-base-content">' + escapeHTML(title) + "</span>";
      var host = href ? '<span class="ml-1 font-mono text-[11px] text-muted-faint">' + escapeHTML(hostOf(href)) + "</span>" : "";
      var detail = item.detail
        ? '<p class="mt-0.5 text-xs text-muted leading-relaxed whitespace-pre-wrap break-words">' + escapeHTML(String(item.detail)) + "</p>"
        : "";
      rows += '<li class="py-1.5 border-b border-base-content/5 last:border-b-0">' + head + host + detail + "</li>";
    }
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      '<ul class="text-sm leading-relaxed">' + rows + "</ul>" +
      remainderParagraph(more) +
      "</section>"
    );
  }

  function fieldsSection(fields, more) {
    if (!Array.isArray(fields) || !fields.length) return "";
    var rows = "";
    for (var i = 0; i < fields.length; i++) {
      var field = fields[i] || {};
      rows +=
        '<div class="grid grid-cols-[minmax(6rem,auto)_1fr] gap-2 py-1 border-b border-base-content/5 last:border-b-0">' +
        '<dt class="font-mono text-[11px] text-muted-faint break-all">' + escapeHTML(String(field.label == null ? "" : field.label)) + "</dt>" +
        '<dd class="text-sm text-base-content break-words whitespace-pre-wrap">' + escapeHTML(String(field.value == null ? "" : field.value)) + "</dd>" +
        "</div>";
    }
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      '<dl class="text-sm leading-relaxed">' + rows + "</dl>" +
      remainderParagraph(more) +
      "</section>"
    );
  }

  function textBodySection(text, more) {
    if (typeof text !== "string" || !text) return "";
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      '<pre class="memory-scroll max-h-72 overflow-auto rounded-box border border-base-content/10 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
      escapeHTML(text) +
      "</pre>" +
      remainderParagraph(more) +
      "</section>"
    );
  }

  // rawPayloadSection is the payload behind one closed disclosure: the step's wire
  // name, the bounded raw JSON the server already prepared, and how much of it did
  // not fit. A native <details> is the control — keyboard operable, and it reports
  // its own state — and min-h-11 keeps its target at 44 CSS pixels.
  function rawPayloadSection(view, tool, html) {
    // The disclosure may render before (or without) the badge shell that normally
    // injects these styles — an expanded step in a replayed transcript, the share
    // page — so it asks for them itself. ensureBadgeStyle is idempotent.
    ensureBadgeStyle();
    var raw = view && typeof view.raw === "string" ? view.raw : "";
    if (!raw) return "";
    var name = typeof tool === "string" && tool ? tool : "";
    // The server highlight covers the whole output, so the bounded raw text's
    // remainder note no longer applies when it is used (#2452).
    var highlighted = typeof html === "string" && html !== "";
    return (
      '<details class="memory-raw-disclosure mt-4 rounded-box border border-base-content/10 bg-base-200/30">' +
      '<summary class="memory-raw-summary flex cursor-pointer items-center gap-2 px-3 text-xs font-semibold tracking-wider uppercase text-muted">' +
      "<span>Raw payload</span>" +
      '<span class="iconify lucide--chevron-down memory-raw-chevron ml-auto size-4 text-muted-faint" aria-hidden="true"></span>' +
      "</summary>" +
      (name ? '<p class="px-3 pb-1 font-mono text-[11px] text-muted-faint break-all">' + escapeHTML(name) + "</p>" : "") +
      '<pre class="memory-scroll max-h-72 overflow-auto px-3 pb-2 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
      (highlighted ? html : escapeHTML(raw)) +
      "</pre>" +
      (highlighted ? "" : remainderParagraph(rawRemainder(view))) +
      "</details>"
    );
  }

  // rawRemainder words what the raw cap hid, so both the disclosure and the
  // directly-shown payload say it the same way.
  function rawRemainder(view) {
    var parts = [];
    if (view && typeof view.rawMoreLines === "number" && view.rawMoreLines > 0) {
      parts.push(view.rawMoreLines + " more " + (view.rawMoreLines === 1 ? "line" : "lines"));
    }
    if (view && typeof view.rawMoreCharacters === "number" && view.rawMoreCharacters > 0) {
      parts.push(view.rawMoreCharacters + " characters cut");
    }
    return parts.join(" · ");
  }

  // rawTextSection shows a no-shape result's bounded text in the open, with the
  // wire name kept beside it rather than behind a second tap.
  function rawTextSection(view, tool, html) {
    ensureBadgeStyle();
    var raw = view && typeof view.raw === "string" ? view.raw : "";
    if (!raw) return "";
    var name = typeof tool === "string" && tool ? tool : "";
    // #2452: the server's chroma fragment is preferred when present. It is
    // trusted exactly like detailSection's html argument — chroma escapes every
    // token, so no tool output reaches the DOM as markup. The fragment covers
    // the whole output, so the bounded raw text's remainder note is dropped.
    var highlighted = typeof html === "string" && html !== "";
    return (
      '<section class="mb-5">' +
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      (name ? '<p class="mb-1 font-mono text-[11px] text-muted-faint break-all">' + escapeHTML(name) + "</p>" : "") +
      '<pre class="memory-scroll max-h-72 overflow-auto rounded-box border border-base-content/10 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' +
      (highlighted ? html : escapeHTML(raw)) +
      "</pre>" +
      (highlighted ? "" : remainderParagraph(rawRemainder(view))) +
      "</section>"
    );
  }

  // httpURL passes only an http(s) address through to markup; anything else is
  // text, not a link.
  function httpURL(value) {
    if (typeof value !== "string") return "";
    var href = value.trim();
    return /^https?:\/\/[^\s]+$/i.test(href) ? href : "";
  }

  // hostOf is the address a reader recognises: the link's host, without scheme or
  // path.
  function hostOf(href) {
    try {
      return new URL(href).host;
    } catch (e) {
      return href;
    }
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
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">' + label + "</p>" +
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
      '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">Result</p>' +
      '<p class="text-sm text-base-content leading-relaxed whitespace-pre-wrap break-words">' +
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
      '<summary class="collapse-title text-xs font-semibold tracking-wider uppercase text-muted">Raw input / output</summary>' +
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
      '<span class="iconify ' + cfg.icon + ' size-4 text-muted-faint" aria-hidden="true"></span></span>' +
      '<span class="memory-badge-label min-w-0 truncate text-xs text-muted">' + escapeHTML(cfg.label) + "</span>" +
      (cfg.afterLabel || "") +
      '<span class="memory-badge-secondary hidden min-w-0 max-w-[40%] shrink truncate text-xs text-muted-faint"></span>' +
      '<span class="memory-badge-chevron ml-auto flex shrink-0 transition-transform duration-200">' +
      '<span class="iconify lucide--chevron-down size-3.5 text-muted-faint" aria-hidden="true"></span></span>' +
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

  /* ---------- tool runs (a turn's steps fold into one row) ---------- */

  // A run is the consecutive steps of one turn. One summary line states the step
  // count, the span from the first start to the last settle (wall clock, only
  // once it reaches a second) and how many steps failed; opening it reveals the
  // steps in execution order. The default is open while a step is still running
  // and closed once the run settles — unless the reader opened or closed it,
  // whose choice outranks the default. Choices are keyed by the turn and live
  // for the page's lifetime: nothing is persisted, so a reload starts from the
  // default.
  var toolRunChoices = typeof Map === "function" ? new Map() : null;

  function toolRunChoice(key) {
    if (!key || !toolRunChoices) return null;
    return toolRunChoices.has(key) ? toolRunChoices.get(key) : null;
  }

  function rememberToolRunChoice(key, open) {
    if (!key || !toolRunChoices) return;
    toolRunChoices.set(key, !!open);
  }

  // captureLiveToolRunChoice / adoptNewestToolRunChoice carry the reader's
  // open/closed choice across the live-DOM -> history re-render on a surface
  // whose stream never names the turn's run id (the public share page, #2323).
  // The live run is keyed "live-turn-N"; after the history render its
  // replacement is keyed by the item's run id.
  function captureLiveToolRunChoice(container) {
    if (!container) return null;
    var live = container.querySelector('details.memory-tool-run[data-turn^="live-turn-"]');
    if (!live) return null;
    return live.hasAttribute("open");
  }

  function adoptNewestToolRunChoice(container, open) {
    if (!container || open === null || open === undefined) return false;
    var runs = container.querySelectorAll('details.memory-tool-run[data-turn]');
    var newest = null;
    for (var i = 0; i < runs.length; i++) {
      var key = runs[i].getAttribute("data-turn") || "";
      if (key.indexOf("live-turn-") === 0) continue;
      newest = runs[i];
    }
    if (!newest) return false;
    var runKey = newest.getAttribute("data-turn") || "";
    if (runKey) rememberToolRunChoice(runKey, open);
    if (open) newest.setAttribute("open", "");
    else newest.removeAttribute("open");
    return true;
  }

  // auxShell is the row a step and a run both sit in: the same shell, so a run
  // summary lines up with the step it replaces, and a single-step turn can go
  // back to being a plain step without moving its text. Like an assistant turn
  // it is a plain block row with no avatar gutter, so every left-side row in
  // the transcript starts at the same content edge (see app.css
  // "left-side transcript rows").
  function auxShell() {
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-aux";
    return wrap;
  }

  // createToolRun builds the grouped row for a turn's consecutive tool calls.
  // A group renders only when it holds 2+ calls; the live path dissolves a lone
  // call back into today's plain chip. The summary states the call count and a
  // breakdown of the tool names, a spinner when any call is running and a word
  // when any failed. The body lists the child chips and scrolls after ~24rem.
  function createToolRun(container, turnKey, opts) {
    opts = opts || {};
    ensureBadgeStyle();
    var steps = [];
    var defaultOpen = opts.openDefault === true;

    var shell = auxShell();
    var el = document.createElement("details");
    el.className = "memory-tool-run memory-tool-group w-full";
    el.setAttribute("data-tool-group", "1");
    el.setAttribute("data-tool-count", "0");
    if (turnKey) el.setAttribute("data-turn", String(turnKey));
    el.innerHTML =
      '<summary class="memory-tool-run-summary text-xs text-muted">' +
      '<span class="flex size-[22px] shrink-0 items-center justify-center">' +
      '<span class="iconify lucide--wrench size-4 text-muted-faint" aria-hidden="true"></span></span>' +
      '<span class="memory-tool-run-count"></span>' +
      '<span class="memory-tool-run-breakdown hidden min-w-0 max-w-[48%] shrink truncate text-xs text-muted-faint"></span>' +
      '<span class="memory-tool-run-running hidden items-center gap-1 text-muted-strong">' +
      '<span class="iconify lucide--loader-circle size-3.5 animate-spin" aria-hidden="true"></span></span>' +
      '<span class="memory-tool-run-failed hidden items-center gap-1 text-error">' +
      '<span class="iconify lucide--alert-triangle size-3.5" aria-hidden="true"></span>' +
      '<span class="memory-tool-run-failed-text"></span></span>' +
      '<span class="memory-tool-run-chevron ml-auto flex shrink-0 items-center">' +
      '<span class="iconify lucide--chevron-right size-3.5 text-muted-faint" aria-hidden="true"></span></span>' +
      "</summary>" +
      '<div class="memory-tool-group-body memory-tool-run-body flex flex-col gap-0.5 overflow-y-auto pb-1"></div>';
    if (defaultOpen) el.setAttribute("open", "");
    shell.appendChild(el);
    container.appendChild(shell);

    var body = el.querySelector(".memory-tool-run-body");
    var countEl = el.querySelector(".memory-tool-run-count");
    var breakdownEl = el.querySelector(".memory-tool-run-breakdown");
    var runningEl = el.querySelector(".memory-tool-run-running");
    var failEl = el.querySelector(".memory-tool-run-failed");
    var failText = el.querySelector(".memory-tool-run-failed-text");

    function foundFailed() {
      var n = 0;
      for (var i = 0; i < steps.length; i++) if (steps[i].failed) n++;
      return n;
    }

    function anyRunning() {
      for (var i = 0; i < steps.length; i++) if (steps[i].running) return true;
      return false;
    }

    // breakdown names the calls a reader would recognise: up to three distinct
    // tools with a ×N multiplier, then "+N more" for the tail.
    function breakdownText() {
      var counts = {};
      var order = [];
      for (var i = 0; i < steps.length; i++) {
        var name = steps[i].tool || "";
        if (!name) continue;
        if (!Object.prototype.hasOwnProperty.call(counts, name)) {
          counts[name] = 0;
          order.push(name);
        }
        counts[name]++;
      }
      if (!order.length) return "";
      var parts = [];
      var limit = Math.min(3, order.length);
      for (var j = 0; j < limit; j++) {
        var n = order[j];
        var label = humanizeToolName(n);
        parts.push(counts[n] > 1 ? label + " ×" + counts[n] : label);
      }
      if (order.length > limit) parts.push("+" + (order.length - limit) + " more");
      return parts.join(", ");
    }

    function paint() {
      el.setAttribute("data-tool-count", String(steps.length));
      countEl.textContent = steps.length + (steps.length === 1 ? " tool call" : " tool calls");
      var breakdown = breakdownText();
      breakdownEl.textContent = breakdown ? " · " + breakdown : "";
      breakdownEl.classList.toggle("hidden", !breakdown);
      // State never rests on colour alone: the failure indicator is a word, and
      // the running spinner is an icon plus a hidden label for screen readers.
      var running = anyRunning();
      runningEl.classList.toggle("hidden", !running);
      runningEl.classList.toggle("flex", running);
      var failed = foundFailed();
      failEl.classList.toggle("hidden", failed === 0);
      failEl.classList.toggle("flex", failed > 0);
      failText.textContent = failed ? " " + failed + " failed" : "";
    }

    function desiredOpen() {
      var choice = toolRunChoice(turnKey);
      if (choice !== null) return choice;
      return defaultOpen;
    }

    function applyOpen(open) {
      open = !!open;
      if (el.open === open) return;
      el.open = open;
    }

    // The reader's click is the choice: the summary's default action flips the
    // group after the event, so the state it is going to is `!el.open`.
    function rememberChoice(open) {
      rememberToolRunChoice(turnKey, open);
      if (!opts.runIdOf) return;
      var id = "";
      try {
        id = opts.runIdOf() || "";
      } catch (e) {
        id = "";
      }
      if (id && id !== turnKey) rememberToolRunChoice(id, open);
    }

    var summary = el.querySelector("summary");
    summary.addEventListener("click", function () {
      rememberChoice(!el.open);
    });

    var api = {
      el: el,
      shell: shell,
      body: body,
      key: turnKey,
      steps: steps,
      addStep: function (chip, facts) {
        facts = facts || {};
        steps.push({
          chip: chip || null,
          tool: facts.tool || "",
          startedAt: facts.startedAt || Date.now(),
          settledAt: facts.settledAt || 0,
          failed: !!facts.failed,
          running: !!facts.running,
        });
        if (chip) body.appendChild(chip);
        paint();
        return chip;
      },
      settleStep: function (chip, facts) {
        facts = facts || {};
        for (var i = 0; i < steps.length; i++) {
          if (chip && steps[i].chip === chip) {
            steps[i].settledAt = facts.settledAt || Date.now();
            steps[i].running = false;
            if (facts.failed !== undefined) steps[i].failed = !!facts.failed;
            paint();
            return;
          }
        }
      },
      removeStep: function (chip) {
        // Caller owns the chip's new home; only forget its group membership.
        for (var i = 0; i < steps.length; i++) {
          if (steps[i].chip !== chip) continue;
          steps.splice(i, 1);
          paint();
          return;
        }
      },
      finish: function (finishOpts) {
        finishOpts = finishOpts || {};
        var now = Date.now();
        for (var i = 0; i < steps.length; i++) {
          if (!steps[i].settledAt) steps[i].settledAt = now;
          if (steps[i].running) steps[i].running = false;
        }
        paint();
        // A page can only learn the turn's id once the run is under way. Keep
        // the reader's choice under the id the history render will use.
        if (finishOpts.runId && finishOpts.runId !== turnKey) {
          var choice = toolRunChoice(turnKey);
          if (choice !== null) rememberToolRunChoice(finishOpts.runId, choice);
        }
        if (steps.length <= 1) {
          api.dissolve();
          return;
        }
        applyOpen(desiredOpen());
      },
      // dissolve replaces the group with the single step it holds (or removes
      // an empty shell): a one-call turn reads as a chip, not as a group of one.
      dissolve: function () {
        var parent = shell.parentNode;
        if (!parent) return;
        for (var i = 0; i < steps.length; i++) {
          var chip = steps[i].chip;
          if (!chip) continue;
          var row = auxShell();
          row.appendChild(chip);
          parent.insertBefore(row, shell);
        }
        parent.removeChild(shell);
      },
    };
    paint();
    return api;
  }

  // Inject the badge + shimmer CSS once. Unlayered, so it sits above the
  // daisyUI/Tailwind layers in app.css; runs after the stylesheet link, so it
  // also wins the cascade against app.css's unlayered tool-chip rules.
  //
  // This is the ONLY runtime-injected stylesheet, and it owns just the
  // expandable-badge shell + its live affordances (the label shimmer and the
  // thinking-icon bob) that app.css deliberately does not define.
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
      // live thinking: the leading icon breathes while the segment is open
      // (the label shimmers above). Scoped to `.memory-thinking` so the tool
      // chip's wrench never bobs, and to `:not(.lucide--chevron-down)` so the
      // hover-swapped chevron stays still. Class-gated on `.memory-badge-live`,
      // so `done:true` removing the class stops it cleanly — same trigger as
      // the shimmer, live and replayed alike.
      ".memory-badge-live.memory-thinking .memory-badge-icon .iconify:not(.lucide--chevron-down){" +
      "animation:memory-badge-bob 2.4s ease-in-out infinite;transform-origin:50% 50%}" +
      "@keyframes memory-badge-bob{" +
      "0%,100%{opacity:.5;transform:translateY(0) scale(1)}" +
      "50%{opacity:1;transform:translateY(-1px) scale(1.05)}}" +
      "@media (prefers-reduced-motion:reduce){" +
      ".memory-badge-live .memory-badge-label{animation:none}" +
      ".memory-badge-live.memory-thinking .memory-badge-icon .iconify:not(.lucide--chevron-down){animation:none;opacity:.85}" +
      // the raw disclosure opens and closes with no motion of its own; this keeps
      // that true if a later restyle adds a transition to it
      ".memory-raw-disclosure,.memory-raw-disclosure *{transition:none!important;animation:none!important}}" +
      // the disclosure's control is 44 CSS pixels tall by its own rule, not by a
      // spacing utility: the app's root font is 15px, which makes min-h-11 41.25px
      ".memory-raw-summary{display:flex;align-items:center;min-height:44px}" +
      // the open state turns the chevron over; no motion when the reader asked for
      // less (the reduced-motion block above covers the transition)
      ".memory-raw-chevron{transition:transform .15s ease}" +
      ".memory-raw-disclosure[open] .memory-raw-chevron{transform:rotate(180deg)}" +
      // a run's summary is the same kind of control as the raw disclosure's: 44
      // CSS pixels by its own rule (same root-font reason), no native marker, and
      // a chevron that turns over when the run is open
      ".memory-tool-run-summary{display:flex;align-items:center;min-height:44px;cursor:pointer;list-style:none;padding:0 .5rem}" +
      ".memory-tool-run-summary::-webkit-details-marker{display:none}" +
      ".memory-tool-run-chevron{transition:transform .15s ease}" +
      ".memory-tool-run[open] .memory-tool-run-chevron{transform:rotate(90deg)}" +
      // the reduced-motion rule for the run chevron comes last: same
      // specificity, so it has to follow the transition it cancels
      "@media (prefers-reduced-motion:reduce){.memory-tool-run-chevron{transition:none}}";
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

    // Same left-side row as an assistant turn (no avatar gutter), so the badge
    // starts at the transcript's content edge.
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-aux";
    messages.appendChild(wrap);

    var badge = expandableBadge({
      className: "memory-thinking w-full",
      icon: "lucide--brain",
      label: "Thinking",
      live: live,
      container: wrap,
    }, ctx);
    if (!badge) return null;
    // reasoning text lives here; history renders markdown, live appends text
    badge.detail.innerHTML =
      '<p class="memory-thinking-body font-mono text-xs leading-relaxed text-muted-strong whitespace-pre-wrap break-words"></p>';

    // Hover affordance: swap the brain icon for a dropdown chevron (Paseo-style).
    var iconEl = badge.root.querySelector(".memory-badge-icon .iconify");
    if (badge.toggle) {
      badge.toggle.addEventListener("mouseenter", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--chevron-down size-4 text-muted-faint");
      });
      badge.toggle.addEventListener("mouseleave", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--brain size-4 text-muted-faint");
      });
    }

    return {
      details: badge.root,
      body: badge.detail.querySelector(".memory-thinking-body"),
      setLive: badge.setLive,
    };
  }

  // Shared thinking host — the live `thinking` SSE path and the transcript path
  // in one object, so the pages that render thinking (/chat, the global
  // assistant drawer, public share pages) share one implementation instead of a
  // copy each.
  //
  // Live thinking merges while contiguous: every `thinking` frame appends to the
  // current block until an activity boundary (`break`) closes it. A later frame
  // after that boundary starts a fresh row.
  //   opts.badgeCtx          passed to createThinkingBlock
  //   opts.scrollToBottom()  called after a live delta lands
  // Returns:
  //   handle(evt)            live frame: appends to the open block, or starts one
  //   break()                an activity boundary ended the current block
  //   render(text, html,role) history: one static, collapsed badge
  //   clear()                forget live state (a new turn started)
  //   finalize()             stop the shimmer on the open block, then clear
  function createThinkingHost(opts) {
    opts = opts || {};
    var current = null; // { body, setLive, done }
    var scroll = opts.scrollToBottom || function () {};

    function breakBlock() {
      if (!current) return;
      if (current.setLive) current.setLive(false);
      current = null;
    }

    function handle(evt) {
      evt = evt || {};
      var text = evt.text == null ? "" : String(evt.text);
      var role = evt.role ? evt.role : "operator";
      if (!current) {
        current = createThinkingBlock(role, { live: true }, opts.badgeCtx);
        if (!current) return;
      }
      if (current.setLive) current.setLive(!(evt && evt.done));
      if (text) {
        current.body.appendChild(document.createTextNode(text));
        scroll();
      }
      if (evt && evt.done) current.done = true;
    }

    function render(text, html, role) {
      var rec = createThinkingBlock(role, { live: false }, opts.badgeCtx);
      if (!rec) return null;
      if (html) {
        // Server rendered the markdown already (content.html) — render it like
        // assistant markdown, but dimmed.
        rec.body.className = "memory-thinking-body memory-md break-words opacity-70";
        rec.body.innerHTML = html;
      } else if (text) {
        rec.body.textContent = text;
      }
      return rec;
    }

    function clear() {
      current = null;
    }

    function finalize() {
      breakBlock();
    }

    return {
      handle: handle,
      break: breakBlock,
      render: render,
      clear: clear,
      finalize: finalize,
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
    messages.appendChild(wrap);

    var badge = expandableBadge({
      className: "memory-agent-prompt w-full",
      icon: "lucide--scroll-text",
      label: "Agent prompt",
      afterLabel:
        '<span class="badge badge-ghost badge-xs font-normal">system</span>',
      container: wrap,
      renderDetails: function (detailEl) {
        detailEl.innerHTML =
          '<p class="mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted">System instruction</p>' +
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
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--chevron-down size-4 text-muted-faint");
      });
      badge.toggle.addEventListener("mouseleave", function () {
        if (iconEl) iconEl.setAttribute("class", "iconify lucide--scroll-text size-4 text-muted-faint");
      });
    }
    return badge.root;
  }

  /* ---------- recorded pipeline stage input ---------- */

  // A recorded pipeline transcript (an extraction run) stores on each message the
  // stage that produced it and what the text is: content.stage names the stage,
  // content.stage_kind says which side of the stage the text belongs to. The
  // label is metadata, never markup inside the text — the recorder used to write
  // "**EntityProcessor** · stage output" into the message, which rendered
  // literally on plain-text surfaces and which the history renderer's
  // chain-of-thought heuristic then mistook for reasoning.
  var STAGE_KIND_LABELS = {
    prompt: "prompt",
    output: "output",
    error: "error",
    header: "run",
  };

  function stageKindLabel(kind) {
    if (!kind) return "";
    return STAGE_KIND_LABELS[kind] || String(kind);
  }

  // stageInputCard renders a stage's INPUT (the run header, or the prompt the
  // stage was asked) as a collapsed card rather than a "You" bubble: the
  // operator never typed it, and a stage prompt embeds the document — tens of
  // kilobytes that must stay inspectable without dominating the transcript.
  // History-only: the live stream never carries a recorded stage prompt.
  function stageInputCard(ctx, stage, kind, text, meta) {
    ctx = ctx || {};
    var messages = ctx.messages;
    if (!messages || !text) return null;

    var isHeader = kind === "header";
    var icon = isHeader ? "lucide--flag" : "lucide--arrow-down-to-line";
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-aux";
    messages.appendChild(wrap);

    var kindLabel = stageKindLabel(kind);
    var badge = expandableBadge({
      className: "memory-stage-input w-full",
      icon: icon,
      label: stage || "Stage",
      afterLabel: kindLabel
        ? '<span class="badge badge-ghost badge-xs font-normal">' + escapeHTML(kindLabel) + "</span>"
        : "",
      container: wrap,
      renderDetails: function (detailEl) {
        var heading = document.createElement("p");
        heading.className = "mb-1.5 text-xs font-semibold tracking-wider uppercase text-muted";
        heading.textContent = isHeader ? "Run" : "Stage prompt";
        var pre = document.createElement("pre");
        pre.className = "memory-scroll max-h-72 overflow-auto rounded-box border border-base-content/10 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words";
        pre.textContent = text;
        detailEl.appendChild(heading);
        detailEl.appendChild(pre);
      },
    }, ctx);
    if (!badge) return null;
    // Same step/relative-time annotation the bubbles carry (run pages only).
    if (meta) {
      var footer = document.createElement("div");
      footer.className = "chat-footer mt-1 text-[11px] text-muted-faint font-mono";
      footer.textContent = meta;
      wrap.appendChild(footer);
    }
    return badge.root;
  }

  /* ---------- run-control surfaces (copy, footer, typed run markers) ---------- */

  // Single implementation: MemoryChatHost.copyText (chat-host.js) owns the
  // navigator.clipboard + hidden-textarea/execCommand fallback. This module is
  // only loaded alongside it (same shell script block on every chat surface),
  // so there is no second fallback copy here to drift out of sync.
  function clipboardCopy(text) {
    if (window.MemoryChatHost && typeof window.MemoryChatHost.copyText === "function") {
      window.MemoryChatHost.copyText(text);
    }
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
  // a whole-message copy button in the assistant action row (never in the
  // reading column) and a copy button on every fenced code block. Idempotent:
  // guarded per element, safe to re-run.
  function enhanceMessage(wrap) {
    if (!wrap) return;
    // Whole-message copy — assistant turns only (plain markdown wrapper).
    var actions = wrap.classList && wrap.classList.contains("memory-assistant")
      ? wrap.querySelector(".memory-assistant-actions")
      : null;
    var body = actions ? wrap.querySelector(".memory-md") : null;
    if (actions && body && !actions._memoryCopyWired) {
      actions._memoryCopyWired = true;
      actions.appendChild(makeCopyButton({
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

  /* ---------- citation sources ---------- */

  // UUID-shaped id, the only object identifier we will turn into a link.
  var UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  // A trusted object-page url: /objects/<uuid>, optionally carrying the
  // relationship fragment `/objects/<uuid>#relationship-<uuid>` a relationship
  // citation points at. No scheme, no query, and no other fragment is trusted.
  var OBJECT_URL_RE = /^\/objects\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?:#relationship-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})?$/i;

  function isUUID(s) {
    return typeof s === "string" && UUID_RE.test(s);
  }

  // citationHref resolves a SAFE href for a citation, or "" when none can be
  // trusted. An agent-supplied url is only used when it is a trusted
  // /objects/<uuid> path (optionally with its #relationship-<uuid> fragment);
  // otherwise an OBJECT id itself must be a UUID. A relationship id is never
  // opened as an object: it is not an object page, so with no trusted url the
  // row stays inert instead of linking to a page that shows "Object
  // unavailable". This is the only place a citation becomes a link — labels and
  // types stay textContent.
  function citationHref(c) {
    if (!c || typeof c !== "object") return "";
    if (typeof c.url === "string" && OBJECT_URL_RE.test(c.url)) return scopedPath(c.url);
    if (c.kind !== "relationship" && isUUID(c.id)) return scopedPath("/objects/" + c.id);
    return "";
  }

  /* ---------- compiled type labels ---------- */

  // The shell embeds a JSON map of compiled type name → {label,icon,color}
  // (<script id="memory-object-types">), so citations can show the schema's
  // human label instead of the raw type name. Parsed lazily and cached for the
  // page; a missing/malformed map degrades to {} (every lookup then humanizes).
  var _objectTypes = null;
  function objectTypes() {
    if (_objectTypes) return _objectTypes;
    _objectTypes = {};
    var el = document.getElementById("memory-object-types");
    if (el) {
      try {
        var parsed = JSON.parse(el.textContent || "{}");
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) _objectTypes = parsed;
      } catch (e) { /* malformed map — fall back to humanizing the type name */ }
    }
    return _objectTypes;
  }

  // humanizeTypeName turns a raw schema type name into a quiet human label:
  // "LegalParagraph" → "Legal paragraph", "legal_paragraph" → "Legal paragraph".
  function humanizeTypeName(name) {
    var s = String(name == null ? "" : name).trim();
    if (!s) return "";
    s = s
      .replace(/[._-]+/g, " ")
      .replace(/([a-z0-9])([A-Z])/g, "$1 $2") // camelCase boundary
      .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2") // acronym boundary (MCPTool)
      .replace(/\s+/g, " ")
      .trim()
      .toLowerCase();
    return s.replace(/^./, function (c) { return c.toUpperCase(); });
  }

  // objectTypeLabel resolves a citation/item type to its human label. A
  // server-supplied `type_label` wins (other clients, like iOS, have no
  // embedded map and rely on it); otherwise the compiled type's label when the
  // embedded map knows it, else a humanized form of the raw name. An empty type
  // resolves to "".
  function objectTypeLabel(typeName, typeLabel) {
    if (typeof typeLabel === "string" && typeLabel) return typeLabel;
    var name = typeName == null ? "" : String(typeName);
    if (!name) return "";
    var t = objectTypes()[name];
    if (t && typeof t.label === "string" && t.label) return t.label;
    return humanizeTypeName(name);
  }

  // objectTypeColor resolves the badge accent: the server-supplied `type_color`
  // wins, else the compiled map's declared color. Returns "" when neither is a
  // simple hex color. Only a trusted hex value ever reaches the style rule.
  function objectTypeColor(typeName, typeColor) {
    if (typeof typeColor === "string" && /^#[0-9a-fA-F]{3,8}$/.test(typeColor)) return typeColor;
    var accent = objectTypeAccent(typeName);
    if (accent && /^#[0-9a-fA-F]{3,8}$/.test(accent.color)) return accent.color;
    return "";
  }

  // objectTypeAccent returns the type's declared {icon,color} when the map
  // carries one, else null. Both fields are free-form schema metadata.
  function objectTypeAccent(typeName) {
    var t = objectTypes()[typeName];
    if (!t || typeof t !== "object") return null;
    var icon = typeof t.icon === "string" ? t.icon : "";
    var color = typeof t.color === "string" ? t.color : "";
    if (!icon && !color) return null;
    return { icon: icon, color: color };
  }

  /* ---------- citation sources (one shared renderer) ---------- */

  // sourceName resolves a source item's display name (citations and A2UI
  // sources items alike): label, else key, else id. A bare string item is its
  // own name. Untrusted — always rendered with textContent.
  function sourceName(item) {
    if (typeof item === "string") return item;
    if (!item || typeof item !== "object") return "";
    if (item.label != null && item.label !== "") return String(item.label);
    if (item.key != null && item.key !== "") return String(item.key);
    if (item.id != null && item.id !== "") return String(item.id);
    return "";
  }

  // sourceRow renders one source as a compact, linkable box in the spirit of
  // the entity row: the name (grow/truncate) plus its human type badge, tinted
  // by the type's declared color when present. Name and type are UNTRUSTED —
  // textContent only; the href is the validated citationHref.
  function sourceRow(item) {
    var li = document.createElement("li");
    li.className = "flex";
    li.setAttribute("data-testid", "chat-source");

    var isObj = item && typeof item === "object";
    var name = sourceName(item);
    var href = isObj ? citationHref(item) : "";

    var row;
    if (href) {
      row = document.createElement("a");
      row.href = href;
      row.className = "memory-source flex min-w-0 w-full items-center gap-2 rounded-field border border-base-content/10 bg-base-200/30 px-2.5 py-1.5 transition-colors hover:bg-base-200/60";
    } else {
      row = document.createElement("span");
      row.className = "memory-source flex min-w-0 w-full items-center gap-2 rounded-field border border-base-content/10 bg-base-200/30 px-2.5 py-1.5";
    }

    var nameEl = document.createElement("span");
    nameEl.className = "min-w-0 grow truncate text-xs text-base-content";
    nameEl.setAttribute("data-testid", "chat-source-name");
    nameEl.textContent = name;
    row.appendChild(nameEl);

    var type = isObj ? (item.type == null ? "" : String(item.type)) : "";
    var typeLabel = isObj
      ? objectTypeLabel(type, item.type_label)
      : objectTypeLabel(type);
    if (typeLabel) {
      var badge = document.createElement("span");
      badge.className = "badge badge-ghost badge-xs shrink-0 font-normal";
      badge.setAttribute("data-testid", "chat-source-type");
      badge.textContent = typeLabel;
      var color = isObj ? objectTypeColor(type, item.type_color) : "";
      if (color) {
        badge.setAttribute(
          "style",
          "color:" + color +
            ";background-color:color-mix(in oklch," + color + " 10%,transparent)" +
            ";border-color:color-mix(in oklch," + color + " 15%,transparent)"
        );
      }
      row.appendChild(badge);
    }

    li.appendChild(row);
    return li;
  }

  // sourceRows builds the shared source list from citations or A2UI items,
  // skipping entries with no displayable name. Returns { list, count } or null
  // when nothing is renderable — so callers can omit the block entirely.
  function sourceRows(items) {
    var list = Array.isArray(items) ? items : [];
    var ul = document.createElement("ul");
    ul.className = "flex flex-col gap-1.5";
    var count = 0;
    for (var i = 0; i < list.length; i++) {
      if (!sourceName(list[i])) continue;
      ul.appendChild(sourceRow(list[i]));
      count++;
    }
    if (!count) return null;
    return { list: ul, count: count };
  }

  // sourcesCountLabel renders the singular-aware source count ("1 source").
  function sourcesCountLabel(n) {
    return n + (n === 1 ? " source" : " sources");
  }

  // sourcesFooter renders the in-bubble Sources disclosure: a quiet, collapsed
  // daisyUI collapse-arrow <details> whose summary reads "N sources". Returns
  // null when there is nothing to show.
  function sourcesFooter(citations) {
    var rows = sourceRows(citations);
    if (!rows) return null;

    var details = document.createElement("details");
    details.className =
      "collapse collapse-arrow mt-3 rounded-box border border-base-content/10 bg-base-200/20";
    details.setAttribute("data-testid", "chat-sources");

    var summary = document.createElement("summary");
    summary.className =
      "collapse-title flex min-h-0 items-center gap-2 py-1.5 text-[11px] font-medium text-muted";
    summary.setAttribute("data-testid", "chat-sources-toggle");
    summary.innerHTML =
      '<span class="iconify lucide--link size-3.5 shrink-0" aria-hidden="true"></span>' +
      '<span data-testid="chat-sources-count"></span>';
    summary.querySelector('[data-testid="chat-sources-count"]').textContent = sourcesCountLabel(rows.count);

    var content = document.createElement("div");
    content.className = "collapse-content";
    content.appendChild(rows.list);

    details.appendChild(summary);
    details.appendChild(content);
    return details;
  }

  // attachSources mounts the Sources footer inside the assistant turn, directly
  // after the rendered markdown, so it reads as agent-side content and never
  // occupies a separate message row. Accepts the assistant wrapper (from
  // addAssistantMessage/openAssistantBubble) or a legacy bubble; idempotent —
  // one footer per turn.
  function attachSources(wrap, citations) {
    if (!wrap) return null;
    var container = wrap;
    var md = wrap.querySelector ? wrap.querySelector(".memory-md") : null;
    if (md && md.parentNode) container = md.parentNode;
    if (container._memorySources) return container._memorySources;
    var footer = sourcesFooter(citations);
    if (!footer) return null;
    if (md && md.parentNode === container) container.insertBefore(footer, md.nextSibling);
    else container.appendChild(footer);
    container._memorySources = footer;
    return footer;
  }

  /* ---------- A2UI declarative cards ---------- */

  // A2UI (v0.9.1) surface renderer. The agent streams declarative "cards" via
  // the gateway's `ui` SSE event; this walks the message list, accumulates
  // components from updateComponents, and renders one card per component into
  // the chat message list (ctx.messages — the same container question/approval
  // cards append to). createSurface / updateDataModel are tolerated (ignored
  // for now — this is a Phase-1 read-only render); deleteSurface tears a card
  // down. Progressive frames for the same surface update its cards in place
  // (one card instance per component), so a streamed surface is never drawn
  // twice. Unknown component ids fall back to a summary card and never throw.
  // All user/model text is escaped (textContent / escapeHTML), never injected
  // via innerHTML.
  //
  // Two render-local concepts are worth keeping apart from the wire:
  //   surfaceKey  identifies a surface's rendered nodes inside one transcript
  //               container, so replay can host two turns that both named their
  //               surface "s1" (see the key comment below);
  //   announcement  a card arriving on the live stream is spoken once, because
  //               the transcript is deliberately not a live region.

  function renderA2UISurface(surfaceId, messages, ctx) {
    ctx = ctx || {};
    var container = ctx.messages;
    if (!container) return;
    var hideEmpty = ctx.hideEmpty;
    var scrollToBottom = ctx.scrollToBottom || function () {};

    // canDecide mirrors the question/approval cards in chat-stream.js: a host
    // that disables end-user decisions (the public share page when the link has
    // allow_end_user_approvals false) supplies an approvalsEnabled hook, and the
    // decision-bearing A2UI cards then render read-only instead of offering a
    // control the server would reject with 403. Hosts that omit the hook (the
    // authenticated chat and sidepanel) keep the full controls.
    var canDecide = !ctx.approvalsEnabled || ctx.approvalsEnabled();

    var list = Array.isArray(messages) ? messages : [];

    // The wire surfaceId identifies a surface to the server; the registry key
    // identifies its rendered nodes inside one transcript container. They are
    // not the same thing on replay: two answers may both name the surface "s1"
    // (the prompt teaches that id), and keying the registry by surfaceId alone
    // made the second render replace the first card in place, so the transcript
    // kept only the newest card. A host rendering one surface per transcript
    // item passes ctx.surfaceKey (the item id) to scope the nodes to that item;
    // the live stream passes nothing, so its progressive frames keep updating
    // one surface in place.
    var replay = ctx.surfaceKey != null && ctx.surfaceKey !== "";
    var surfaceKey = replay ? surfaceId + "\u0000" + ctx.surfaceKey : surfaceId;

    // deleteSurface tears the card down — the server sends it when a surface it
    // showed progressively later fails validation, so a half-rendered card must
    // not outlive the run.
    for (var d = 0; d < list.length; d++) {
      if (list[d] && list[d].deleteSurface) {
        removeA2UISurface(container, surfaceKey);
        if (hideEmpty) hideEmpty();
        scrollToBottom();
        return;
      }
    }

    // Accumulate components from updateComponents; ignore other envelope kinds.
    var components = [];
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

    // Update in place: a progressive frame for a surface we already rendered
    // replaces its cards rather than appending a second instance.
    //
    // "New" means no rendered cards yet, not "no registry entry": the first
    // frame of a live surface carries only createSurface (the stream parses one
    // message at a time), so it arrives with zero components and leaves an empty
    // entry behind. Announcing on the next frame — the one that actually carries
    // the card — is what makes the announcement fire at all.
    //
    // This rests on an invariant: a builder that returns a node for every
    // catalog component, so a frame with components always renders cards. No
    // builder returns null today (the only `return null`s in this renderer are
    // in a2uiPairRows, which is a row helper, not a builder); if one ever did,
    // a surface with no renderable components would keep an empty registry entry
    // and re-announce on every progressive frame.
    var rendered = a2uiSurfaceRegistry(container)[surfaceKey];
    var isNewSurface = !rendered || rendered.length === 0;
    replaceA2UISurface(container, surfaceId, surfaceKey, components, canDecide);
    // Announce only a surface whose first card just arrived, and only on the
    // live stream: the transcript is deliberately not a live region (announcing
    // a whole replayed history would be noise), so a card that streams in would
    // otherwise be silent to assistive tech.
    if (!replay && isNewSurface && components.length) {
      announceA2UI(container, components);
    }
    if (hideEmpty) hideEmpty();
    scrollToBottom();
  }

  // a2uiSurfaceIDOf derives the surface id from a persisted message list using
  // the same "first createSurface wins" rule as the server's SurfaceIDOf, so a
  // replayed card keeps its identity.
  function a2uiSurfaceIDOf(messages) {
    var list = Array.isArray(messages) ? messages : [];
    for (var i = 0; i < list.length; i++) {
      var created = list[i] && list[i].createSurface;
      if (created && created.surfaceId) return String(created.surfaceId);
    }
    return "surface-1";
  }

  // a2uiSurfaceRegistry stores each surface's rendered card nodes on its
  // message container, keyed by the render-local surface key (the wire
  // surfaceId on the live stream, surfaceId + timeline item id on replay). Each
  // page owns its own container, so the registry is never shared across pages or
  // the live stream and history.
  function a2uiSurfaceRegistry(container) {
    if (!container._a2uiSurfaces) container._a2uiSurfaces = {};
    return container._a2uiSurfaces;
  }

  // resetA2UI drops a transcript container's rendered-surface state and removes
  // its announcer. A host starting a new conversation clears the transcript with
  // `container.innerHTML = ""`, which removes the cards and the announcer node
  // but not the registry this renderer keeps on the container: without this
  // reset the new conversation's first card matches a stale, detached registry
  // entry and is never announced, and the announcement ordinal keeps counting
  // from the previous conversation (#597).
  function resetA2UI(container) {
    if (!container) return;
    delete container._a2uiSurfaces;
    var node = container.querySelector(".memory-a2ui-announcer");
    if (node && node.parentNode) node.parentNode.removeChild(node);
  }

  // replaceA2UISurface swaps a surface's cards for the newly accumulated
  // components, preserving the surface's position in the transcript. surfaceKey
  // scopes the rendered nodes to one transcript item (see renderA2UISurface);
  // surfaceId is what a card action reports back to the server.
  function replaceA2UISurface(container, surfaceId, surfaceKey, components, canDecide) {
    var registry = a2uiSurfaceRegistry(container);
    var old = registry[surfaceKey] || [];
    var anchor = old.length ? old[0] : null;

    var nodes = [];
    for (var k = 0; k < components.length; k++) {
      var node = buildA2UICard(components[k], surfaceId, canDecide);
      if (!node) continue;
      if (anchor && anchor.parentNode) container.insertBefore(node, anchor);
      else container.appendChild(node);
      nodes.push(node);
    }
    for (var r = 0; r < old.length; r++) {
      if (old[r] && old[r].parentNode) old[r].parentNode.removeChild(old[r]);
    }
    registry[surfaceKey] = nodes;
  }

  // removeA2UISurface tears a surface's cards down (deleteSurface).
  function removeA2UISurface(container, surfaceKey) {
    var registry = a2uiSurfaceRegistry(container);
    var old = registry[surfaceKey];
    if (old) {
      for (var r = 0; r < old.length; r++) {
        if (old[r] && old[r].parentNode) old[r].parentNode.removeChild(old[r]);
      }
    }
    delete registry[surfaceKey];
  }

  // announceA2UI tells assistive tech that a card arrived. One visually hidden
  // polite status node per transcript carries the message; only a newly arrived
  // surface updates it, so a streamed turn announces each card once rather than
  // re-reading the surface on every progressive frame.
  //
  // The ordinal lives on the announcer node, not on the transcript container:
  // clearing the transcript is `container.innerHTML = ""`, which removes the
  // node, so a fresh conversation restarts at "New card 1". A container-lifetime
  // counter would survive the clear and announce the first card of a new
  // conversation as "New card 47".
  function announceA2UI(container, components) {
    if (!container || !components || !components.length) return;
    var node = container.querySelector(".memory-a2ui-announcer");
    if (!node) {
      node = document.createElement("div");
      node.className = "memory-a2ui-announcer sr-only";
      node.setAttribute("role", "status");
      node.setAttribute("aria-live", "polite");
      container.appendChild(node);
    }
    var n = (node._a2uiAnnounced || 0) + 1;
    node._a2uiAnnounced = n;
    node.textContent = a2uiAnnouncementText(n, components);
  }

  // a2uiAnnouncementText builds the status text for a surface that just
  // rendered. The running ordinal keeps two consecutive surfaces of the same
  // kind from producing identical text — a live region whose text does not
  // change is not re-announced, so "New card 1: Result card." followed by the
  // same string would be heard once.
  //
  // The copy is always singular "New card N", because N is the ordinal of the
  // surface, not a count of cards: a surface carrying two kinds reads
  // "New card 1: Result card, Sources card." A plural noun with "1" reads wrong
  // to an assistive technology.
  function a2uiAnnouncementText(n, components) {
    // Distinct kinds, in render order: a surface may carry more than one card.
    var kinds = [];
    for (var k = 0; k < components.length; k++) {
      var label = a2uiCardKindLabel(components[k] && components[k].component);
      if (kinds.indexOf(label) < 0) kinds.push(label);
    }
    return "New card " + n + ": " + kinds.join(", ") + ".";
  }

  // a2uiCardKindLabel names a card the way a screen reader should hear it, so
  // the card's group is identifiable by type before its body is read out. (It
  // maps onto A2UI v1.0's @accessibility.label, so that migration is a superset
  // of this rather than a rewrite.)
  function a2uiCardKindLabel(kind) {
    switch (kind) {
      case "proposal": return "Proposal card";
      case "approval": return "Approval card";
      case "question": return "Question card";
      case "code": return "Code card";
      case "entity": return "Graph object card";
      case "object-form": return "Form card";
      case "todo": return "Task list card";
      case "result": return "Result card";
      case "sources": return "Sources card";
      default: return "Card";
    }
  }

  function buildA2UICard(comp, surfaceId, canDecide) {
    var node = a2uiCardNode(comp, surfaceId, canDecide);
    if (node) {
      // Group semantics: the card boundary and its type are announced before
      // the body is read out. A group is not an AT quick-nav target, so this
      // names the card rather than claiming landmark-style jumping between
      // cards; it maps onto A2UI v1.0's @accessibility.label.
      node.setAttribute("role", "group");
      node.setAttribute("aria-label", a2uiCardKindLabel(comp && comp.component));
    }
    return node;
  }

  function a2uiCardNode(comp, surfaceId, canDecide) {
    switch (comp && comp.component) {
      case "proposal": return a2uiProposal(comp, surfaceId, canDecide);
      case "approval": return a2uiApproval(comp, surfaceId, canDecide);
      case "question": return a2uiQuestion(comp, surfaceId, canDecide);
      case "code": return a2uiCode(comp, surfaceId);
      case "entity": return a2uiEntity(comp, surfaceId);
      case "object-form": return a2uiObjectForm(comp, surfaceId);
      case "todo": return a2uiTodo(comp, surfaceId);
      case "result": return a2uiResult(comp, surfaceId);
      case "sources": return a2uiSources(comp, surfaceId);
      default: return a2uiSummary(comp, surfaceId);
    }
  }

  // a2uiShell builds the wrapper + card shell every card lives in: the same
  // `chat chat-start` + `card` structure the question/approval cards use, so
  // A2UI cards read as the agent speaking. Like those cards it is a plain
  // block row — no avatar gutter — so every card starts at the transcript's
  // content edge.
  function a2uiShell() {
    var wrap = document.createElement("div");
    wrap.className = "chat chat-start memory-rise memory-card";
    var card = document.createElement("div");
    card.className =
      "card card-border bg-base-100 w-fit min-w-[min(100%,30rem)] max-w-[85%] shadow-sm";
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

  function a2uiHeader(title, badge, href) {
    var h = document.createElement("div");
    h.className = "flex items-center gap-2";
    if (badge) {
      var b = document.createElement("span");
      b.className = "badge badge-sm badge-outline";
      b.textContent = badge;
      h.appendChild(b);
    }
    var t;
    if (href) {
      t = document.createElement("a");
      t.href = href;
      t.className = "link link-hover text-xs uppercase tracking-wider text-muted";
      t.setAttribute("data-testid", "a2ui-object-link");
    } else {
      t = document.createElement("span");
      t.className = "text-xs uppercase tracking-wider text-muted";
    }
    t.textContent = title;
    h.appendChild(t);
    return h;
  }

  function a2uiLabel(text) {
    var p = document.createElement("p");
    p.className = "text-sm text-base-content leading-relaxed whitespace-pre-wrap break-words";
    p.textContent = text == null ? "" : String(text);
    return p;
  }

  function a2uiSectionLabel(text) {
    var p = document.createElement("p");
    p.className = "mb-0 mt-1 text-xs font-semibold tracking-wider uppercase text-muted";
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

  // a2uiRows renders [[label, value, href?], ...] as stacked key/value rows. A
  // third element (when present and trusted) turns the value into a link.
  function a2uiRows(rows) {
    var list = document.createElement("div");
    list.className = "flex flex-col gap-2";
    for (var i = 0; i < rows.length; i++) {
      var pair = rows[i];
      var row = document.createElement("div");
      row.className = "flex flex-col";
      var key = document.createElement("span");
      key.className = "text-[10px] font-semibold tracking-wider uppercase text-muted";
      key.textContent = pair[0];
      var val;
      if (pair[2]) {
        val = document.createElement("a");
        val.href = pair[2];
        val.className = "link link-hover text-sm text-base-content leading-relaxed break-words whitespace-pre-wrap";
        val.setAttribute("data-testid", "a2ui-object-link");
      } else {
        val = document.createElement("span");
        val.className = "text-sm text-base-content leading-relaxed break-words whitespace-pre-wrap";
      }
      val.textContent = pair[1];
      row.appendChild(key);
      row.appendChild(val);
      list.appendChild(row);
    }
    return list;
  }

  // a2uiPairRows normalizes a structured prop into [[label, value, href?], ...]
  // rows for a2uiRows. It accepts an array of objects ({label|key|name|title,
  // value|text|body}), an array of [label, value] pairs, or a plain object
  // (entries become [key, value]). Anything else returns null.
  function a2uiPairRows(value) {
    if (value === undefined || value === null) return null;
    if (Array.isArray(value)) {
      if (!value.length) return [];
      var out = [];
      for (var i = 0; i < value.length; i++) {
        var item = value[i];
        if (Array.isArray(item)) {
          out.push([item[0], a2uiText(item[1])]);
        } else if (item && typeof item === "object") {
          var label = item.label != null ? item.label :
            item.key != null ? item.key :
            item.name != null ? item.name : item.title;
          var val = item.value != null ? item.value :
            item.text != null ? item.text : item.body;
          out.push([label, a2uiText(val)]);
        } else {
          return null;
        }
      }
      return out;
    }
    if (typeof value === "object") {
      var rows = [];
      for (var key in value) {
        if (Object.prototype.hasOwnProperty.call(value, key)) {
          rows.push([key, a2uiText(value[key])]);
        }
      }
      return rows;
    }
    return null;
  }

  // a2uiEmit reports a card action as a DOM event from the element that was
  // clicked, bubbling to whichever host owns the transcript. The renderer owns
  // no transport: the chat page sends the action as its conversation's next
  // turn, and a host that renders the same card without a sender (the side
  // panel, the public share page) stays inert instead of posting somewhere.
  function a2uiEmit(source, surfaceId, action) {
    (source || document).dispatchEvent(new CustomEvent("a2ui:action", {
      bubbles: true,
      composed: true,
      detail: { surfaceId: surfaceId, action: action },
    }));
  }

  function a2uiButton(surfaceId, label, action, primary) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = primary ? "btn btn-primary btn-sm" : "btn btn-ghost btn-sm";
    btn.textContent = label;
    btn.addEventListener("click", function () {
      // The renderer reports the click and nothing else: the host page sends it
      // (chat.js posts it as the conversation's next turn, and the server frames
      // the wording the agent receives), so no endpoint is invented here.
      a2uiEmit(btn, surfaceId, action);
    });
    return btn;
  }

  // a2uiApprovalsDisabledNote is the read-only footer the decision-bearing A2UI
  // cards show when the host disables end-user decisions: same right-aligned
  // muted-text footer, and the same copy, as the chat-stream question/approval
  // read-only cards, so both renderers read the same.
  function a2uiApprovalsDisabledNote() {
    var note = document.createElement("div");
    note.className = "memory-approval__actions flex items-center justify-end gap-2 pt-1";
    var span = document.createElement("span");
    span.className = "text-xs text-muted-faint";
    span.textContent = "Approvals are disabled for this chat.";
    note.appendChild(span);
    return note;
  }

  function a2uiActions(surfaceId, comp, pairs) {
    // pairs = [[label, responseValue, primary], ...]
    var row = document.createElement("div");
    row.className = "flex items-center justify-end gap-2 pt-1";
    for (var i = 0; i < pairs.length; i++) {
      row.appendChild(a2uiButton(
        surfaceId,
        pairs[i][0],
        { componentId: comp.id, response: pairs[i][1] },
        !!pairs[i][2]
      ));
    }
    return row;
  }

  function a2uiProposal(comp, surfaceId, canDecide) {
    var shell = a2uiShell();
    shell.body.appendChild(a2uiHeader("Proposal", comp.kind || "proposal"));
    if (comp.summary != null && comp.summary !== "") {
      shell.body.appendChild(a2uiLabel(comp.summary));
    }
    if (comp.body !== undefined && comp.body !== null) {
      var bodyPairs = a2uiPairRows(comp.body);
      if (bodyPairs && bodyPairs.length) {
        shell.body.appendChild(a2uiRows(bodyPairs));
      } else {
        shell.body.appendChild(a2uiPre(comp.body));
      }
    }
    if (canDecide) {
      shell.body.appendChild(a2uiActions(surfaceId, comp, [
        ["Reject", "reject", false],
        ["Accept", "accept", true],
      ]));
    } else {
      shell.body.appendChild(a2uiApprovalsDisabledNote());
    }
    return shell.wrap;
  }

  // a2uiApproval renders an A2UI approval component with the same card skeleton,
  // hook classes and data-testids as the canonical chat approval card
  // (chat-stream.js renderApproval), so the shared CSS and the card contract
  // test apply to both renderers. Decisions still dispatch an `a2ui:action`
  // CustomEvent — the A2UI action round-trip is a separate, unwired follow-up.
  function a2uiApproval(comp, surfaceId, canDecide) {
    var shell = a2uiShell();
    shell.card.className =
      "memory-approval card card-border bg-base-100 w-full max-w-[85%] shadow-sm border-warning/30";
    shell.card.setAttribute("data-testid", "chat-approval-card");
    shell.card.setAttribute("data-question-id", surfaceId || "");

    var title = document.createElement("h3");
    title.className = "memory-approval__title text-base leading-snug font-semibold break-words";
    title.textContent = "Approve tool call";
    shell.body.appendChild(title);

    var sentence = document.createElement("p");
    sentence.className = "memory-approval__sentence mt-0.5 text-xs text-muted-strong";
    sentence.appendChild(document.createTextNode("The agent wants to run "));
    var code = document.createElement("code");
    code.className = "font-mono text-xs";
    code.textContent = comp.tool || "tool";
    sentence.appendChild(code);
    sentence.appendChild(document.createTextNode("."));
    shell.body.appendChild(sentence);

    var inputPairs = a2uiPairRows(comp.input);
    if (inputPairs && inputPairs.length) {
      shell.body.appendChild(a2uiRows(inputPairs));
    } else {
      var args = document.createElement("pre");
      args.className =
        "memory-approval__args approval-args max-h-40 overflow-y-auto rounded-field bg-base-200/60 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words";
      args.textContent = safeJSON(comp.input);
      shell.body.appendChild(args);
    }

    // Read-only when the host disables end-user decisions: the tool call and its
    // args stay visible, but no decision controls render (the server 403s them).
    if (!canDecide) {
      shell.body.appendChild(a2uiApprovalsDisabledNote());
      return shell.wrap;
    }

    var actions = document.createElement("div");
    actions.className = "memory-approval__actions flex items-center justify-end gap-2 pt-1";

    var cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "memory-approval__cancel approval-cancel btn btn-ghost btn-sm";
    cancelBtn.setAttribute("data-testid", "chat-approval-cancel");
    cancelBtn.textContent = "Cancel";
    cancelBtn.addEventListener("click", function () {
      a2uiEmit(cancelBtn, surfaceId, { componentId: comp.id, response: "cancel" });
    });
    actions.appendChild(cancelBtn);

    var rejectBtn = document.createElement("button");
    rejectBtn.type = "button";
    rejectBtn.className = "memory-approval__reject-action approval-reject btn btn-ghost btn-sm text-error";
    rejectBtn.setAttribute("data-testid", "chat-approval-reject");
    rejectBtn.textContent = "Reject";
    rejectBtn.addEventListener("click", function () {
      a2uiEmit(rejectBtn, surfaceId, { componentId: comp.id, response: "reject" });
    });
    actions.appendChild(rejectBtn);

    var approveBtn = document.createElement("button");
    approveBtn.type = "button";
    approveBtn.className = "memory-approval__approve approval-approve btn btn-primary btn-sm";
    approveBtn.setAttribute("data-testid", "chat-approval-approve");
    approveBtn.textContent = "Approve";
    approveBtn.addEventListener("click", function () {
      a2uiEmit(approveBtn, surfaceId, { componentId: comp.id, response: "approve" });
    });
    actions.appendChild(approveBtn);

    shell.body.appendChild(actions);
    return shell.wrap;
  }

  // a2uiQuestion renders an A2UI question component with the same card skeleton,
  // hook classes and data-testids as the canonical chat question card
  // (chat-stream.js renderQuestion). Options answer directly on click — the A2UI
  // surface has no two-step select/submit — so the option rows reuse the
  // canonical option hook classes and the chat-question-option testid, while the
  // submit button/footer are omitted. Choices dispatch an `a2ui:action`
  // CustomEvent.
  function a2uiQuestion(comp, surfaceId, canDecide) {
    var shell = a2uiShell();
    shell.card.className =
      "memory-question card card-border bg-base-100 w-fit min-w-[min(100%,30rem)] max-w-[85%] shadow-sm";
    shell.card.setAttribute("data-testid", "chat-question-card");
    shell.card.setAttribute("data-question-id", surfaceId || "");

    var body = document.createElement("div");
    body.className = "memory-question__body q-body memory-md text-base leading-snug break-words";
    body.textContent = (comp.prompt != null && comp.prompt !== "") ? comp.prompt : "Question";
    shell.body.appendChild(body);

    // Read-only when the host disables end-user decisions: no option rows, no
    // decision controls — the prompt stays visible (mirrors chat-stream.js).
    if (!canDecide) {
      shell.body.appendChild(a2uiApprovalsDisabledNote());
      return shell.wrap;
    }

    var options = comp.options;
    if (Array.isArray(options) && options.length) {
      var optsEl = document.createElement("div");
      optsEl.className = "memory-question__options q-options flex flex-col gap-2";
      for (var i = 0; i < options.length; i++) {
        (function (opt) {
          var isObj = opt && typeof opt === "object";
          var value = isObj ? (opt.value != null ? opt.value : opt.label) : opt;
          var label = isObj ? (opt.label != null ? opt.label : opt.value) : opt;
          var description = isObj ? opt.description : null;

          var row = document.createElement("button");
          row.type = "button";
          row.className =
            "memory-question__option flex w-full items-start gap-3 rounded-field border px-3 py-2.5 text-left transition-colors duration-150 cursor-pointer border-base-300/70 bg-base-100 hover:bg-base-200/50";
          row.setAttribute("data-testid", "chat-question-option");
          row.setAttribute("data-option-value", value == null ? "" : String(value));

          var ind = document.createElement("span");
          ind.className = "memory-question__indicator mt-0.5 flex shrink-0 items-center justify-center border-2 border-base-content/30 size-4.5 rounded-full";
          row.appendChild(ind);

          var textWrap = document.createElement("span");
          textWrap.className = "flex min-w-0 grow flex-col";
          var lab = document.createElement("span");
          lab.className = "memory-question__label text-sm leading-snug font-medium";
          lab.textContent = label == null ? "" : String(label);
          textWrap.appendChild(lab);
          if (description) {
            var desc = document.createElement("span");
            desc.className = "memory-question__description mt-0.5 text-xs leading-snug text-muted";
            desc.textContent = String(description);
            textWrap.appendChild(desc);
          }
          row.appendChild(textWrap);

          row.addEventListener("click", function () {
            a2uiEmit(row, surfaceId, { componentId: comp.id, response: value });
          });
          optsEl.appendChild(row);
        })(options[i]);
      }
      shell.body.appendChild(optsEl);
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
    // The entity's `id` is the object's canonical id when it is a UUID; render
    // the header as a link to its object page. A non-UUID id stays plain text.
    var entityHref = isUUID(comp.id) ? scopedPath("/objects/" + comp.id) : "";
    var entityName = (comp.label != null && comp.label !== "") ? String(comp.label) : "Entity";
    shell.body.appendChild(a2uiHeader(entityName, comp.type || "entity", entityHref));
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
        var target = relObj.target || relObj.entity || relObj.id || "";
        var targetHref = isUUID(target) ? scopedPath("/objects/" + target) : "";
        relRows.push([
          relObj.relation || relObj.type || relObj.label || "related",
          target,
          targetHref,
        ]);
      }
      shell.body.appendChild(a2uiRows(relRows));
    }
    return shell.wrap;
  }

  // a2uiSources renders the `sources` component: a list of item objects
  // {id, type, label, url}. Each item uses the SAME compact source row as the
  // in-bubble footer (human type label, link when the url/id is a safe object
  // ref); the count shows in the header. Empty/missing list falls back to a
  // quiet placeholder.
  function a2uiSources(comp, surfaceId) {
    var shell = a2uiShell();
    var items = Array.isArray(comp.items) ? comp.items : [];
    var rows = sourceRows(items);
    shell.body.appendChild(a2uiHeader("Sources", rows ? String(rows.count) : null));
    if (rows) {
      shell.body.appendChild(rows.list);
    } else {
      shell.body.appendChild(a2uiLabel("No sources."));
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
        span.className = "text-sm text-base-content break-words";
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
    var pairs = a2uiPairRows(comp.rows);
    if (pairs && pairs.length) {
      shell.body.appendChild(a2uiRows(pairs));
      return shell.wrap;
    }
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

  // A2UI surface actions are dispatched as bubbling `a2ui:action` events
  // (detail: {surfaceId, action}) from the clicked element, and sent by whichever
  // host owns the transcript: chat.js turns one into the conversation's next
  // turn. The renderer stays transport-free, so the same card works in the side
  // panel and the share page unchanged.

  /* ---------- shared timeline renderer ---------- */

  // isFailedRun / runDurationMs are run-lifecycle helpers for the renderer below.
  // isFailedRun mirrors chat.js's copy (still used by its header status).
  function isFailedRun(status) {
    return status === "failed" || status === "error";
  }

  // runDurationMs derives a turn's wall-clock duration from the run's
  // completed_at − created_at, falling back to the run_end's duration_ms.
  // Returns null when the run has not ended (no duration is shown then).
  function runDurationMs(runCtx, endItem) {
    var start = runCtx && runCtx.createdAt;
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

  // planToolRuns maps a timeline index to the consecutive group it belongs to.
  // A group is 2+ renderable tool calls with nothing that renders in between and
  // no boundary: thinking, assistant text, a question/approval, ask_user, an
  // awaiting-confirmation call or a run boundary splits it. A hidden item that
  // paints nothing (system/tool result, empty placeholder) does not split it.
  function planToolRuns(items, showRunMarkers) {
    var CS = window.MemoryChatStream || {};
    function isThinking(item) {
      if (!item || item.kind !== "message") return false;
      var content = item.content || {};
      if (item.role === "user" || item.role === "tool" || item.role === "system") return false;
      if (content.reasoning) return true;
      return !!(CS.isThinkingMessage && CS.isThinkingMessage(content));
    }
    function paints(item) {
      if (!item || typeof item !== "object") return false;
      if (item.kind === "tool_call") return true;
      if (item.kind === "run_start") return !!showRunMarkers;
      if (item.kind === "run_end") return !!showRunMarkers || isFailedRun(item.run_status);
      if (item.kind !== "message") return false;
      var content = item.content || {};
      var text = content.text || "";
      var html = content.html || "";
      if (item.role === "user") return !!(text && !(CS.isResumePrompt && CS.isResumePrompt(text)));
      if (item.role === "tool" || item.role === "system") return false;
      if (text && CS.isPauseNotice && CS.isPauseNotice(text)) return false;
      if (Array.isArray(content.a2ui) && content.a2ui.length) return true;
      return !!(text || html || content.reasoning);
    }
    // splits reports a boundary between tool calls even when the item itself
    // does not paint (a thinking item with no prose still ends a group).
    function splits(item) {
      if (!item || typeof item !== "object") return false;
      if (item.kind === "run_start" || item.kind === "run_end") return true;
      // A2UI surfaces and approval/question rows are activity boundaries even
      // though they are not part of the tool-call vocabulary here.
      if (item.kind === "ui" || item.kind === "a2ui" || item.kind === "surface") return true;
      if (item.kind === "tool_call") {
        return item.tool_name === "ask_user" || item.tool_status === "awaiting_confirmation";
      }
      if (item.kind !== "message") return false;
      if (item.role === "system" || item.role === "tool") return false;
      if (item.role === "user") return true;
      if (isThinking(item)) return true;
      var content = item.content || {};
      var text = content.text || "";
      if (text && CS.isPauseNotice && CS.isPauseNotice(text)) return false;
      if (Array.isArray(content.a2ui) && content.a2ui.length) return true;
      return !!(text || content.html || content.reasoning);
    }

    var plan = {};
    var i = 0;
    while (i < items.length) {
      var item = items[i];
      if (!paints(item) || item.kind !== "tool_call" || item.tool_name === "ask_user" ||
          item.tool_status === "awaiting_confirmation") {
        i++;
        continue;
      }
      var key = item.run_id || "";
      var members = [i];
      var j = i + 1;
      while (j < items.length) {
        var next = items[j];
        if (splits(next)) break;
        if (!paints(next)) {
          j++;
          continue;
        }
        if (next.kind === "tool_call" && next.tool_name !== "ask_user" &&
            next.tool_status !== "awaiting_confirmation" && (next.run_id || "") === key) {
          members.push(j);
          j++;
          continue;
        }
        break;
      }
      if (members.length > 1) {
        for (var m = 0; m < members.length; m++) {
          plan[members[m]] = {
            key: key,
            count: members.length,
            first: m === 0,
            last: m === members.length - 1,
          };
        }
      }
      i = members[members.length - 1] + 1;
    }
    return plan;
  }

  // renderTimeline walks a raw history payload (conversation or run) and paints
  // it into ctx.container. It is the single implementation behind the /chat
  // page's renderTimelineItems (chat.js) and the side panel's renderHistoryItems
  // (sidepanel.js): the item vocabulary, sorting and rendering are shared, and
  // the per-surface deviations are carried by `flags` (with page-local callbacks
  // in `ctx`).
  //
  // flags — the REAL deviations between the two call sites, no others:
  //   showRunMarkers  render run_start/run_end boundary rows (and the
  //                   failed-run fallback). Both surfaces default to false: the
  //                   boundaries are a debug affordance, hidden in the normal
  //                   chat transcript (#1381). Run lifecycle STATE is tracked
  //                   regardless (header, working placeholder, cancelable run
  //                   id, turn footers) — only the visible rows are gated. The
  //                   one exception is a FAILED run_end: its alert banner is
  //                   durable in the transcript even without this flag, because
  //                   a failure that vanishes on refresh is not a record (#2451).
  //                   The /chat page enables this via its debug switch (chat.js).
  //   showMeta        chat annotates bubbles/chips with "step N · rel time" on a
  //                   dedicated run transcript (currentScopeIsRun()); the side
  //                   panel never does. (The issue names runTranscript AND
  //                   showMeta; in the code both collapse to this one deviation —
  //                   the run-scope meta annotation — so it is a single flag.)
  //   silent          the side panel suppresses the per-bubble scrollToBottom and
  //                   scrolls once at the end; chat scrolls on every bubble.
  //
  // Thinking is NOT a flag: every surface renders it. An operator run writes
  // its answer text on the same record as the next tool call, so a surface that
  // skipped thinking items lost the assistant's prose, not just the reasoning —
  // which is exactly what the assistant drawer did.
  //
  // ctx — page-local hooks and state (kept on the host page):
  //   container                              required — the message list element
  //   badgeCtx                               required — agentPromptCard/thinking ctx
  //   getAgentName()                         assistant display name
  //   begin()                                pre-render DOM reset (chat preserves
  //                                          the working placeholder + hideEmpty)
  //   addUserMessage(text, silent, meta)
  //   addAssistantMessage(html, name, silent, meta) -> Element|null
  //   toolChip(tool, status, detail, payload)
  //   renderHistoryQuestion(input, questionId, answer)
  //   renderThinkingBlock(text, html, kind)  optional — a host that omits it
  //                                          skips thinking items instead of
  //                                          failing
  //   isThinkingItem(item, content)          surface-specific thinking guard
  //   stripContextPreamble(text)             optional
  //   pendingApprovals[], renderApproval({questionId,tool,input})   optional
  //   attachTurnFooters(records), onNewestRun(runId,status,ended), afterRender(status,ended)  optional
  function renderTimeline(items, ctx, flags) {
    if (!ctx || !ctx.container) return;
    flags = flags || {};
    var CS = window.MemoryChatStream || {};
    var container = ctx.container;
    var showRunMarkers = !!flags.showRunMarkers;
    var showMeta = !!flags.showMeta;
    var silent = !!flags.silent;

    if (ctx.begin) ctx.begin();
    // A timeline render is a full rebuild of the transcript: start with no
    // rendered surfaces so replay keys into a clean registry (see resetA2UI).
    resetA2UI(container);

    // The server timeline is already chronological, but be explicit about it:
    // stable-sort by effective time, then step_number (a per-run counter, so it
    // can't order across runs on its own), preserving array order on ties.
    // run_end items carry the run's start time in created_at and the real end
    // time in completed_at — the shared sortTimeline sorts by the latter so "Run
    // complete" lands after the run's content instead of right after "Run started".
    items = MemoryChatHost.sortTimeline(items || []);

    // The run's composed system instruction (recorded as a `system` message)
    // renders once, as a collapsed agent-prompt card above the transcript.
    // Later system records are skipped — the instruction is stable across a
    // conversation's runs, so a card per run would just duplicate it.
    for (var pi = 0; pi < items.length; pi++) {
      var pit = items[pi];
      if (pit && pit.kind === "message" && pit.role === "system") {
        var promptText = (pit.content && pit.content.text) || "";
        if (promptText) {
          if (agentPromptCard) agentPromptCard(ctx.badgeCtx, promptText);
          break;
        }
      }
    }

    var name = ctx.getAgentName ? ctx.getAgentName() : "";
    var runStatus = ""; // status of the run being rendered (set by run_start)
    var runError = ""; // error_message of that run, if the server attached one
    var runEnded = false; // whether a run_end was seen for the current run
    var runCtx = null; // model/duration window for the run being rendered
    var turnFooters = []; // {el, ctx} — assistant bubbles, for footer attachment
    var newestRunId = ""; // active run id, resolved from the newest lifecycle item
    // A turn's consecutive steps fold into one run. The groups are planned
    // before the walk so a one-step turn still renders as a plain step and a
    // multi-step one is a run from its first chip; `openRun` is the run being
    // filled as the chips render.
    var toolRuns = planToolRuns(items, showRunMarkers);
    var openRun = null;
    var newestRunStatus = "";
    var newestRunEnded = false;
    // Contiguous reasoning merges into one collapsed Thinking row. The previous
    // timeline item must itself be thinking (and on the same run) for the merge.
    var thinkingRec = null;
    var thinkingRun = "";
    var prevWasThinking = false;
    // The thinking-item branch renders planning monologues (function_calls).
    // A message can also carry reasoning plus an answer; that answer still
    // renders below its Thinking block, so reasoning is handled separately in
    // the assistant branch rather than swallowed here.
    function itemIsThinking(it) {
      if (!it || it.kind !== "message") return false;
      var c = it.content || {};
      if (it.role === "system" || it.role === "tool" || it.role === "user") return false;
      return ctx.isThinkingItem ? ctx.isThinkingItem(it, c) : CS.isThinkingMessage(c);
    }
    function itemProducesThinking(it) {
      if (!it || it.kind !== "message") return false;
      var c = it.content || {};
      if (it.role === "system" || it.role === "tool" || it.role === "user") return false;
      if (itemIsThinking(it)) return true;
      // Reasoning that comes with an answer renders the answer after it, so a
      // following reasoning item must not merge back above that answer.
      return !!c.reasoning && !c.html && !c.text;
    }
    function appendThinkingContent(rec, text, html) {
      if (!rec || !rec.body) return;
      if (html) {
        rec.body.insertAdjacentHTML("beforeend", html);
      }
      if (text) {
        if ((rec.body.textContent || "").trim()) {
          rec.body.appendChild(document.createTextNode("\n\n"));
        }
        rec.body.appendChild(document.createTextNode(text));
      }
    }
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      if (!item || typeof item !== "object") continue;
      var currentThinking = itemProducesThinking(item);
      // Run-transcript annotation: step number + relative time under each
      // message/tool item. Only on a dedicated run page (flags.showMeta);
      // conversation transcripts and the live stream render no meta at all.
      var meta = showMeta
        ? ("step " + (item.step_number || "") + (item.created_at ? " · " + CS.relTime(item.created_at) : ""))
        : "";
      switch (item.kind) {
        case "run_start":
          // Run lifecycle state is tracked for every surface — the chat header,
          // the working placeholder, the cancelable run id and the turn footers
          // all read it; only the visible boundary row is debug-gated (#1381).
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
          // Typed turn boundary carrying the run's model. The start marker is a
          // divider, never a terminal status: run_status here is the run's FINAL
          // status once history is serialized, so stamping it on the start
          // marker made a failed run's "Run started" row inherit the
          // [data-status="failed"] banner layout (display:block) — killing its
          // flex row and collapsing it to a left-aligned unspaced line (#1300).
          // The end marker (or the no-run_end fallback below) owns the failure.
          if (showRunMarkers) {
            container.appendChild(runMarker({
              phase: "start",
              model: runCtx.model,
            }));
          }
          break;
        case "run_end":
          // State first (see run_start); the boundary row is debug-only (#1381).
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
          // input-required ("waiting on you"). The successful/non-failure
          // boundaries stay debug-only (#1381), but a failure is a durable
          // transcript row — a header badge that clears on refresh is not a
          // record of what happened (#2451).
          var endStatus = item.run_status || runStatus;
          if (showRunMarkers || isFailedRun(endStatus)) {
            container.appendChild(runMarker({
              phase: "end",
              status: endStatus,
              error: item.error_message || runError,
            }));
          }
          break;
        case "tool_call":
          // ask_user never renders as a tool chip. A pending question renders
          // as an interactive card; an answered one (the gateway annotates
          // tool_output.response) renders as a static card with the chosen
          // answer highlighted.
          if (item.tool_name === "ask_user") {
            var qout = item.tool_output || {};
            if (qout.question_id) {
              // Pending and answered questions both render inline in the
              // transcript; renderHistoryQuestion renders the interactive card
              // for an unanswered one and the static "Answered" card otherwise.
              ctx.renderHistoryQuestion(item.tool_input || {}, qout.question_id, qout.response);
            }
            break;
          }
          // Tool activity from history: same chip language as live streaming, and
          // the same vocabulary — the category, subject and view the gateway
          // attached to the item (see enrichToolItem), so a run read back describes
          // a call the way watching it did.
          var out = item.tool_output;
          var view = item.tool_view && typeof item.tool_view === "object" ? item.tool_view : null;
          var cls = CS.classifyTool(item.tool_status, out, view);
          // Fold this turn's consecutive steps into one run: the group is created
          // at its first step, filled as the chips render, and settled at its last.
          var plannedRun = toolRuns[i];
          if (!plannedRun) {
            openRun = null;
          } else if (!openRun || plannedRun.first) {
            openRun = createToolRun(container, plannedRun.key, { openDefault: false });
          }
          // A step outside a run keeps its own row, exactly as it rendered before
          // runs existed; the caller owns placement, so the chip builder never
          // mistakes a recorded step for a live one.
          var chipHost = openRun ? openRun.body : null;
          if (!chipHost) {
            chipHost = auxShell();
            container.appendChild(chipHost);
          }
          var startedAt = item.created_at ? Date.parse(item.created_at) : 0;
          if (!isFinite(startedAt)) startedAt = 0;
          var settledAt = startedAt;
          if (startedAt && typeof item.duration_ms === "number" && item.duration_ms > 0) {
            settledAt = startedAt + item.duration_ms;
          }
          var chipEl = ctx.toolChip(item.tool_name || "tool", cls.status, cls.line || cls.summary || "", {
            tool: item.tool_name || "tool",
            status: cls.status,
            summary: cls.summary,
            error: cls.error,
            input: item.tool_input,
            output: out,
            inputHtml: item.tool_input_html,
            outputHtml: item.tool_output_html,
            view: view,
            category: item.tool_category,
            subject: item.tool_subject,
            id: item.id,
            durationMs: item.duration_ms,
            meta: meta,
            host: chipHost,
          });
          if (openRun) {
            openRun.addStep(chipEl, {
              tool: item.tool_name || "tool",
              startedAt: startedAt,
              settledAt: settledAt,
              failed: cls.status === "error",
              running: cls.status === "running" || cls.status === "awaiting_confirmation",
            });
            if (plannedRun.last) {
              openRun.finish();
              openRun = null;
            }
          }
          break;
        case "message":
          var content = item.content || {};
          var text = content.text || "";
          var html = content.html || "";
          // Recorded pipeline provenance (see stageInputCard): the stage that
          // produced the message and which side of the stage the text is.
          var stage = typeof content.stage === "string" ? content.stage : "";
          var stageKind = typeof content.stage_kind === "string" ? content.stage_kind : "";
          // A persisted surface is a first-class replayable item (#427): render
          // the card(s) at the item's transcript position, even when the turn
          // carries no text at all (a card-only answer was invisible on reload).
          if (Array.isArray(content.a2ui) && content.a2ui.length &&
              item.role !== "system" && item.role !== "user" && item.role !== "tool") {
            renderA2UISurface(a2uiSurfaceIDOf(content.a2ui), content.a2ui, {
              messages: container,
              // Scope the surface's rendered nodes to this timeline item: two
              // turns may both name their surface "s1", and the registry must
              // not let the later render replace the earlier card.
              surfaceKey: item.id != null && item.id !== "" ? String(item.id) : "item-" + i,
              hideEmpty: ctx.hideEmpty,
              scrollToBottom: silent ? function () {} : (ctx.scrollToBottom || function () {}),
            });
          }
          if (item.role === "system") {
            // The composed system instruction — already rendered once as the
            // agent-prompt card above; never a chat bubble.
            break;
          }
          if (item.role === "user") {
            if (text && !CS.isResumePrompt(text)) {
              if (stage) {
                // A recorded stage prompt (or the run header) is not something
                // the operator typed: it renders as a collapsed, stage-labelled
                // card instead of a "You" bubble carrying the whole document.
                stageInputCard(ctx.badgeCtx, stage, stageKind, text, meta);
              } else {
                var userText = ctx.stripContextPreamble ? ctx.stripContextPreamble(text) : text;
                ctx.addUserMessage(userText, silent, meta);
              }
            }
          } else if (item.role === "tool") {
            // Tool result — already shown by the preceding tool_call chip.
            break;
          } else if (!stage && itemIsThinking(item)) {
            // Planning/thinking monologue (carries function_calls): a collapsed
            // Thinking block on every surface, exactly like the live stream.
            // Adjacent reasoning items in the same run merge into that one row;
            // any other timeline item ends the block (prevWasThinking).
            if ((text || html) && ctx.renderThinkingBlock) {
              var sameRun = !thinkingRun || !item.run_id || item.run_id === thinkingRun;
              if (prevWasThinking && thinkingRec && sameRun) {
                appendThinkingContent(thinkingRec, text, html);
              } else {
                thinkingRec = ctx.renderThinkingBlock(text, html, "reasoning");
              }
              if (item.run_id) thinkingRun = item.run_id;
            }
          } else if (CS.isPauseNotice(text)) {
            // Synthetic "Execution paused…" message the executor injects when a
            // run pauses on ask_user — the question card already conveys this.
            break;
          } else if (text || html || content.reasoning) {
            // assistant / diane: skip function-call-only and empty messages.
            // The server splits the chain-of-thought into content.reasoning
            // (rendered as a Thinking block above) and renders the reply
            // markdown into content.html.
            if (content.reasoning && ctx.renderThinkingBlock) {
              var rRun = !thinkingRun || !item.run_id || item.run_id === thinkingRun;
              if (prevWasThinking && thinkingRec && rRun) {
                appendThinkingContent(thinkingRec, content.reasoning, null);
              } else {
                thinkingRec = ctx.renderThinkingBlock(content.reasoning, null, "reasoning");
              }
              if (item.run_id) thinkingRun = item.run_id;
            }
            // A recorded stage message is authored by its stage (the model that
            // answered, or the processor that emitted the status line), which is
            // more specific than the run's agent; the bubble says who produced it.
            // A non-answer kind (an error) is stated in the header rather than as
            // markup inside the text.
            var authoredBy = stage || name;
            if (stage && stageKind && stageKind !== "output" && stageKind !== "header") {
              authoredBy = stage + " · " + stageKindLabel(stageKind);
            }
            // Paseo-style assistant turns have no header, except recorded
            // pipeline stages where the stage name is provenance the operator
            // needs to see.
            var turnEl = ctx.addAssistantMessage(
              html || escapeHTML(text),
              authoredBy,
              silent,
              meta,
              stage ? { header: authoredBy } : null
            );
            if (turnEl && runCtx) turnFooters.push({ el: turnEl, ctx: runCtx });
            // Grounded citations ride on the history item; render the Sources
            // block beneath the answer (idempotent, no-op when empty).
            if (turnEl && item.citations) attachSources(turnEl, item.citations);
          }
          break;
      }
      prevWasThinking = currentThinking;
    }

    // A run with no run_end item (partial/older history) still has to surface
    // its failure, so fall back to the run_start's status once the loop ends.
    // Like the end marker it mimics, the failure path is durable even when the
    // debug boundary rows are off (#2451).
    if (!runEnded && isFailedRun(runStatus)) {
      container.appendChild(runMarker({ phase: "end", status: runStatus, error: runError }));
    }

    // One footer per completed turn, on the turn's last assistant bubble.
    if (ctx.attachTurnFooters) ctx.attachTurnFooters(turnFooters);
    // Remember the newest run so the stop action can cancel it.
    if (ctx.onNewestRun) ctx.onNewestRun(newestRunId, newestRunStatus, newestRunEnded);

    // Pending tool approvals (a run paused awaiting a human decision) render as
    // the same interactive Approve/Reject/Cancel cards as live approval events,
    // inline in the transcript.
    var pendingApprovals = ctx.pendingApprovals || [];
    if (ctx.renderApproval) {
      for (var p = 0; p < pendingApprovals.length; p++) {
        var pa = pendingApprovals[p];
        if (!pa || !pa.questionId) continue;
        ctx.renderApproval({ questionId: pa.questionId, tool: pa.tool, input: pa.input });
      }
    }

    if (ctx.afterRender) ctx.afterRender(newestRunStatus, newestRunEnded);
  }

  /* ---------- subagent composer track ---------- */

  // normalizeSubagentStatus folds the server's run lifecycle into the statuses
  // the track understands. Unknown values pass through so a newer server field
  // is not silently dropped.
  function normalizeSubagentStatus(raw) {
    var t = String(raw == null ? "" : raw).trim().toLowerCase();
    // Persisted runstatus values: "submitted" = queued, "working" = running.
    // Keeping these distinct is what lets history rebuild drop a queued child
    // (it runs in another worker and has no parent-stream terminal projection).
    if (t === "submitted") return "queued";
    if (t === "working" || t === "in_progress" || t === "in-progress") return "running";
    if (t === "input-required" || t === "input_required") return "paused";
    if (t === "error") return "failed";
    if (t === "cancelled") return "canceled";
    return t;
  }

  function subagentBucket(status) {
    if (status === "queued" || status === "running") return "working";
    if (status === "paused") return "paused";
    if (status === "failed") return "failed";
    return "idle";
  }

  // createSubagentTrack renders the composer pill + popover for live subagent
  // frames and rebuilt history. It is intentionally mount-optional: the public
  // share page and the side panel can feed it frames and lose nothing when
  // there is no composer to hang the pill on.
  function createSubagentTrack(opts) {
    opts = opts || {};
    var getMount = typeof opts.mount === "function" ? opts.mount : function () { return opts.mount; };
    var openRunHref = typeof opts.openRun === "function"
      ? opts.openRun
      : function (runId) { return scopedPath("/runs/" + encodeURIComponent(runId)); };

    var state = {};
    var order = [];
    var currentParent = "";
    var open = false;

    function mountEl() {
      var m = getMount && getMount();
      return m && m.nodeType === 1 ? m : null;
    }

    function findKey(evt) {
      var runId = evt.runId ? String(evt.runId) : "";
      var id = evt.id ? String(evt.id) : "";
      if (runId && state[runId]) return runId;
      for (var i = 0; i < order.length; i++) {
        var row = state[order[i]];
        if (!row) continue;
        if (id && row.id === id) return order[i];
        if (runId && row.runId === runId) return order[i];
      }
      return runId || id;
    }

    function insert(row) {
      if (!row) return;
      var key = row.runId || row.id;
      if (!key) return;
      var existing = state[key] ? key : findKey(row);
      if (existing && state[existing]) {
        var cur = state[existing];
        if (row.id) cur.id = row.id;
        if (row.runId) cur.runId = row.runId;
        if (row.agentName) cur.agentName = row.agentName;
        if (row.task) cur.task = row.task;
        if (row.status) cur.status = row.status;
        if (row.error != null) cur.error = row.error;
        return;
      }
      state[key] = {
        id: row.id || "",
        runId: row.runId || "",
        agentName: row.agentName || "",
        task: row.task || "",
        status: row.status || "running",
        error: row.error || "",
      };
      order.push(key);
    }

    function upsert(evt) {
      if (!evt) return;
      var parent = evt.parentRunId ? String(evt.parentRunId) : "";
      if (parent && currentParent && parent !== currentParent) {
        state = {};
        order = [];
      }
      if (parent) currentParent = parent;
      insert({
        id: evt.id ? String(evt.id) : "",
        runId: evt.runId ? String(evt.runId) : "",
        agentName: evt.agentName ? String(evt.agentName) : "",
        task: evt.task ? String(evt.task) : "",
        status: normalizeSubagentStatus(evt.status),
        error: evt.error == null ? "" : String(evt.error),
      });
      render();
    }

    function unwrapOutput(raw) {
      if (!raw || typeof raw !== "object") return {};
      if (raw.data && typeof raw.data === "object") return raw.data;
      if (raw.result && typeof raw.result === "object" && !raw.results) return raw.result;
      return raw;
    }

    function statusFromRun(raw, hasOutput) {
      if (!hasOutput) return "running";
      var s = normalizeSubagentStatus(raw);
      return s || "completed";
    }

    function rebuild(items) {
      state = {};
      order = [];
      currentParent = "";
      var list = Array.isArray(items) ? items : [];
      var newestRun = "";
      for (var r = 0; r < list.length; r++) {
        var it = list[r];
        if (it && (it.kind === "run_start" || it.kind === "run_end") && it.run_id) newestRun = it.run_id;
      }
      var calls = [];
      for (var i = 0; i < list.length; i++) {
        var item = list[i];
        if (!item || item.kind !== "tool_call") continue;
        var name = String(item.tool_name || "").replace(/[-_]/g, "_");
        if (name !== "spawn_agents" && name !== "trigger_agent") continue;
        if (newestRun && item.run_id && item.run_id !== newestRun) continue;
        calls.push(item);
      }
      for (var c = 0; c < calls.length; c++) {
        var call = calls[c];
        var tool = String(call.tool_name || "").replace(/[-_]/g, "_");
        var input = call.tool_input && typeof call.tool_input === "object" ? call.tool_input : {};
        var rawOut = call.tool_output;
        var out = unwrapOutput(rawOut);
        var hasOutput = rawOut !== undefined && rawOut !== null;
        // A spawn/trigger call with no terminal output has no authoritative
        // child status. Live `subagent` frames (and live_replay) own the
        // in-flight rows; synthesizing them here would duplicate those rows
        // with a non-correlatable id on every mid-run transcript refresh.
        if (!hasOutput) continue;
        if (tool === "spawn_agents") {
          var reqs = Array.isArray(input.agents) ? input.agents : [];
          var results = Array.isArray(out.results) ? out.results : [];
          var total = Math.max(reqs.length, results.length);
          for (var q = 0; q < total; q++) {
            var req = reqs[q] || {};
            var res = results[q] || {};
            var runId = res.run_id || "";
            var spawnStatus = statusFromRun(res.status, hasOutput && results.length > 0);
            // A queued child is dispatched to another worker and has no parent
            // stream handle for its terminal state. Showing it would leave a
            // permanent "working" row after reload.
            if (spawnStatus === "queued") continue;
            insert({
              id: runId || (call.id ? call.id + ":" + q : "spawn:" + c + ":" + q),
              runId: runId,
              agentName: res.agent_name || req.agent_name || "",
              task: req.task || res.task || "",
              status: spawnStatus,
              error: res.error || "",
            });
          }
        } else {
          var msg = input.message;
          var task = "";
          if (typeof msg === "string") task = msg;
          else if (msg && typeof msg === "object" && typeof msg.instructions === "string") task = msg.instructions;
          if (!task && typeof out.message === "string") task = out.message;
          var tRun = out.run_id || (out.data && out.data.run_id) || "";
          var triggerStatus = statusFromRun(out.status, hasOutput);
          // Same queued caveat as spawn_agents: no terminal projection for a
          // child running in another worker.
          if (triggerStatus === "queued") continue;
          insert({
            id: tRun || (call.id ? String(call.id) : ""),
            runId: tRun,
            agentName: input.agent_name || out.agent_name || "",
            task: task,
            status: triggerStatus,
            error: out.error || "",
          });
        }
      }
      render();
    }

    function segHTML(kind, count) {
      if (kind === "working") {
        return '<span class="memory-subagent-seg" data-status="working">' +
          '<span class="iconify lucide--loader-circle size-3 animate-spin" aria-hidden="true"></span>' +
          '<span>' + count + " working</span></span>";
      }
      if (kind === "paused") {
        return '<span class="memory-subagent-seg" data-status="paused">' +
          '<span class="iconify lucide--pause size-3" aria-hidden="true"></span>' +
          '<span>' + count + " needs input</span></span>";
      }
      return '<span class="memory-subagent-seg" data-status="failed">' +
        '<span class="iconify lucide--alert-triangle size-3" aria-hidden="true"></span>' +
        '<span>' + count + " failed</span></span>";
    }

    function rowHTML(key) {
      var r = state[key];
      if (!r) return "";
      var href = r.runId ? openRunHref(r.runId) : "";
      var tag = href ? "a" : "div";
      var attrs = href
        ? ' href="' + escapeHTML(href) + '" target="_blank" rel="noopener"'
        : "";
      return "<" + tag + ' class="memory-subagent-row" data-testid="chat-subagent-row" data-status="' +
        escapeHTML(r.status) + '"' + (r.runId ? ' data-run-id="' + escapeHTML(r.runId) + '"' : "") + attrs + ">" +
        '<span class="memory-subagent-dot" data-status="' + escapeHTML(subagentBucket(r.status)) + '" aria-hidden="true"></span>' +
        '<span class="memory-subagent-copy">' +
        '<span class="memory-subagent-name">' + escapeHTML(r.agentName || "Subagent") + "</span>" +
        (r.task ? '<span class="memory-subagent-task">' + escapeHTML(r.task) + "</span>" : "") +
        (r.error ? '<span class="memory-subagent-error">' + escapeHTML(r.error) + "</span>" : "") +
        "</span>" +
        (href ? '<span class="iconify lucide--external-link size-3.5 text-muted-faint" aria-hidden="true"></span>' : "") +
        "</" + tag + ">";
    }

    function render() {
      var mount = mountEl();
      if (!mount) return;
      if (!order.length) {
        mount.classList.add("hidden");
        mount.innerHTML = "";
        return;
      }
      mount.classList.remove("hidden");
      var working = 0, paused = 0, failed = 0;
      for (var i = 0; i < order.length; i++) {
        var b = subagentBucket(state[order[i]].status);
        if (b === "working") working++;
        else if (b === "paused") paused++;
        else if (b === "failed") failed++;
      }
      var segs = [];
      if (working) segs.push(segHTML("working", working));
      if (paused) segs.push(segHTML("paused", paused));
      if (failed) segs.push(segHTML("failed", failed));
      if (!segs.length) {
        segs.push('<span class="memory-subagent-fallback">' + order.length +
          (order.length === 1 ? " subagent" : " subagents") + "</span>");
      }
      var rows = "";
      for (var j = 0; j < order.length; j++) rows += rowHTML(order[j]);
      mount.innerHTML =
        '<div class="memory-subagent-track" data-testid="chat-subagent-track">' +
        '<button type="button" class="memory-subagent-pill" data-testid="chat-subagent-pill" aria-expanded="' +
        (open ? "true" : "false") + '">' +
        '<span class="iconify lucide--users size-3.5 text-muted-faint" aria-hidden="true"></span>' +
        segs.join('<span class="memory-subagent-sep" aria-hidden="true">·</span>') +
        '<span class="memory-subagent-chevron iconify lucide--chevron-up size-3.5 text-muted-faint" aria-hidden="true"></span>' +
        "</button>" +
        '<div class="memory-subagent-popover' + (open ? "" : " hidden") +
        '" data-testid="chat-subagent-popover" role="dialog" aria-label="Subagents">' +
        rows + "</div></div>";
      var pill = mount.querySelector(".memory-subagent-pill");
      if (pill) {
        pill.addEventListener("click", function () {
          open = !open;
          render();
        });
      }
    }

    // Outside click / Escape close the popover. Registered once per track.
    document.addEventListener("click", function (ev) {
      if (!open) return;
      var mount = mountEl();
      if (!mount) return;
      if (mount.contains(ev.target)) return;
      open = false;
      render();
    });
    document.addEventListener("keydown", function (ev) {
      if (ev.key !== "Escape" || !open) return;
      open = false;
      render();
    });

    function reset() {
      state = {};
      order = [];
      currentParent = "";
      open = false;
      render();
    }

    return {
      upsert: upsert,
      rebuild: rebuild,
      reset: reset,
      render: render,
      _state: function () { return state; },
    };
  }

  window.MemoryChatComponents = {
    escapeHTML: escapeHTML,
    humanizeToolName: humanizeToolName,
    toolLabel: toolLabel,
    toolIcon: toolIcon,
    renderToolDetails: renderToolDetails,
    toolBodySection: toolBodySection,
    rawPayloadSection: rawPayloadSection,
    rawTextSection: rawTextSection,
    remainderLine: remainderLine,
    detailSection: detailSection,
    errorSection: errorSection,
    summarySection: summarySection,
    rawSection: rawSection,
    expandableBadge: expandableBadge,
    ensureBadgeStyle: ensureBadgeStyle,
    createThinkingBlock: createThinkingBlock,
    createThinkingHost: createThinkingHost,
    agentPromptCard: agentPromptCard,
    formatDurationMs: formatDurationMs,
    makeCopyButton: makeCopyButton,
    enhanceMessage: enhanceMessage,
    turnFooter: turnFooter,
    runMarker: runMarker,
    renderA2UISurface: renderA2UISurface,
    resetA2UI: resetA2UI,
    auxShell: auxShell,
    createToolRun: createToolRun,
    captureLiveToolRunChoice: captureLiveToolRunChoice,
    adoptNewestToolRunChoice: adoptNewestToolRunChoice,
    renderTimeline: renderTimeline,
    humanizeTypeName: humanizeTypeName,
    sourcesFooter: sourcesFooter,
    attachSources: attachSources,
    createSubagentTrack: createSubagentTrack,
    normalizeSubagentStatus: normalizeSubagentStatus,
  };
})();
