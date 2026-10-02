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

   Drawer width + modal mode reuse the assistant side panel's pattern
   (#1389): MemoryChatHost.createResizeGrip drives the persisted width, and
   openModal() moves the SAME <aside> into #object-preview-modal-box so the
   loaded preview, listeners, and scroll position survive — nothing is forked.

   Public API: window.MemoryObjectPreview = { open, close }; */
(function () {
  "use strict";

  var root = null, panel = null, backdrop = null, body = null;
  var editLink = null, closeBtn = null;
  var lastFocused = null;
  var resizeHandle = null, widthToggle = null, modalOpenBtn = null;

  // Drawer width: single source of truth is the shared resize grip
  // (chat-host.js), applied as an inline max-width (the drawer keeps its
  // `w-full` behavior on small screens). Mirrors the assistant panel.
  var PANEL_WIDTH_DEFAULT = 512; // 32rem — matches the drawer's max-w-lg
  var PANEL_WIDTH_DOUBLE = 1024;
  var WIDTH_STORAGE_KEY = "memory.objectpreview.width.v1";

  // Modal window mode: the <aside> is moved into a centered dialog; closing
  // it (X / Escape / backdrop) returns the aside to the drawer.
  var modal = null, modalBox = null, modalMode = false;

  var panelGrip = MemoryChatHost.createResizeGrip({
    handle: null, // assigned in init() once #object-preview-resize is resolved
    el: panel,
    storageKey: WIDTH_STORAGE_KEY,
    defaultWidth: PANEL_WIDTH_DEFAULT,
    minWidth: 320,
    maxWidthFn: function () { return Math.min(window.innerWidth - 48, 1120); },
    direction: -1,
    apply: function (w) {
      if (!panel) return;
      // In modal mode the aside fills #object-preview-modal-box via the
      // .object-preview-in-modal stylesheet rule; an inline max-width would beat
      // that rule (inline styles win over non-!important CSS), so never set it
      // here while the panel is in the modal.
      if (modalMode) { panel.style.maxWidth = ""; return; }
      // Below md the drawer is full-width; the persisted width only caps it on
      // screens wide enough for a side drawer to make sense.
      if (window.innerWidth >= 768) panel.style.maxWidth = w + "px";
      else panel.style.maxWidth = "";
    },
    onWidthSet: onWidthSet,
  });

  function onWidthSet() {
    var w = panelGrip.getWidth();
    if (widthToggle) widthToggle.setAttribute("aria-pressed", w === PANEL_WIDTH_DOUBLE ? "true" : "false");
    if (resizeHandle) resizeHandle.setAttribute("aria-valuenow", String(w));
  }

  // Drag + clamp + persist live in the shared resize grip (chat-host.js); the
  // double-width header button just sets the preset. Persist explicitly
  // (setWidth applies but doesn't persist) so a reload keeps the toggled width.
  function toggleWidth() {
    var next = panelGrip.getWidth() === PANEL_WIDTH_DOUBLE ? PANEL_WIDTH_DEFAULT : PANEL_WIDTH_DOUBLE;
    panelGrip.setWidth(next);
    persistWidth();
  }

  function persistWidth() {
    try { localStorage.setItem(WIDTH_STORAGE_KEY, String(panelGrip.getWidth())); } catch (e) {}
  }

  // Keyboard resize for the separator (role="separator" + tabindex="0"). The
  // grip sits on the drawer's LEFT edge (direction -1), so ArrowLeft widens.
  function onGripKeydown(ev) {
    if (ev.key !== "ArrowLeft" && ev.key !== "ArrowRight") return;
    ev.preventDefault();
    var step = 24;
    panelGrip.setWidth(panelGrip.getWidth() + (ev.key === "ArrowLeft" ? step : -step));
    persistWidth();
  }

  function init() {
    root = document.getElementById("object-preview-root");
    if (!root || root.dataset.ready === "1") return;
    root.dataset.ready = "1";

    panel = document.getElementById("object-preview-panel");
    backdrop = document.getElementById("object-preview-backdrop");
    body = document.getElementById("object-preview-body");
    editLink = document.getElementById("object-preview-edit");
    closeBtn = document.getElementById("object-preview-close");
    resizeHandle = document.getElementById("object-preview-resize");
    widthToggle = document.getElementById("object-preview-width-toggle");
    modalOpenBtn = document.getElementById("object-preview-open-modal");
    modal = document.getElementById("object-preview-modal");
    modalBox = document.getElementById("object-preview-modal-box");
    if (!panel || !body) return;

    if (backdrop) backdrop.addEventListener("click", close);
    if (closeBtn) closeBtn.addEventListener("click", onCloseClick);
    if (widthToggle) widthToggle.addEventListener("click", toggleWidth);
    if (modalOpenBtn) modalOpenBtn.addEventListener("click", openModal);
    if (resizeHandle) {
      panelGrip.handle = resizeHandle;
      resizeHandle.addEventListener("pointerdown", panelGrip.onPointerDown);
      resizeHandle.addEventListener("keydown", onGripKeydown);
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
    if (modalMode) backToDrawer(); // never open a fresh preview inside the modal shell
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

  // The header X is shared by the drawer and the modal. In modal mode it
  // returns to the drawer (preview stays open), mirroring the assistant panel;
  // in drawer mode it closes the preview.
  function onCloseClick() {
    if (modalMode) { backToDrawer(); return; }
    close();
  }

  function close() {
    if (!panel || !isOpen()) return;
    // Programmatic close while promoted: return to the drawer first so the
    // aside is not hidden inside a still-open modal.
    if (modalMode) backToDrawer();
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
      // In modal mode Escape returns to the drawer (preview stays open), and
      // stopping propagation keeps the native dialog cancel from re-running
      // backToDrawer via the 'close' listener.
      ev.preventDefault();
      ev.stopImmediatePropagation();
      if (modalMode) { backToDrawer(); return; }
      close();
      return;
    }
    if (ev.key === "Tab") trapFocus(ev);
  }

  /* ---------- modal window mode ---------- */

  // "Open in modal" promotes the drawer to a centered dialog window. The SAME
  // <aside> (header, body) is moved into the dialog's modal-box, so the loaded
  // preview, listeners, and scroll position are all preserved — nothing is
  // forked. Closing the modal (X / Escape / backdrop) moves the aside back into
  // the drawer, which stays open.
  function openModal() {
    if (!modal || !modalBox || !panel || modalMode) return;
    modalMode = true;
    // Drop the drawer's inline width cap so .object-preview-in-modal's
    // width:100% actually fills the modal box (inline style would beat the
    // stylesheet rule and keep the drawer width, e.g. 512px).
    panel.style.maxWidth = "";
    panel.style.width = "";
    if (panel.parentNode) panel.parentNode.removeChild(panel);
    modalBox.appendChild(panel);
    panel.classList.add("object-preview-in-modal");
    if (backdrop) backdrop.classList.add("hidden"); // dialog has its own backdrop
    modal.showModal();
    focusClose();
  }

  function backToDrawer() {
    if (!modalMode) return;
    modalMode = false;
    if (modal && modal.open && typeof modal.close === "function") {
      modal.close(); // 'close' listener is guarded by modalMode
    }
    if (panel.parentNode) panel.parentNode.removeChild(panel);
    panel.classList.remove("object-preview-in-modal");
    if (root) root.appendChild(panel);
    panelGrip.onWindowResize(); // restore the drawer's persisted width cap
    if (isOpen() && backdrop) backdrop.classList.remove("hidden");
    focusClose();
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
