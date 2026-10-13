// Board client (board subsystem — SSE-driven auto-refresh).
//
// Keeps the Kanban columns live without a manual reload: the agent moves work
// items asynchronously (e.g. revision -> in_progress) and the gateway's project
// event stream announces those entity changes. One EventSource subscribes to the
// project stream and, on entity.created/updated/deleted frames for graph_object
// or agent_run entities, re-renders #board through htmx (server-rendered
// partial), so no server/agent-produced content is ever inserted via innerHTML.
//
// Refreshes are debounced (a burst of events issues one GET), suppressed while
// the tab is hidden (deferred until it is visible again) or while a board drag
// is in flight (the drop's own POST re-renders the board), and are a no-op when
// #board is absent — so the script is safe on every page.
(function () {
  "use strict";

  var REFRESH_DEBOUNCE_MS = 300;
  var ENTITY_EVENTS = ["entity.created", "entity.updated", "entity.deleted"];

  var stream = null;
  var debounceTimer = null;
  var pendingRefresh = false;

  // activeProjectId reads the shell's active project (set on <body>), the same
  // source inbox.js uses. The board is always a project page, so this is the
  // project whose stream (and partial) we subscribe to.
  function activeProjectId() {
    if (!document.body) return "";
    return document.body.getAttribute("data-active-project-id") || "";
  }

  // isDragging reflects the native drag-in-flight marker app.js sets on <body>
  // during dragstart and clears on drop/dragend. Re-rendering #board mid-drag
  // would destroy the drag source element and break the native drag.
  function isDragging() {
    return document.body && document.body.getAttribute("data-board-dragging") === "true";
  }

  function refresh() {
    var board = document.getElementById("board");
    if (!board) return;
    if (document.hidden || isDragging()) {
      // Hidden: defer (see visibilitychange below). Dragging: the drop's own
      // POST re-renders #board with fresh data, so drop this refresh.
      if (document.hidden) pendingRefresh = true;
      return;
    }
    pendingRefresh = false;
    var url = window.MemoryScope ? MemoryScope.path("/board/partial") : "/board/partial";
    if (window.htmx && window.htmx.ajax) {
      window.htmx.ajax("GET", url, { target: "#board", swap: "outerHTML" });
    }
  }

  // scheduleRefresh coalesces a burst of events into a single refresh.
  function scheduleRefresh() {
    if (debounceTimer) return;
    debounceTimer = window.setTimeout(function () {
      debounceTimer = null;
      refresh();
    }, REFRESH_DEBOUNCE_MS);
  }

  function onEntityEvent(ev) {
    var data;
    try {
      data = JSON.parse(ev.data);
    } catch (e) {
      return; // not JSON (e.g. heartbeat): nothing to refresh for
    }
    if (!data || (data.entity !== "graph_object" && data.entity !== "agent_run")) {
      return;
    }
    scheduleRefresh();
  }

  function closeStream() {
    if (stream) stream.close();
    stream = null;
  }

  // openStream subscribes to the project event stream. The session cookie
  // authenticates the gateway SSE proxy, which attaches the bearer upstream.
  function openStream() {
    var pid = activeProjectId();
    if (!pid) return;
    stream = new EventSource(
      "/api/notifications/stream?scope=project&project_id=" + encodeURIComponent(pid)
    );
    for (var i = 0; i < ENTITY_EVENTS.length; i++) {
      stream.addEventListener(ENTITY_EVENTS[i], onEntityEvent);
    }
    stream.onerror = function () {
      // EventSource auto-reconnects; the next event re-renders the board.
    };
  }

  function init() {
    if (!document.getElementById("board")) return;
    openStream();
    window.addEventListener("pagehide", closeStream);
    window.addEventListener("beforeunload", closeStream);
    document.addEventListener("visibilitychange", function () {
      if (!document.hidden && pendingRefresh) refresh();
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
