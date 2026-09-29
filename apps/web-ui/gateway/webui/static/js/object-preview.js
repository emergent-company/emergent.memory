/* Memory web UI — inline object-reference preview drawer.
   A persistent right-hand drawer hosted by the app shell (sibling of the
   assistant side panel, so it lives OUTSIDE #main-content and survives HTMX
   sidebar navigation). Clicking an inline object reference inside chat content
   opens a read-only summary here instead of navigating to the edit page; the
   anchor's href is left intact, so cmd/ctrl/middle-click and no-JS still
   navigate. The summary body is fetched with HTMX from GET /objects/:id/preview.

   Scoping: interception is limited to chat content — an assistant markdown
   block inside a chat message list (#chat-messages or #sidepanel-messages) or a
   sources-block citation link ([data-testid="citation-link"]). Object references
   rendered elsewhere (.memory-md on the object-detail knowledge answer, skill
   previews, share pages, or the object list/detail pages) navigate normally.

   Public API: window.MemoryObjectPreview = { open, close }; */
(function () {
  "use strict";

  var root = null, panel = null, backdrop = null, body = null;
  var editLink = null, closeBtn = null;
  var lastFocused = null;

  function init() {
    root = document.getElementById("object-preview-root");
    if (!root || root.dataset.ready === "1") return;
    root.dataset.ready = "1";

    panel = document.getElementById("object-preview-panel");
    backdrop = document.getElementById("object-preview-backdrop");
    body = document.getElementById("object-preview-body");
    editLink = document.getElementById("object-preview-edit");
    closeBtn = document.getElementById("object-preview-close");
    if (!panel || !body) return;

    if (backdrop) backdrop.addEventListener("click", close);
    if (closeBtn) closeBtn.addEventListener("click", close);

    // Capture phase: htmx boost attaches its own click handler to the anchor
    // element (bubble/target), so a bubble-phase listener here would run too
    // late and the boosted navigation would win. Capturing at the document
    // lets us preventDefault before htmx sees the event.
    document.addEventListener("click", onDocumentClick, true);
    document.addEventListener("keydown", onKeydown);
    // htmx v4 reports a non-2xx (and, per config, does not swap it) as
    // htmx:response:error; a network drop throws htmx:error. Handle both so the
    // drawer shows a graceful state instead of a stuck spinner.
    document.addEventListener("htmx:response:error", onResponseError);
    document.addEventListener("htmx:error", onResponseError);
  }

  function isOpen() {
    return !!panel && !panel.classList.contains("translate-x-full");
  }

  /* ---------- click interception (chat refs only) ---------- */

  function onDocumentClick(ev) {
    if (ev.defaultPrevented) return;
    // Plain left-click only — modifier/middle clicks keep native behaviour.
    if (ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;

    var a = ev.target && ev.target.closest ? ev.target.closest("a[href]") : null;
    if (!a || !isChatObjectRef(a)) return;

    var ref = parseRef(a.getAttribute("href"));
    if (!ref) return;

    ev.preventDefault();
    ev.stopPropagation(); // keep htmx boost / other link handlers out of it
    open(ref);
  }

  // isChatObjectRef is the whole scoping rule: an anchor to /objects/… that
  // lives in a chat message list — the inline assistant markdown rendered into
  // #chat-messages / #sidepanel-messages — or is a sources-block citation link.
  // Other .memory-md surfaces (the object-detail knowledge answer, skill
  // previews) and the object list/detail pages are deliberately excluded.
  function isChatObjectRef(a) {
    var href = a.getAttribute("href") || "";
    if (href.indexOf("/objects/") !== 0) return false;
    if (a.target && a.target !== "" && a.target !== "_self") return false;
    if (a.hasAttribute("download")) return false;
    if (a.getAttribute("data-testid") === "citation-link") return true;
    return !!a.closest("#chat-messages, #sidepanel-messages");
  }

  // parseRef splits a /objects/<id>[#relationship-<rel>] href. Returns null for
  // refs we should not intercept (empty, or an id containing a slash — a human
  // key the single-segment preview route cannot address, so navigation wins).
  function parseRef(href) {
    if (!href || href.indexOf("/objects/") !== 0) return null;
    var path = href, frag = "";
    var hash = path.indexOf("#");
    if (hash >= 0) { frag = path.slice(hash + 1); path = path.slice(0, hash); }
    var id = path.slice("/objects/".length);
    if (!id || id.indexOf("/") >= 0) return null;
    var rel = "";
    if (frag.indexOf("relationship-") === 0) rel = frag.slice("relationship-".length);
    return { id: id, rel: rel, editHref: path };
  }

  /* ---------- open / close ---------- */

  function open(ref) {
    if (!panel || !ref) return;
    lastFocused = document.activeElement;
    if (editLink) editLink.setAttribute("href", ref.editHref || "/objects/" + ref.id);

    setLoading();
    panel.classList.remove("translate-x-full");
    panel.removeAttribute("inert"); // re-enter the tab order
    panel.setAttribute("aria-hidden", "false");
    if (backdrop) backdrop.classList.remove("hidden");
    focusClose();

    var url = "/objects/" + ref.id + "/preview" + (ref.rel ? "?rel=" + encodeURIComponent(ref.rel) : "");
    if (window.htmx && htmx.ajax) {
      htmx.ajax("GET", url, { target: "#object-preview-body", swap: "innerHTML" });
    }
  }

  function close() {
    if (!panel || !isOpen()) return;
    panel.classList.add("translate-x-full");
    panel.setAttribute("aria-hidden", "true");
    panel.setAttribute("inert", ""); // drop the closed drawer's controls from the tab order
    if (backdrop) backdrop.classList.add("hidden");
    body.innerHTML = "";
    if (lastFocused && typeof lastFocused.focus === "function") {
      try { lastFocused.focus(); } catch (e) { /* element may be gone */ }
    }
    lastFocused = null;
  }

  function setLoading() {
    // Static markup only — no data is interpolated here.
    body.innerHTML =
      '<div class="flex h-full items-center justify-center p-6">' +
      '<span class="loading loading-spinner loading-md text-primary" aria-hidden="true"></span>' +
      '<span class="sr-only">Loading preview…</span></div>';
  }

  function focusClose() {
    if (closeBtn) closeBtn.focus();
  }

  function onKeydown(ev) {
    if (!isOpen()) return;
    if (ev.key === "Escape") {
      // Own Escape while the preview is open; stop it reaching the side panel.
      ev.preventDefault();
      ev.stopImmediatePropagation();
      close();
      return;
    }
    if (ev.key === "Tab") trapFocus(ev);
  }

  // Light focus trap: keep Tab cycling within the drawer while it is open.
  function trapFocus(ev) {
    var items = panel.querySelectorAll(
      'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])'
    );
    var visible = [];
    for (var i = 0; i < items.length; i++) {
      if (items[i].offsetParent !== null) visible.push(items[i]);
    }
    if (!visible.length) return;
    var first = visible[0], last = visible[visible.length - 1];
    if (ev.shiftKey && document.activeElement === first) {
      ev.preventDefault();
      last.focus();
    } else if (!ev.shiftKey && document.activeElement === last) {
      ev.preventDefault();
      first.focus();
    }
  }

  function onResponseError(ev) {
    var target = ev && ev.detail && ev.detail.ctx && ev.detail.ctx.target;
    if (target !== body) return;
    body.innerHTML = "";
    body.appendChild(notFoundNode());
  }

  function notFoundNode() {
    var el = document.createElement("div");
    el.className = "flex h-full flex-col items-center justify-center gap-2 p-6 text-center";
    var icon = document.createElement("span");
    icon.className = "iconify lucide--circle-alert size-8 text-warning";
    icon.setAttribute("aria-hidden", "true");
    var title = document.createElement("p");
    title.className = "text-sm font-medium";
    title.textContent = "Object unavailable";
    var desc = document.createElement("p");
    desc.className = "text-muted text-xs";
    desc.textContent = "This reference could not be loaded. It may have been removed or renamed.";
    el.appendChild(icon);
    el.appendChild(title);
    el.appendChild(desc);
    return el;
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.MemoryObjectPreview = { open: open, close: close };
})();
