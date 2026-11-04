/*
 * documents-chunks.js — inline chunk editor + revision history for the
 * document detail page (improve-document-chunking-and-curation, #1473).
 *
 * The chunk rows are server-rendered by documents_chunks.templ; this file
 * hydrates them by calling the shipped chunk-curation JSON routes:
 *   PATCH  /api/chunks/:id                    — save edited text
 *   GET    /api/chunks/:id/revisions          — revision history
 *   GET    /api/chunks/:id/revisions/diff     — line diff between revisions
 *   POST   /api/chunks/:id/rollback           — restore a prior revision
 *
 * All handlers are delegated from document, so they survive htmx-boosted
 * navigations and partial swaps. Chunk text is document content and is treated
 * as untrusted: every render uses textContent / createElement, never innerHTML.
 *
 * Accessibility: the per-chunk [data-chunk-status] is an aria-live region;
 * opening an editor focuses its textarea; a successful save focuses the Edit
 * trigger; opening history focuses its heading; a successful rollback focuses
 * the chunk text; and rollback is gated behind a native confirm dialog that
 * returns focus to the trigger on cancel. A failed save keeps the draft in the
 * textarea and surfaces the error inline (no data loss).
 */
(function () {
  "use strict";

  if (window.__documentsChunksInit) return;
  window.__documentsChunksInit = true;

  // revisionsCache holds the last fetched revisions per chunk id, so a
  // rollback can restore the selected revision's text without a second fetch.
  var revisionsCache = {};

  // pendingRollback is the rollback awaiting confirmation in the shared dialog.
  var pendingRollback = null;

  function rowOf(el) {
    return el && el.closest ? el.closest("[data-chunk-row]") : null;
  }

  function chunkId(row) {
    return row ? row.getAttribute("data-chunk-id") || "" : "";
  }

  function query(row, selector) {
    return row ? row.querySelector(selector) : null;
  }

  function setStatus(row, message, isError) {
    var el = query(row, "[data-chunk-status]");
    if (!el) return;
    el.textContent = message || "";
    if (isError) {
      el.classList.remove("text-success");
      el.classList.add("text-error");
      el.setAttribute("role", "alert");
    } else {
      el.classList.remove("text-error");
      if (message) el.classList.add("text-success");
      else el.classList.remove("text-success");
      el.setAttribute("role", "status");
    }
  }

  function setExpanded(trigger, expanded) {
    if (trigger) trigger.setAttribute("aria-expanded", expanded ? "true" : "false");
  }

  function markEdited(row, edited) {
    var badge = query(row, "[data-chunk-edited-badge]");
    if (badge) badge.hidden = !edited;
  }

  // updateSize keeps the server-rendered "<n> chars" metadata in step with an
  // edited/rolled-back text. The API reports len() in UTF-8 bytes, so count the
  // same way rather than by JS UTF-16 code units.
  function updateSize(row, text) {
    var el = query(row, "[data-chunk-size]");
    if (!el) return;
    var bytes = typeof TextEncoder === "function" ? new TextEncoder().encode(text).length : text.length;
    el.textContent = bytes + " chars";
  }

  // resetDiff hides and clears the diff region. A rendered diff is relative to
  // the revision at render time, so it is stale after any successful mutation.
  function resetDiff(row) {
    var region = query(row, "[data-chunk-diff-region]");
    var pre = query(row, "[data-chunk-diff]");
    var heading = query(row, "[data-chunk-diff-heading]");
    if (region) region.hidden = true;
    if (pre) pre.textContent = "";
    if (heading) heading.textContent = "Differences";
  }

  function formatTime(value) {
    if (!value) return "";
    var d = new Date(value);
    if (isNaN(d.getTime())) return value;
    return d.toLocaleString();
  }

  // jsonFetch sends a request and resolves the parsed JSON body, rejecting with
  // the server-provided message on any non-2xx response.
  function jsonFetch(url, options) {
    return fetch(url, options).then(function (res) {
      return res.text().then(function (body) {
        var data = null;
        try {
          data = body ? JSON.parse(body) : null;
        } catch (e) {
          data = null;
        }
        if (!res.ok) {
          var message = (data && (data.error || data.message)) || "Request failed (" + res.status + ")";
          if (typeof message !== "string") message = "Request failed (" + res.status + ")";
          throw new Error(message);
        }
        return data;
      });
    });
  }

  function openEditor(row, trigger) {
    var text = query(row, "[data-chunk-text]");
    var editor = query(row, "[data-chunk-editor]");
    if (!editor) return;
    var textarea = query(editor, "textarea");
    // A fresh open starts from the saved text; a re-click while the editor is
    // already open keeps the in-progress draft.
    if (editor.hidden && textarea && text) textarea.value = text.textContent;
    if (text) text.hidden = true;
    editor.hidden = false;
    setExpanded(trigger || query(row, '[data-chunk-action="edit"]'), true);
    if (textarea) {
      textarea.focus();
      var end = textarea.value.length;
      if (typeof textarea.setSelectionRange === "function") textarea.setSelectionRange(end, end);
    }
  }

  function closeEditor(row) {
    var text = query(row, "[data-chunk-text]");
    var editor = query(row, "[data-chunk-editor]");
    if (editor) editor.hidden = true;
    if (text) text.hidden = false;
    setExpanded(query(row, '[data-chunk-action="edit"]'), false);
  }

  function saveEdit(row, editor) {
    if (!row || !editor) return;
    var textarea = query(editor, "textarea");
    var text = textarea ? textarea.value : "";
    if (!text.trim()) {
      setStatus(row, "Chunk text cannot be empty.", true);
      if (textarea) textarea.focus();
      return;
    }
    var saveButton = query(editor, '[data-chunk-action="save"]');
    if (saveButton) saveButton.disabled = true;
    row.setAttribute("aria-busy", "true");
    setStatus(row, "Saving…", false);

    jsonFetch("/api/chunks/" + encodeURIComponent(chunkId(row)), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: text }),
    }).then(
      function (res) {
        var revision = res && typeof res.revision === "number" ? res.revision : null;
        var textEl = query(row, "[data-chunk-text]");
        if (textEl) textEl.textContent = text;
        updateSize(row, text);
        if (revision !== null) row.setAttribute("data-chunk-revision", String(revision));
        markEdited(row, true);
        // The revision list and any rendered diff describe the pre-save state;
        // refresh the open history and drop the now-stale diff.
        var history = query(row, "[data-chunk-history]");
        if (history && !history.hidden) loadRevisions(row);
        resetDiff(row);
        closeEditor(row);
        setStatus(row, revision !== null ? "Saved as revision " + revision + "." : "Saved.", false);
        var editTrigger = query(row, '[data-chunk-action="edit"]');
        if (editTrigger) editTrigger.focus();
      },
      function (err) {
        // Keep the draft in the textarea so nothing is lost; the user can
        // retry or cancel.
        setStatus(row, "Could not save: " + err.message, true);
        if (textarea) textarea.focus();
      },
    ).then(function () {
      row.removeAttribute("aria-busy");
      if (saveButton) saveButton.disabled = false;
    });
  }

  function actionButton(label, action, seq, testid) {
    var button = document.createElement("button");
    button.type = "button";
    button.setAttribute("data-chunk-action", action);
    button.setAttribute("data-revision-seq", String(seq));
    button.setAttribute("data-testid", testid);
    button.className = action === "rollback" ? "btn btn-error btn-ghost btn-xs" : "btn btn-ghost btn-xs";
    button.textContent = label;
    return button;
  }

  function renderRevisions(row, revisions) {
    var list = query(row, "[data-chunk-revisions]");
    if (!list) return;
    list.textContent = "";
    var index = row.getAttribute("data-chunk-index") || "0";
    var current = parseInt(row.getAttribute("data-chunk-revision") || "0", 10);
    (revisions || []).forEach(function (revision) {
      var item = document.createElement("li");
      item.setAttribute("data-chunk-revision-item", "");
      item.setAttribute("data-revision-seq", String(revision.seq));
      item.className = "flex flex-wrap items-center gap-2 border-t border-base-content/10 py-2";

      var seqBadge = document.createElement("span");
      seqBadge.className = "badge badge-neutral badge-xs";
      seqBadge.textContent = "r" + revision.seq;
      item.appendChild(seqBadge);

      if (revision.seq === current) {
        var currentBadge = document.createElement("span");
        currentBadge.className = "badge badge-success badge-xs";
        currentBadge.textContent = "current";
        item.appendChild(currentBadge);
      }

      var meta = document.createElement("span");
      meta.className = "text-muted text-xs";
      var bits = [];
      if (revision.author) bits.push(revision.author);
      if (revision.createdAt) bits.push(formatTime(revision.createdAt));
      meta.textContent = bits.join(" · ");
      item.appendChild(meta);

      if (revision.seq !== current) {
        var actions = document.createElement("span");
        actions.className = "ml-auto flex items-center gap-1";
        actions.appendChild(actionButton("View diff", "diff", revision.seq, "chunk-diff-" + index + "-" + revision.seq));
        actions.appendChild(actionButton("Roll back", "rollback", revision.seq, "chunk-rollback-" + index + "-" + revision.seq));
        item.appendChild(actions);
      }
      list.appendChild(item);
    });
  }

  function loadRevisions(row) {
    var status = query(row, "[data-chunk-revisions-status]");
    if (status) status.textContent = "Loading revisions…";
    jsonFetch("/api/chunks/" + encodeURIComponent(chunkId(row)) + "/revisions").then(
      function (res) {
        var revisions = (res && res.data) || [];
        revisionsCache[chunkId(row)] = revisions;
        renderRevisions(row, revisions);
        if (status) status.textContent = revisions.length ? "" : "No revisions yet.";
      },
      function (err) {
        if (status) status.textContent = "Could not load revisions: " + err.message;
      },
    );
  }

  function toggleHistory(row, trigger) {
    var panel = query(row, "[data-chunk-history]");
    if (!panel) return;
    if (panel.hidden) {
      panel.hidden = false;
      setExpanded(trigger, true);
      loadRevisions(row);
      var heading = query(panel, "[data-chunk-history-heading]");
      if (heading) heading.focus();
    } else {
      // Closing via the in-panel Close button: the History trigger is the
      // element that owns aria-expanded and must regain focus — the Close
      // button is inside the panel that is being hidden and cannot hold focus.
      panel.hidden = true;
      var openTrigger = query(row, '[data-chunk-action="history"]');
      setExpanded(openTrigger, false);
      if (openTrigger) openTrigger.focus();
    }
  }

  function showDiff(row, fromSeq) {
    var region = query(row, "[data-chunk-diff-region]");
    var pre = query(row, "[data-chunk-diff]");
    var to = parseInt(row.getAttribute("data-chunk-revision") || "0", 10);
    if (region) region.hidden = false;
    if (pre) pre.textContent = "Loading diff…";
    jsonFetch(
      "/api/chunks/" + encodeURIComponent(chunkId(row)) +
        "/revisions/diff?from=" + encodeURIComponent(fromSeq) + "&to=" + encodeURIComponent(to),
    ).then(
      function (res) {
        if (!pre) return;
        pre.textContent = "";
        var lines = (res && res.lines) || [];
        lines.forEach(function (line) {
          var span = document.createElement("span");
          span.setAttribute("data-diff-type", line.type || "equal");
          var prefix = line.type === "add" ? "+ " : line.type === "remove" ? "- " : "  ";
          span.textContent = prefix + line.text + "\n";
          if (line.type === "add") span.className = "block bg-success/10 text-success";
          else if (line.type === "remove") span.className = "block bg-error/10 text-error";
          else span.className = "block";
          pre.appendChild(span);
        });
        var heading = query(row, "[data-chunk-diff-heading]");
        if (heading && res && typeof res.from === "number" && typeof res.to === "number") {
          heading.textContent = "Differences (r" + res.from + " → r" + res.to + ")";
        }
      },
      function (err) {
        if (pre) pre.textContent = "Could not load diff: " + err.message;
      },
    );
  }

  function requestRollback(row, seq, trigger) {
    pendingRollback = { row: row, seq: seq, trigger: trigger };
    var dialog = document.getElementById("chunk-rollback-confirm");
    var message = dialog ? dialog.querySelector("[data-chunk-rollback-message]") : null;
    if (message) {
      message.textContent =
        "Roll back this chunk to revision " + seq +
        "? This adds a new revision; the current text stays in history.";
    }
    if (dialog && typeof dialog.showModal === "function") dialog.showModal();
  }

  function cancelRollback() {
    var trigger = pendingRollback ? pendingRollback.trigger : null;
    pendingRollback = null;
    var dialog = document.getElementById("chunk-rollback-confirm");
    if (dialog && dialog.open && typeof dialog.close === "function") dialog.close();
    if (trigger) trigger.focus();
  }

  function confirmRollback() {
    if (!pendingRollback) return;
    var row = pendingRollback.row;
    var seq = pendingRollback.seq;
    var trigger = pendingRollback.trigger;
    pendingRollback = null;

    var dialog = document.getElementById("chunk-rollback-confirm");
    if (dialog && dialog.open && typeof dialog.close === "function") dialog.close();

    setStatus(row, "Rolling back…", false);
    jsonFetch("/api/chunks/" + encodeURIComponent(chunkId(row)) + "/rollback", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ seq: seq }),
    }).then(
      function (res) {
        var revision = res && typeof res.revision === "number" ? res.revision : seq;
        var textEl = query(row, "[data-chunk-text]");
        var cached = (revisionsCache[chunkId(row)] || []).filter(function (r) {
          return r.seq === seq;
        })[0];
        if (cached && textEl) textEl.textContent = cached.text;
        if (cached) updateSize(row, cached.text);
        row.setAttribute("data-chunk-revision", String(revision));
        markEdited(row, true);
        setStatus(row, "Rolled back to revision " + seq + ".", false);
        var history = query(row, "[data-chunk-history]");
        if (history && !history.hidden) loadRevisions(row);
        resetDiff(row);
        if (textEl) textEl.focus();
      },
      function (err) {
        setStatus(row, "Could not roll back: " + err.message, true);
        if (trigger) trigger.focus();
      },
    );
  }

  document.addEventListener("click", function (ev) {
    var target = ev.target;
    if (!target || !target.closest) return;
    var control = target.closest("[data-chunk-action]");
    if (!control) return;
    var row = rowOf(control);
    if (!row) return;
    var action = control.getAttribute("data-chunk-action");
    if (action === "edit") {
      ev.preventDefault();
      openEditor(row, control);
    } else if (action === "cancel-edit") {
      ev.preventDefault();
      closeEditor(row);
      var editTrigger = query(row, '[data-chunk-action="edit"]');
      if (editTrigger) editTrigger.focus();
    } else if (action === "history" || action === "close-history") {
      ev.preventDefault();
      toggleHistory(row, control);
    } else if (action === "diff") {
      ev.preventDefault();
      showDiff(row, parseInt(control.getAttribute("data-revision-seq"), 10));
    } else if (action === "rollback") {
      ev.preventDefault();
      requestRollback(row, parseInt(control.getAttribute("data-revision-seq"), 10), control);
    }
  });

  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form || !form.matches || !form.matches("[data-chunk-editor]")) return;
    ev.preventDefault();
    saveEdit(rowOf(form), form);
  }, true);

  document.addEventListener("click", function (ev) {
    var target = ev.target;
    if (!target || !target.closest) return;
    if (target.closest("[data-chunk-rollback-approve]")) {
      ev.preventDefault();
      confirmRollback();
    } else if (target.closest("[data-chunk-rollback-dismiss]")) {
      ev.preventDefault();
      cancelRollback();
    }
  });

  // Expose the pieces for the hermetic js-dom wiring spec and for any inline
  // page script that needs to drive the editor programmatically.
  window.MemoryChunkEditor = {
    openEditor: openEditor,
    closeEditor: closeEditor,
    saveEdit: saveEdit,
    loadRevisions: loadRevisions,
    renderRevisions: renderRevisions,
    showDiff: showDiff,
    requestRollback: requestRollback,
    confirmRollback: confirmRollback,
    cancelRollback: cancelRollback,
  };
})();
