/* Memory web UI — shared chat streaming engine.
   Loaded by BOTH the /chat page (chat.js) and the global side panel
   (sidepanel.js) so streaming, bubbles, tool chips, and question/approval
   cards behave identically everywhere and there is exactly one
   implementation to keep in sync.

   Pure helpers (normalizeStatus, summarizeText, relTime, …) are
   self-contained. The stateful engine (createEngine) takes a ctx object —
   getters/setters into the host page's IIFE state plus page-local callbacks
   (answerQuestion/postDecision/onStreamStart/onStreamFinish/…) — supplied by
   each page. Each page owns its own message stream container and state, so
   nothing is shared mutable state.

   page-local functions that intentionally stay in the host pages (they are
   genuinely different there): answerQuestion, postDecision, handleEvent,
   thinking-block handling, session rails, persistence. */
(function () {
  "use strict";

  // Re-exported from chat-components.js (loaded first). Captured at load time;
  // fall back to a local copy only if the components module is somehow absent.
  var escapeHTML = (window.MemoryChatComponents && window.MemoryChatComponents.escapeHTML) || function (s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  };

  function notify(kind, message) {
    if (window.MemoryApp && window.MemoryApp.toast) {
      window.MemoryApp.toast(kind, message);
    }
  }

  // Humanized relative time, mirroring the server-side relTime() helper.
  function relTime(iso) {
    if (!iso) return "—";
    var t = new Date(iso);
    if (isNaN(t.getTime())) return iso;
    var mins = Math.floor((Date.now() - t.getTime()) / 60000);
    if (mins < 1) return "just now";
    if (mins < 60) return mins + "m ago";
    var hours = Math.floor(mins / 60);
    if (hours < 24) return hours + "h ago";
    var days = Math.floor(hours / 24);
    if (days < 7) return days + "d ago";
    return iso.slice(0, 10);
  }

  // isPauseNotice reports whether an assistant message is one of the synthetic
  // "Execution paused…" notices the executor injects when a run pauses.
  function isPauseNotice(text) {
    return text === "Execution paused. Waiting for user response to your question." ||
           text === "Execution paused. Waiting for user approval of tool call.";
  }

  // isResumePrompt reports whether a user-role message is the internal resume
  // prompt Memory injects when it resumes a paused run ("Previously you asked…
  // The user responded… Continue from where you left off."). It is not a real
  // user message and must not render as a "You" bubble.
  function isResumePrompt(text) {
    return text.indexOf("Continue from where you left off.") !== -1;
  }

  // isThinkingMessage reports whether an operator/assistant message is the
  // planning monologue (non-empty function_calls array), not the final answer.
  function isThinkingMessage(content) {
    return !!(
      content &&
      Array.isArray(content.function_calls) &&
      content.function_calls.length > 0
    );
  }

  // Tool-result normalization, shared by live SSE and history rendering.
  // The memory service (and any other MCP server) reports results in
  // heterogeneous shapes, so we classify generically at the boundary:
  //   status  ∈ running | ok | error
  //   error   → human-readable failure string ("" when ok)
  //   summary → one-line result summary
  function normalizeStatus(raw) {
    switch (raw) {
      case "running":
      case "started":
      case "in_progress":
        return "running";
      case "ok":
      case "completed":
      case "success":
        return "ok";
      case "error":
      case "failed":
      case "failure":
        return "error";
      default:
        return raw || "running";
    }
  }

  // hasFailureSignal detects an explicit failure in a tool result object,
  // across the error conventions used by different MCP servers. `message` is
  // deliberately NOT a signal — memory sets it on success too.
  function hasFailureSignal(out) {
    if (!out || typeof out !== "object" || Array.isArray(out)) return false;
    if (out.is_error === true) return true;
    if (out.ok === false) return true;
    if (out.success === false) return true; // boolean false (HA-style)
    if (typeof out.error === "string" && out.error) return true;
    if (Array.isArray(out.errors) && out.errors.length) return true;
    if (typeof out.failed === "number" && out.failed > 0) return true;
    var results = Array.isArray(out.results) ? out.results : null;
    if (results) {
      for (var i = 0; i < results.length; i++) {
        var r = results[i];
        if (r && typeof r === "object" &&
            (r.success === false || (typeof r.error === "string" && r.error))) {
          return true;
        }
      }
    }
    return false;
  }

  // firstError returns the first concrete error string in a result object.
  function firstError(out) {
    if (!out || typeof out !== "object" || Array.isArray(out)) return "";
    if (typeof out.error === "string" && out.error) return out.error;
    if (Array.isArray(out.errors) && out.errors.length) return String(out.errors[0]);
    var results = Array.isArray(out.results) ? out.results : null;
    if (results) {
      for (var i = 0; i < results.length; i++) {
        var r = results[i];
        if (r && typeof r === "object" && typeof r.error === "string" && r.error) {
          return r.error;
        }
      }
    }
    return "";
  }

  // summarizeOutput distills a result object to one human line. Prefers the
  // tool's own message/result, then falls back to a count.
  function summarizeOutput(out) {
    var s = summarizeText(out);
    if (!s) return "";
    return s.length > 120 ? s.slice(0, 120) + "…" : s;
  }

  // summarizeText extracts the raw one-line summary; summarizeOutput clips it so
  // a huge JSON blob can't blow the chip past the chat width.
  function summarizeText(out) {
    if (!out) return "";
    if (typeof out === "string") return out;
    if (Array.isArray(out)) return out.length + " result" + (out.length === 1 ? "" : "s");
    if (typeof out.message === "string" && out.message) return out.message;
    if (typeof out.result === "string" && out.result) {
      // A JSON-string result (e.g. agent-def-list) is far more useful as a
      // count than as a raw blob.
      var parsed = tryParseJSON(out.result);
      if (Array.isArray(parsed)) return parsed.length + " result" + (parsed.length === 1 ? "" : "s");
      return out.result;
    }
    var total = typeof out.total === "number" ? out.total : null;
    if (total === null && out.pagination && typeof out.pagination.total === "number") {
      total = out.pagination.total;
    }
    if (total !== null) return total + " result" + (total === 1 ? "" : "s");
    if (typeof out.count === "number") return out.count + " item" + (out.count === 1 ? "" : "s");
    if (Array.isArray(out.entities)) {
      return out.entities.length + " entit" + (out.entities.length === 1 ? "y" : "ies");
    }
    if (Array.isArray(out.data)) return out.data.length + " result" + (out.data.length === 1 ? "" : "s");
    return "";
  }

  function tryParseJSON(s) {
    try { return JSON.parse(s); } catch (e) { return null; }
  }

  // classifyTool turns a raw tool status + output into {status, error, summary}.
  function classifyTool(rawStatus, output) {
    var status = normalizeStatus(rawStatus);
    var out = (output && typeof output === "object" && !Array.isArray(output)) ? output : null;
    var error = firstError(out);
    var failed = status === "error" || (status === "ok" && hasFailureSignal(out));
    if (failed) {
      status = "error";
      if (!error && out && typeof out.message === "string") error = out.message;
      if (!error) error = "tool failed";
    }
    return { status: status, error: error, summary: summarizeOutput(output) };
  }

  /* ---------- stateful engine (bound to a host page's ctx) ---------- */

  function createEngine(ctx) {

    /* ---------- composer ---------- */

    function autoGrow() {
      var input = ctx.input;
      if (!input) return;
      input.style.height = "auto";
      input.style.height = Math.min(input.scrollHeight, 160) + "px";
    }

    /* ---------- bubbles ---------- */

    // meta is an optional run-transcript annotation ("step N · 5m ago")
    // rendered as a quiet footer under the bubble when set; every other call
    // site (live stream, history, side panel) omits it → no footer.
    function addUserMessage(text, silent, meta) {
      ctx.hideEmpty();
      var wrap = document.createElement("div");
      wrap.className = "chat chat-end memory-rise";
      wrap.innerHTML =
        '<div class="chat-header text-xs text-base-content/50">You</div>' +
        '<div class="chat-bubble chat-bubble-primary max-w-[85%]"><p class="whitespace-pre-wrap break-words"></p></div>' +
        (meta
          ? '<div class="chat-footer mt-1 text-[11px] text-base-content/35 font-mono">' + escapeHTML(meta) + "</div>"
          : "");
      wrap.querySelector("p").textContent = text;
      ctx.messages.appendChild(wrap);
      if (!silent) ctx.scrollToBottom();
    }

    // meta behaves as in addUserMessage: appended under the bubble only when set.
    function addAssistantMessage(html, name, silent, meta) {
      ctx.hideEmpty();
      var wrap = document.createElement("div");
      wrap.className = "chat chat-start memory-rise";
      wrap.innerHTML =
        '<div class="chat-image bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
        '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>' +
        '<div class="chat-header text-xs text-base-content/50">' + escapeHTML(name || "Memory") + '</div>' +
        '<div class="chat-bubble chat-bubble-neutral max-w-[85%]"><div class="memory-md break-words"></div></div>' +
        (meta
          ? '<div class="chat-footer mt-1 text-[11px] text-base-content/35 font-mono">' + escapeHTML(meta) + "</div>"
          : "");
      wrap.querySelector(".memory-md").innerHTML = html;
      ctx.messages.appendChild(wrap);
      if (!silent) ctx.scrollToBottom();
    }

    function openAssistantBubble() {
      ctx.hideEmpty();
      var b = document.createElement("div");
      b.className = "chat chat-start memory-rise";
      b.innerHTML =
        '<div class="chat-image bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
        '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>' +
        '<div class="chat-header text-xs text-base-content/50">' + escapeHTML(ctx.currentAgentName()) + '</div>' +
        '<div class="chat-bubble chat-bubble-neutral max-w-[85%]"><div class="memory-md break-words"></div></div>';
      ctx.bubble = b;
      ctx.messages.appendChild(b);
      ctx.bubbleHTML = "";
      b._recorded = false;
      updateBubbleText();
      ctx.scrollToBottom();
      return b;
    }

    function updateBubbleText() {
      var b = ctx.bubble;
      if (!b) return;
      var el = b.querySelector(".memory-md");
      // animated typing indicator while waiting for the first token
      if (ctx.streaming && !ctx.bubbleHTML) {
        if (!b.querySelector(".memory-typing")) {
          el.innerHTML = '<span class="memory-typing"><span></span><span></span><span></span></span>';
        }
        return;
      }
      el.innerHTML = ctx.bubbleHTML;
      var caret = b.querySelector(".memory-caret");
      if (ctx.streaming) {
        if (!caret) {
          caret = document.createElement("span");
          caret.className = "memory-caret";
          el.appendChild(caret);
        }
      } else if (caret) {
        caret.remove();
      }
    }

    /* ---------- tool chips (shared badge shell from chat-components.js) ---------- */

    function toolChip(tool, status, detail, payload) {
      var p = payload || { tool: tool, status: status };

      // Same chat-start shell as thinking/assistant bubbles so the chip aligns
      // with (and is constrained to) the chat content column.
      var wrap = document.createElement("div");
      wrap.className = "chat chat-start memory-rise memory-aux";
      wrap.innerHTML =
        '<div class="chat-image invisible bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
        '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>';
      ctx.messages.appendChild(wrap);

      var chip = MemoryChatComponents.expandableBadge({
        className: "memory-tool-chip col-start-2 row-start-2 w-full",
        icon: "lucide--wrench",
        label: MemoryChatComponents.humanizeToolName(tool),
        live: status === "running",
        dataStatus: status,
        container: wrap,
        renderDetails: function (detailEl, rootEl) {
          MemoryChatComponents.renderToolDetails(detailEl, rootEl._payload || {});
        },
      }, ctx.badgeCtx);
      if (!chip) return null;
      chip.label.textContent = MemoryChatComponents.humanizeToolName(tool);
      chip.root.setAttribute("data-tool", tool); // stable raw key for live updates
      // Hover affordance: swap the wrench for a dropdown chevron (like thinking).
      var iconEl = chip.root.querySelector(".memory-badge-icon .iconify");
      if (chip.toggle) {
        chip.toggle.addEventListener("mouseenter", function () {
          if (iconEl) iconEl.setAttribute("class", "iconify lucide--chevron-down size-4 text-base-content/40");
        });
        chip.toggle.addEventListener("mouseleave", function () {
          if (iconEl) iconEl.setAttribute("class", "iconify lucide--wrench size-4 text-base-content/40");
        });
      }
      // structured detail for the inline expand (kept on the element, as before)
      chip.root._payload = p;
      p.tool = tool;
      p.status = status;
      setToolStatus(chip.root, status, detail);
      // Optional run-transcript annotation ("step N · 5m ago"): a quiet footer
      // under the chip, only when the caller set payload.meta (dedicated run
      // transcript page). Live streams / conversations / side panel omit it.
      if (p.meta) {
        var metaEl = document.createElement("div");
        metaEl.className = "mt-1 px-2 text-[11px] text-base-content/35 font-mono";
        metaEl.textContent = p.meta;
        chip.root.appendChild(metaEl);
      }
      // Badges live inline in the message stream, at the point they were called
      // — appended in arrival order so they interleave with the bubbles.
      ctx.scrollToBottom();
      return chip.root;
    }

    function setToolStatus(chip, status, detail) {
      // running → shimmer sweep on the label; the trailing status slot shows the
      // success/error spinner (the leading icon stays the tool wrench).
      chip.classList.toggle("memory-badge-live", status === "running");
      var secondary = chip.querySelector(".memory-badge-secondary");
      var statusEl = chip.querySelector(".memory-tool-status");
      if (!statusEl) {
        statusEl = document.createElement("span");
        statusEl.className = "memory-tool-status ml-auto flex shrink-0 items-center";
        var toggle = chip.querySelector(".memory-badge-toggle");
        var chevron = chip.querySelector(".memory-badge-chevron");
        if (toggle && chevron) toggle.insertBefore(statusEl, chevron);
      }
      var ic = "lucide--loader-circle";
      var tint = "text-base-content/40";
      var sec = "";
      switch (status) {
        case "running":
          tint = "text-base-content/40 animate-spin";
          break;
        case "ok":
          ic = "lucide--circle-check";
          tint = "text-success";
          sec = detail || "";
          break;
        case "error":
          ic = "lucide--alert-triangle";
          tint = "text-error";
          sec = detail || "error";
          break;
        default:
          sec = detail || status || "";
      }
      statusEl.innerHTML =
        '<span class="iconify ' + ic + ' size-3.5 ' + tint + '" aria-hidden="true"></span>';
      if (secondary) {
        if (sec) {
          secondary.textContent = sec;
          secondary.classList.remove("hidden");
        } else {
          secondary.classList.add("hidden");
        }
      }
      if (detail) chip.title = detail;
    }

    function handleToolEvent(evt) {
      var tool = evt.tool || "tool";
      var chips = Array.prototype.slice.call(ctx.messages.querySelectorAll(".memory-tool-chip"));
      var target = null;
      // Chips carry the raw tool id in data-tool (set by toolChip); the visible
      // label is humanized, so identity is matched on the attribute.
      for (var i = chips.length - 1; i >= 0; i--) {
        if (chips[i].getAttribute("data-tool") === tool) { target = chips[i]; break; }
      }

      // "started" events carry the input args in `result`, not output — don't
      // classify them. Just surface the chip as running.
      if (normalizeStatus(evt.status || "running") === "running") {
        if (!target) {
          toolChip(tool, "running", "", {
            tool: tool, status: "running", input: evt.result, inputHtml: evt.resultHtml,
          });
        }
        return;
      }

      var cls = classifyTool(evt.status, evt.result);
      if (evt.error) { cls.status = "error"; cls.error = evt.error; }
      var detail = cls.error || cls.summary || "";

      if (target) {
        setToolStatus(target, cls.status, detail);
        target.setAttribute("data-status", cls.status);
        target._payload = target._payload || { tool: tool };
        target._payload.status = cls.status;
        target._payload.summary = cls.summary;
        target._payload.error = cls.error;
        target._payload.output = evt.result;
        target._payload.outputHtml = evt.resultHtml;
      } else {
        toolChip(tool, cls.status, detail, {
          tool: tool, status: cls.status, summary: cls.summary, error: cls.error,
          output: evt.result, outputHtml: evt.resultHtml,
        });
      }
      ctx.scrollToBottom();
    }

    /* ---------- question cards (opencode-style prompt) ---------- */

    // Renders a single-answer / multi-answer / free-text question card from a
    // `question` SSE event. The card collects one answer, POSTs it back via
    // ctx.answerQuestion() (page-local), then the resumed run streams into a
    // fresh bubble.
    function renderQuestion(evt) {
      if (!ctx.messages) return;
      var q = evt.question || "Question";
      var questionId = evt.questionId;
      var type = evt.interactionType || "buttons";
      var opts = Array.isArray(evt.options) ? evt.options : [];
      var isText = type === "text";
      var isMulti = type === "multi_select";
      var answered = !!evt.answered;
      var answer = evt.answer;
      var hint = isMulti
        ? "Select all answers that apply"
        : isText ? "" : "Select one answer";

      ctx.hideEmpty();

      // Same shell as an assistant bubble so the card reads as the agent asking
      // (avatar + name header), with the card in the chat grid's bubble slot.
      var wrap = document.createElement("div");
      wrap.className = "chat chat-start memory-rise";
      wrap.innerHTML =
        '<div class="chat-image bg-primary/5 text-primary border-primary/10 flex items-center justify-center rounded-full border p-2">' +
        '<span class="iconify lucide--bot size-5" aria-hidden="true"></span></div>' +
        '<div class="chat-header text-xs text-base-content/50">' + escapeHTML(ctx.currentAgentName()) + '</div>';

      var card = document.createElement("div");
      card.className =
        "memory-question card card-border bg-base-100 col-start-2 row-start-2 w-fit min-w-[min(100%,30rem)] max-w-[85%] shadow-sm";
      card.innerHTML =
        '<div class="card-body gap-3 p-4">' +
        '<div class="q-body memory-md text-base leading-snug break-words">' + (evt.questionHtml || escapeHTML(q)) + '</div>' +
        (hint ? '<p class="mt-0.5 text-xs text-base-content/50">' + escapeHTML(hint) + '</p>' : "") +
        '<div class="q-options flex flex-col gap-2"></div>' +
        (answered
          ? '<div class="flex items-center justify-end gap-2 pt-1">' +
            '<span class="badge badge-success badge-sm gap-1">' +
            '<span class="iconify lucide--check size-3" aria-hidden="true"></span> Answered</span></div>'
          : '<div class="flex items-center justify-between gap-3 pt-1">' +
            '<span class="text-xs text-base-content/40">Esc to dismiss</span>' +
            '<button type="button" class="q-submit btn btn-primary btn-sm min-w-28" disabled>Submit</button>' +
            '</div>') +
        '</div>';
      wrap.appendChild(card);
      ctx.messages.appendChild(wrap);

      var optsEl = card.querySelector(".q-options");
      var submitBtn = card.querySelector(".q-submit");
      if (submitBtn) submitBtn.addEventListener("click", submitAnswer);
      var selectedValue = null; // single-select: chosen option value
      var selectedMap = {};     // multi-select: option value -> bool
      var rowOpts = [];         // option objects actually rendered (rows[i] <-> rowOpts[i])
      var rows = [];            // option row <button> elements
      var textInput = null;     // text-type <input>

      function rowCls(sel) {
        var c =
          "flex w-full items-start gap-3 rounded-xl border px-3 py-2.5 text-left transition-colors duration-150 cursor-pointer border-base-300/70 bg-base-100";
        return sel
          ? c + " border-primary bg-primary/5 ring-1 ring-primary/40 hover:bg-primary/10"
          : c + " hover:bg-base-200/50";
      }

      function indicatorCls(sel) {
        var c =
          "q-ind mt-0.5 flex shrink-0 items-center justify-center border-2 transition-colors duration-150 " +
          (isMulti ? "size-4.5 rounded-md" : "size-4.5 rounded-full");
        return sel ? c + " border-primary bg-primary" : c + " border-base-content/30";
      }

      function indicatorInner(sel) {
        if (!sel) return "";
        return isMulti
          ? '<span class="iconify lucide--check size-3 text-primary-content" aria-hidden="true"></span>'
          : '<span class="size-2 rounded-full bg-primary"></span>';
      }

      function buildRow(opt, i) {
        var row = document.createElement("button");
        row.type = "button";
        row.setAttribute("role", isMulti ? "checkbox" : "radio");
        row.setAttribute("aria-checked", "false");
        row.className = rowCls(false);

        var ind = document.createElement("span");
        ind.className = indicatorCls(false);
        row.appendChild(ind);

        var textWrap = document.createElement("span");
        textWrap.className = "flex min-w-0 grow flex-col";
        var label = document.createElement("span");
        label.className = "text-sm leading-snug font-medium";
        label.textContent = opt.label || opt.value || "Option " + (i + 1);
        textWrap.appendChild(label);
        if (opt.description) {
          var desc = document.createElement("span");
          desc.className = "mt-0.5 text-xs leading-snug text-base-content/50";
          desc.textContent = opt.description;
          textWrap.appendChild(desc);
        }
        row.appendChild(textWrap);

        row.addEventListener("click", function () {
          if (isMulti) {
            selectedMap[opt.value] = !selectedMap[opt.value];
          } else {
            selectedValue = opt.value;
          }
          refresh();
        });
        return row;
      }

      if (isText) {
        textInput = document.createElement("input");
        textInput.type = "text";
        textInput.className = "input w-full";
        if (evt.placeholder) textInput.placeholder = evt.placeholder;
        if (evt.maxLength && evt.maxLength > 0) textInput.maxLength = evt.maxLength;
        textInput.setAttribute("autocomplete", "off");
        textInput.setAttribute("aria-label", q);
        textInput.addEventListener("input", refresh);
        optsEl.appendChild(textInput);
      } else {
        for (var i = 0; i < opts.length; i++) {
          var o = opts[i];
          if (!o || o.value === undefined) continue;
          rows.push(buildRow(o, i));
          rowOpts.push(o);
          optsEl.appendChild(rows[rows.length - 1]);
        }
      }

      if (answered) {
        // Static "answered" state: pre-select the chosen option(s), disable the
        // controls, and skip the submit/keydown wiring entirely.
        if (isText) {
          textInput.value = answer || "";
          textInput.readOnly = true;
        } else {
          if (isMulti) {
            var arr = [];
            try { arr = JSON.parse(answer || "[]"); } catch (e) {}
            for (var i = 0; i < arr.length; i++) selectedMap[arr[i]] = true;
          } else {
            selectedValue = answer;
          }
          for (var i = 0; i < rows.length; i++) rows[i].disabled = true;
        }
        refresh();
        ctx.scrollToBottom();
        return;
      }

      function valid() {
        if (isText) return textInput.value.trim().length > 0;
        if (isMulti) {
          for (var i = 0; i < rowOpts.length; i++) {
            if (selectedMap[rowOpts[i].value]) return true;
          }
          return false;
        }
        return selectedValue !== null;
      }

      function refresh() {
        for (var i = 0; i < rows.length; i++) {
          var sel = isMulti ? !!selectedMap[rowOpts[i].value] : selectedValue === rowOpts[i].value;
          rows[i].className = rowCls(sel);
          rows[i].setAttribute("aria-checked", sel ? "true" : "false");
          var ind = rows[i].querySelector(".q-ind");
          ind.className = indicatorCls(sel);
          ind.innerHTML = indicatorInner(sel);
        }
        submitBtn && (submitBtn.disabled = !valid());
      }

      function answerValue() {
        if (isText) return textInput.value.trim();
        if (isMulti) {
          var vals = [];
          for (var i = 0; i < rowOpts.length; i++) {
            if (selectedMap[rowOpts[i].value]) vals.push(rowOpts[i].value);
          }
          return JSON.stringify(vals);
        }
        return selectedValue;
      }

      function cleanup() {
        document.removeEventListener("keydown", onKey);
        if (wrap.parentNode) wrap.parentNode.removeChild(wrap);
      }

      function dismiss() {
        cleanup(); // no answer: the gateway's stream stays ended
        ctx.scrollToBottom();
      }

      function submitAnswer() {
        if (!valid()) return;
        cleanup();
        ctx.answerQuestion(questionId, answerValue());
        ctx.scrollToBottom();
      }

      function onKey(ev) {
        if (ev.key === "Escape") {
          ev.preventDefault();
          dismiss();
          return;
        }
        if (ev.key === "Enter" && !ev.shiftKey &&
            ev.target && ev.target.closest && ev.target.closest(".memory-question")) {
          if (valid()) {
            ev.preventDefault();
            submitAnswer();
          }
        }
      }
      document.addEventListener("keydown", onKey);

      refresh();
      ctx.scrollToBottom();

      // focus the primary control so keyboard use works immediately
      if (isText) {
        textInput.focus();
      } else if (rows.length) {
        rows[0].focus();
      }
    }

    // renderHistoryQuestion maps a history ask_user tool_call (snake_case input)
    // onto the camelCase shape renderQuestion expects, so a paused run shows the
    // same interactive card as a live `question` SSE event.
    function renderHistoryQuestion(input, questionId, answer) {
      var opts = Array.isArray(input.options)
        ? input.options.map(function (o) {
            if (typeof o === "string") return { label: o, value: o };
            return { label: o.label, value: o.value, description: o.description };
          })
        : [];
      renderQuestion({
        questionId: questionId,
        question: input.question || "Question",
        questionHtml: input.question_html,
        interactionType: input.interaction_type || "buttons",
        options: opts,
        placeholder: input.placeholder,
        maxLength: input.max_length,
        answered: answer !== undefined && answer !== null,
        answer: answer,
      });
    }

    /* ---------- approval cards ---------- */

    // Renders an approval card for a tool-policy confirmation (approve / reject
    // with optional message / cancel), distinct from question cards. Decisions
    // go through ctx.postDecision() (page-local) with a completion note.
    function renderApproval(evt) {
      if (!ctx.messages) return;
      var questionId = evt.questionId;
      var tool = evt.tool || "a tool";
      var input = evt.input || {};

      ctx.hideEmpty();

      var wrap = document.createElement("div");
      wrap.className = "chat chat-start memory-rise";
      wrap.innerHTML =
        '<div class="chat-image bg-warning/10 text-warning border-warning/20 flex items-center justify-center rounded-full border p-2">' +
        '<span class="iconify lucide--shield-alert size-5" aria-hidden="true"></span></div>' +
        '<div class="chat-header text-xs text-base-content/50">' + escapeHTML(ctx.currentAgentName()) + '</div>';

      var card = document.createElement("div");
      card.className =
        "memory-approval card card-border bg-base-100 col-start-2 row-start-2 w-full max-w-[85%] shadow-sm border-warning/30";
      card.innerHTML =
        '<div class="card-body gap-3 p-4">' +
        '<h3 class="text-base leading-snug font-semibold break-words">Approve tool call</h3>' +
        '<p class="mt-0.5 text-xs text-base-content/60">The agent wants to run <code class="font-mono text-xs">' + escapeHTML(tool) + '</code>.</p>' +
        '<pre class="approval-args max-h-40 overflow-y-auto rounded-lg bg-base-200/60 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words">' + escapeHTML(JSON.stringify(input, null, 2)) + '</pre>' +
        '<div class="approval-reject-box hidden flex-col gap-2">' +
        '<input type="text" class="approval-msg input input-sm w-full" placeholder="Reason (optional)" maxlength="500">' +
        '</div>' +
        '<div class="flex items-center justify-end gap-2 pt-1">' +
        '<button type="button" class="approval-cancel btn btn-ghost btn-sm">Cancel</button>' +
        '<button type="button" class="approval-reject btn btn-ghost btn-sm text-error">Reject</button>' +
        '<button type="button" class="approval-approve btn btn-primary btn-sm">Approve</button>' +
        '</div>' +
        '</div>';
      wrap.appendChild(card);
      ctx.messages.appendChild(wrap);

      var rejectBox = card.querySelector(".approval-reject-box");
      var msgInput = card.querySelector(".approval-msg");
      var rejectBtn = card.querySelector(".approval-reject");
      var approveBtn = card.querySelector(".approval-approve");
      var cancelBtn = card.querySelector(".approval-cancel");
      var resolving = false;

      function cleanup() {
        document.removeEventListener("keydown", onKey);
        if (wrap.parentNode) wrap.parentNode.removeChild(wrap);
      }

      function resolve(payload, note) {
        if (resolving) return;
        resolving = true;
        cleanup();
        ctx.hideEmpty();
        ctx.setStreaming(true);
        openAssistantBubble();
        ctx.postDecision(questionId, payload, note);
      }

      approveBtn.addEventListener("click", function () {
        resolve({ response: "approve" }, "Tool approved — the agent is continuing.");
      });
      rejectBtn.addEventListener("click", function () {
        if (rejectBox.classList.contains("hidden")) {
          rejectBox.classList.remove("hidden");
          rejectBox.classList.add("flex");
          msgInput.focus();
          rejectBtn.textContent = "Confirm reject";
          return;
        }
        resolve({ response: "reject", message: msgInput.value.trim() }, "Tool rejected — the agent is continuing.");
      });
      cancelBtn.addEventListener("click", function () {
        resolve(null, "Approval cancelled — the agent is continuing.");
      });

      function onKey(ev) {
        if (ev.key === "Escape") {
          ev.preventDefault();
          resolve(null, "Approval cancelled — the agent is continuing.");
        }
      }
      document.addEventListener("keydown", onKey);

      ctx.scrollToBottom();
    }

    /* ---------- streaming ---------- */

    async function streamChat(agent, text) {
      ctx.onStreamStart(); // chat: clearThinking; sidepanel: no-op
      ctx.aborter = new AbortController();
      var payload = { agentDefinitionId: agent, message: text };
      if (ctx.conversationId) payload.conversationId = ctx.conversationId;

      var res;
      try {
        res = await fetch("/api/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json", "Accept": "text/event-stream" },
          body: JSON.stringify(payload),
          signal: ctx.aborter.signal,
        });
      } catch (err) {
        if (err.name === "AbortError") { finishStream("aborted"); return; }
        failStream("Could not reach the gateway: " + err.message);
        return;
      }

      if (!res.ok || !res.body) {
        var msg = "Gateway error " + res.status;
        try {
          var j = await res.json();
          if (j && j.error) msg = j.error;
        } catch (e) {}
        failStream(msg);
        return;
      }

      var reader = res.body.getReader();
      var decoder = new TextDecoder();
      var buf = "";
      try {
        while (true) {
          var chunk = await reader.read();
          if (chunk.done) break;
          buf += decoder.decode(chunk.value, { stream: true });
          var idx;
          while ((idx = buf.indexOf("\n")) !== -1) {
            var line = buf.slice(0, idx).trim();
            buf = buf.slice(idx + 1);
            if (!line) continue;
            if (line.indexOf("data:") === 0) ctx.handleEvent(line.slice(5).trim());
          }
        }
      } catch (err) {
        if (err.name === "AbortError") { finishStream("aborted"); return; }
        failStream("Stream interrupted: " + err.message);
        return;
      }
      finishStream("done");
    }

    function finishStream(reason) {
      ctx.streaming = false;
      ctx.onStreamFinish(); // chat: finalizeThinking; sidepanel: recordHistory+persist
      updateBubbleText();
      ctx.setStreaming(false);
      ctx.scrollToBottom();
      if (reason === "aborted" && ctx.bubble && !ctx.bubbleHTML) {
        var el = ctx.bubble.querySelector(".memory-md");
        if (el && !el.textContent.trim()) ctx.bubble.remove();
      }
      ctx.aborter = null;
    }

    function failStream(message) {
      ctx.streaming = false;
      ctx.onStreamFail(); // chat: finalizeThinking; sidepanel: no-op
      updateBubbleText();
      ctx.setStreaming(false);
      if (ctx.bubble) {
        var el = ctx.bubble.querySelector(".memory-md");
        if (el && !el.textContent.trim()) {
          el.innerHTML = '<span class="text-error">' + escapeHTML(message) + '</span>';
        } else {
          notify("error", message);
        }
      } else {
        notify("error", message);
      }
      ctx.scrollToBottom();
      ctx.aborter = null;
    }

    return {
      addUserMessage: addUserMessage,
      addAssistantMessage: addAssistantMessage,
      openAssistantBubble: openAssistantBubble,
      updateBubbleText: updateBubbleText,
      toolChip: toolChip,
      setToolStatus: setToolStatus,
      handleToolEvent: handleToolEvent,
      renderQuestion: renderQuestion,
      renderApproval: renderApproval,
      renderHistoryQuestion: renderHistoryQuestion,
      streamChat: streamChat,
      finishStream: finishStream,
      failStream: failStream,
      autoGrow: autoGrow,
    };
  }

  window.MemoryChatStream = {
    escapeHTML: escapeHTML,
    notify: notify,
    relTime: relTime,
    isPauseNotice: isPauseNotice,
    isResumePrompt: isResumePrompt,
    isThinkingMessage: isThinkingMessage,
    normalizeStatus: normalizeStatus,
    hasFailureSignal: hasFailureSignal,
    firstError: firstError,
    summarizeOutput: summarizeOutput,
    summarizeText: summarizeText,
    tryParseJSON: tryParseJSON,
    classifyTool: classifyTool,
    createEngine: createEngine,
  };
})();
