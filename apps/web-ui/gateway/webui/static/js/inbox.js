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
//   - the inbox list refreshes through the server-rendered
//     /partial/inbox-list fragment (the list plus out-of-band refreshes of the
//     tab strip and, on the Project scope, the scope switch), so notification
//     text is never re-inserted via innerHTML and the page around the list —
//     scroll, focus, open rows — survives the swap.
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

  // refreshList re-renders the inbox list fragment through htmx. The server
  // returns #inbox-list plus out-of-band refreshes of #inbox-tabs and, on the
  // Project scope, #inbox-scope-switch, so counts stay live while the page
  // around the list keeps its scroll, focus and open rows. Swapping
  // #main-content here (the pre-fragment behaviour) re-rendered the whole page
  // on every event and every inline action.
  function refreshList() {
    var page = qs(PAGE_SEL);
    if (!page) return;
    var scope = page.getAttribute("data-scope") || "all";
    var tab = page.getAttribute("data-tab") || "all";
    /* The project filter changes without a page load: its form swaps the list in
       place and pushes the URL. Read it from the URL when it is there, so a live
       refresh after a filter change keeps the filter instead of reverting to the
       value the page was first rendered with. */
    var project = new URLSearchParams(window.location.search).get("project");
    if (project === null) project = page.getAttribute("data-project-filter") || "";
    var base = "/partial/inbox-list";
    var path = window.MemoryScope ? MemoryScope.path(base) : base;
    var url =
      path + "?scope=" + encodeURIComponent(scope) + "&tab=" + encodeURIComponent(tab);
    if (project) url += "&project=" + encodeURIComponent(project);
    if (window.htmx && window.htmx.ajax) {
      // outerHTML: the response's root IS #inbox-list, so innerHTML would nest a
      // second #inbox-list inside the target (duplicate id) on the first swap.
      var pending;
      try {
        pending = window.htmx.ajax("GET", url, {
          target: "#inbox-list",
          swap: "outerHTML",
        });
      } catch (e) {
        return; // htmx could not issue the request; the next event retries.
      }
      // htmx.ajax returns a promise. A failed refresh (502, timeout, dropped
      // connection) must not become an unhandled rejection: the list keeps the
      // rows it already has, the next notification or inline action retries,
      // and the bell reconcile above is independent. Swallow it deliberately.
      if (pending && typeof pending.catch === "function") {
        pending.catch(function () {});
      }
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

  // setRowBusy marks every action button in a row busy and disabled (or clears
  // it). aria-busy alone does not disable a button, so a second click could
  // still submit — the resolve path would then race the recorded verb. An
  // element outside a row (mark-all-read) is handled on its own.
  function setRowBusy(el, busy) {
    var row = el.closest("[data-notification-id]");
    var targets = row ? qsa("[data-notif-action]", row) : [el];
    targets.forEach(function (btn) {
      if (busy) {
        btn.setAttribute("aria-busy", "true");
        btn.setAttribute("disabled", "disabled");
      } else {
        btn.removeAttribute("aria-busy");
        btn.removeAttribute("disabled");
      }
    });
  }

  // sendAction performs one notification mutation, then reconciles + refreshes.
  // after optionally runs on success before the refresh, so a mutation that
  // implies further state (a resolve also marks the row read and archives it)
  // lands in the same refresh instead of a second round trip.
  //
  // A row is single-flight: while one request is in flight every action button
  // in it is disabled, so a double click (or Accept followed by Decline) cannot
  // fire a second competing submit. The server also refuses a verb that differs
  // from the recorded one, so a second tab is safe even if this guard is
  // bypassed.
  function sendAction(el, method, url, body, after) {
    if (el.getAttribute("aria-busy") === "true") return;
    setRowBusy(el, true);
    fetch(url, {
      method: method,
      headers: body ? { "Content-Type": "application/json" } : {},
      credentials: "same-origin",
      body: body ? JSON.stringify(body) : undefined,
    })
      .then(function (r) {
        if (r.ok) return after ? after() : undefined;
        // A refusal is more than a status: the gateway proxies memory's 409
        // with the reason the write was not recorded (the row already carries a
        // different outcome), so show that instead of a bare "HTTP 409", which
        // reads like a transport failure. A response without a JSON body keeps
        // the status line.
        return r
          .json()
          .catch(function () {
            return null;
          })
          .then(function (body) {
            throw new Error((body && body.error) || "HTTP " + r.status);
          });
      })
      .then(function () {
        reconcile();
        refreshList();
      })
      .catch(function (err) {
        // Leave the row as-is; the next event/reconcile re-syncs state. A failed
        // mutation is not silent any more: the earlier swallowed error is what
        // made a broken resolve impossible to report.
        if (window.MemoryApp && window.MemoryApp.toast) {
          window.MemoryApp.toast(
            "error",
            "Could not update the notification" + (err && err.message ? " (" + err.message + ")" : "") + "."
          );
        }
      })
      .finally(function () {
        setRowBusy(el, false);
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

  // resolveAction records the outcome of an actionable notification's button
  // and then finishes the item: a resolved action is done, so the row is also
  // marked read and archived (cleared). Without that it stayed in the Action
  // required tab after Accept (issue #599) — that tab used to filter on
  // requires_action alone; the server now also requires action_status IS NULL,
  // so these follow-ups are convenience and the tab leaves the row even when
  // they fail.
  //
  // The verb travels in `action`, the field this gateway route binds
  // (notifications_handlers.go); the gateway translates it to memory's
  // `status` field itself. `status` here would bind nothing and the resolve
  // would go upstream verb-less.
  function resolveAction(el, base, verb) {
    sendAction(el, "POST", base + "/resolve", { action: verb }, function () {
      return markReadAndArchive(base);
    });
  }

  // markReadAndArchive marks a notification read and then archives it. Both
  // are best-effort: a resolve already succeeded, so a failed follow-up must
  // not surface as a broken row — the next reconcile re-syncs it.
  function markReadAndArchive(base) {
    return fetch(base + "/read", {
      method: "PATCH",
      credentials: "same-origin",
    })
      .catch(function () {})
      .then(function () {
        return fetch(base + "/clear", {
          method: "POST",
          credentials: "same-origin",
        });
      })
      .catch(function () {});
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
        resolveAction(el, base, el.getAttribute("data-notif-verb") || "");
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
