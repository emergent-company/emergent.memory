// Inbox client (inbox subsystem, web-inbox capability).
//
// Keeps the topbar bell count and the inbox list live without a manual refresh:
//   - one EventSource per relevant scope ("account" always, plus the active
//     project while the Everything or Project scope is on screen) — the session
//     cookie authenticates the gateway SSE proxy, which attaches the bearer
//     upstream. Memory emits entity events (`event: entity.created`) whose
//     payload names the entity, so the client filters frames on
//     `entity === "notification"` rather than listening for a notification-named
//     event the server never sends;
//   - counts are reconciled from /api/notifications/counts on load, on every
//     htmx settle (scope/tab switch), and on every received event;
//   - the inbox list refreshes in place through htmx (server-rendered partial),
//     so notification text is never re-inserted via innerHTML.
//
// Inline actions are delegated clicks that call the gateway JSON API and then
// reconcile counts + refresh the list. Everything degrades to a no-op when its
// anchor element is absent, so the script is safe on every page.
(function () {
  "use strict";

  var BEL_SEL = "[data-notifications-bell]";
  var BADGE_SEL = "[data-notifications-badge]";
  var PAGE_SEL = "[data-inbox-page]";

  var accountStream = null;
  var projectStream = null;
  var activeProjectKey = "";
  var accountStreamOpen = false;

  function qs(sel, root) {
    return (root || document).querySelector(sel);
  }

  function qsa(sel, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(sel));
  }

  function badgeText(n) {
    return n > 9 ? "9+" : String(n);
  }

  // setBell paints the unread count on every bell in the document, creating or
  // removing the badge marker as the count crosses zero.
  function setBell(n) {
    qsa(BEL_SEL).forEach(function (bell) {
      var holder = qs(".indicator", bell) || bell;
      var badge = qs(BADGE_SEL, bell);
      if (n <= 0) {
        if (badge) badge.remove();
        bell.removeAttribute("data-unread");
        return;
      }
      if (!badge) {
        badge = document.createElement("span");
        badge.setAttribute("data-notifications-badge", "");
        badge.className =
          "indicator-item indicator-top indicator-end badge badge-error badge-sm tabular-nums";
        holder.appendChild(badge);
      }
      badge.textContent = badgeText(n);
      bell.setAttribute("data-unread", String(n));
    });
  }

  // pageScope is the inbox scope on screen, defaulting to "all" (Everything) —
  // the scope every non-inbox page's bell counts, and the inbox's default view.
  function pageScope() {
    var page = qs(PAGE_SEL);
    return page ? page.getAttribute("data-scope") || "all" : "all";
  }

  function pageProjectId() {
    var page = qs(PAGE_SEL);
    return page ? page.getAttribute("data-project-id") || "" : "";
  }

  // activeProjectId reads the shell's active project (set on <body>), so the
  // bell reconciles account + project unread on every page, not only the inbox.
  function activeProjectId() {
    if (!document.body) return "";
    return document.body.getAttribute("data-active-project-id") || "";
  }

  function countsURL(scope, projectId) {
    var url = "/api/notifications/counts?scope=" + encodeURIComponent(scope);
    if (projectId) url += "&project_id=" + encodeURIComponent(projectId);
    return url;
  }

  function fetchUnread(scope, projectId) {
    return fetch(countsURL(scope, projectId), {
      headers: { Accept: "application/json" },
      credentials: "same-origin",
    })
      .then(function (r) {
        return r.ok ? r.json() : null;
      })
      .then(function (c) {
        return c && typeof c.unread === "number" ? c.unread : 0;
      })
      .catch(function () {
        return 0;
      });
  }

  // reconcile re-reads the unread count for the inbox scope currently on
  // screen and paints it on the bell. It runs on load, scope switch, reconnect,
  // and every event. The count comes from the SAME scope-filtered counts call
  // the inbox list uses, so the badge can never disagree with the list it links
  // to: Everything everywhere except on the inbox, where it follows the selected
  // scope. (Regression: #1342 — summing account + project unread showed 11 over
  // an account inbox of 2. Regression: document-lifecycle — the default scope
  // was account, so a teammate's upload never ticked the bell off a project
  // page.)
  function reconcile() {
    var scope = pageScope();
    var pid = scope === "project" ? pageProjectId() || activeProjectId() : "";
    fetchUnread(scope, pid).then(setBell);
  }

  // refreshList re-renders the inbox partial through htmx (server-rendered).
  function refreshList() {
    var page = qs(PAGE_SEL);
    if (!page) return;
    var scope = page.getAttribute("data-scope") || "all";
    var tab = page.getAttribute("data-tab") || "all";
    var project = page.getAttribute("data-project-filter") || "";
    var path = window.MemoryScope ? MemoryScope.path("/inbox") : "/inbox";
    var url =
      path + "?scope=" + encodeURIComponent(scope) + "&tab=" + encodeURIComponent(tab);
    if (project) url += "&project=" + encodeURIComponent(project);
    if (window.htmx && window.htmx.ajax) {
      window.htmx.ajax("GET", url, { target: "#main-content", swap: "innerHTML" });
    } else {
      window.location.reload();
    }
  }

  // onStreamFrame handles one SSE frame from either stream. Memory emits
  // `event: entity.created` (and entity.updated) with the entity in the payload,
  // and both streams also carry documents, graph objects and agent runs, so the
  // entity must be checked before touching the inbox.
  function onStreamFrame(ev) {
    if (!ev || !ev.data) return;
    var payload;
    try {
      payload = JSON.parse(ev.data);
    } catch (e) {
      return; // `connected`/`heartbeat` frames are not notification entities
    }
    if (!payload || payload.entity !== "notification") return;
    onNotification();
  }

  function onNotification() {
    reconcile();
    refreshList();
  }

  function closeStream(stream) {
    if (stream) stream.close();
  }

  // subscribe wires both entity event names the server emits onto one handler.
  function subscribe(stream) {
    stream.addEventListener("entity.created", onStreamFrame);
    stream.addEventListener("entity.updated", onStreamFrame);
    return stream;
  }

  // openStreams subscribes to the streams relevant to the current page. The
  // account stream always drives the bell; the active project's stream is added
  // while the Everything or Project scope is on screen, because memory's account
  // stream drops project-scope notifications (scope != "account"). Streams are
  // keyed so repeated htmx settles do not stack connections.
  //
  // Known limitation: in the Everything scope only the ACTIVE project streams
  // live, so a row arriving from another project appears on the next reconcile
  // (htmx settle or scope switch) rather than instantly.
  function openStreams() {
    if (!accountStreamOpen) {
      accountStreamOpen = true;
      accountStream = subscribe(new EventSource("/api/notifications/stream?scope=account"));
      accountStream.onerror = function () {
        // EventSource auto-reconnects; reconcile once it is back.
      };
      accountStream.onopen = reconcile;
    }
    var scope = pageScope();
    var projectKey =
      scope === "project" || scope === "all" ? activeProjectId() || pageProjectId() : "";
    if (projectKey !== activeProjectKey) {
      closeStream(projectStream);
      projectStream = null;
      activeProjectKey = projectKey;
      if (projectKey) {
        projectStream = subscribe(
          new EventSource(
            "/api/notifications/stream?scope=project&project_id=" + encodeURIComponent(projectKey)
          )
        );
        projectStream.onerror = function () {};
      }
    }
  }

  // sendAction performs one notification mutation, then reconciles + refreshes.
  function sendAction(el, method, url, body) {
    el.setAttribute("aria-busy", "true");
    fetch(url, {
      method: method,
      headers: body ? { "Content-Type": "application/json" } : {},
      credentials: "same-origin",
      body: body ? JSON.stringify(body) : undefined,
    })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        reconcile();
        refreshList();
      })
      .catch(function () {
        // Leave the row as-is; the next event/reconcile re-syncs state.
      })
      .finally(function () {
        el.removeAttribute("aria-busy");
      });
  }

  function markAllQuery() {
    var scope = pageScope();
    var q = "?scope=" + encodeURIComponent(scope);
    var pid = pageProjectId();
    if (scope === "project" && pid) q += "&project_id=" + encodeURIComponent(pid);
    return q;
  }

  // openNotification navigates a row to its deep link (the pending
  // question/approval) and marks it read on the way out. The read PATCH is
  // sent with keepalive so it survives the navigation; navigation is not
  // blocked on it. Already-read rows navigate straight through.
  function openNotification(el) {
    var href = el.getAttribute("href");
    if (!href) return;
    var id = el.getAttribute("data-notif-id");
    var row = el.closest("[data-notification-id]");
    var unread = row && row.getAttribute("data-notification-unread") === "true";
    if (id && unread) {
      fetch("/api/notifications/" + encodeURIComponent(id) + "/read", {
        method: "PATCH",
        credentials: "same-origin",
        keepalive: true,
      }).catch(function () {});
    }
    window.location.assign(href);
  }

  function onClick(ev) {
    var openEl = ev.target.closest("[data-notification-open]");
    if (openEl) {
      ev.preventDefault();
      openNotification(openEl);
      return;
    }
    var el = ev.target.closest("[data-notif-action], [data-inbox-action]");
    if (!el) return;
    ev.preventDefault();
    if (el.hasAttribute("data-inbox-action")) {
      sendAction(el, "POST", "/api/notifications/mark-all-read" + markAllQuery(), null);
      return;
    }
    var id = el.getAttribute("data-notif-id");
    if (!id) return;
    var base = "/api/notifications/" + encodeURIComponent(id);
    switch (el.getAttribute("data-notif-action")) {
      case "read":
        sendAction(el, "PATCH", base + "/read", null);
        break;
      case "unread":
        sendAction(el, "POST", base + "/unread", null);
        break;
      case "dismiss":
        sendAction(el, "DELETE", base + "/dismiss", null);
        break;
      case "snooze":
        sendAction(el, "POST", base + "/snooze", {
          until: el.getAttribute("data-snooze-until") || "",
        });
        break;
      case "unsnooze":
        sendAction(el, "POST", base + "/unsnooze", null);
        break;
      case "clear":
        sendAction(el, "POST", base + "/clear", null);
        break;
      case "restore":
        sendAction(el, "POST", base + "/restore", null);
        break;
      case "resolve":
        sendAction(el, "POST", base + "/resolve", {
          action: el.getAttribute("data-notif-verb") || "",
        });
        break;
    }
  }

  function init() {
    if (document.body) {
      document.body.addEventListener("click", onClick);
    }
    document.addEventListener("htmx:afterSettle", function () {
      openStreams();
      reconcile();
    });
    openStreams();
    reconcile();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
