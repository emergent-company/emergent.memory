/* Memory web UI — agent settings client.
   Two delegated behaviours, both keyed off data attributes so htmx swaps re-init
   them (listeners live on document, init runs on load and after every swap):

   1. Debounced auto-save for a settings form marked `data-agent-autosave`
      (the agent General subpage). Text fields save ~700ms after typing stops;
      selects, pickers and the visibility listbox save on change; an unchanged
      value never posts. A node marked `data-autosave-status` reflects
      saving/saved/error, and a failed save keeps the edited values in place
      with a retry affordance.

   2. Tool-approval inheritance on the Tools subpage: changing the agent default
      policy or a capability group's policy relabels every policy select's
      `Inherit (<value>)` option to the value it actually inherits. It also
      cancels the native <summary> activation for a policy <select> inside a
      disclosure header, so choosing a group policy no longer folds the group
      (spec: a summary's activation behaviour runs unless the click is
      cancelled, and stopPropagation alone does not cancel it).

   Loaded on every page from ui.templ; each behaviour no-ops when its markup is
   absent. Kept dependency-free (no Alpine/htmx coupling) so the js-dom gate can
   load it verbatim. */
(function () {
  "use strict";

  var SAVE_DEBOUNCE_MS = 700;

  /* ---------- debounced auto-save ---------- */

  /* Order-preserving serialization of the whole form, used both as the save
     body and as the "did anything change?" comparison. */
  function formSignature(form) {
    try {
      return new URLSearchParams(new FormData(form)).toString();
    } catch (e) {
      return "";
    }
  }

  function setStatus(form, state, message) {
    var status = form.querySelector("[data-autosave-status]");
    if (!status) return;
    status.setAttribute("data-state", state);
    var label = status.querySelector("[data-autosave-status-label]");
    if (label && message != null) label.textContent = message;
    var icon = status.querySelector("[data-autosave-icon]");
    if (icon) {
      icon.classList.remove(
        "lucide--cloud-check",
        "lucide--loader-circle",
        "lucide--alert-triangle",
        "text-muted-faint",
        "text-error",
        "animate-spin"
      );
      if (state === "saving") {
        icon.classList.add("lucide--loader-circle", "animate-spin", "text-muted-faint");
      } else if (state === "error") {
        icon.classList.add("lucide--alert-triangle", "text-error");
      } else {
        icon.classList.add("lucide--cloud-check", "text-muted-faint");
      }
    }
    var retry = status.querySelector("[data-autosave-retry]");
    if (retry) retry.hidden = state !== "error";
  }

  function initAutosave(form) {
    if (form.getAttribute("data-autosave-bound") === "1") return;
    form.setAttribute("data-autosave-bound", "1");

    var url = form.getAttribute("data-autosave-url") || form.getAttribute("action");
    if (!url) return;

    var saved = formSignature(form);
    var timer = null;
    var inflight = null; // AbortController while a request is in flight

    function clearTimer() {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
    }

    function save() {
      timer = null;
      var snap = formSignature(form);
      if (snap === saved) return;
      if (inflight) return; // completion re-schedules; see below
      var ctrl = typeof AbortController === "function" ? new AbortController() : null;
      inflight = ctrl;
      setStatus(form, "saving", "Saving…");
      var opts = {
        method: "POST",
        body: snap,
        headers: {
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
          Accept: "application/json",
          "X-Requested-With": "fetch",
        },
        credentials: "same-origin",
      };
      if (ctrl) opts.signal = ctrl.signal;
      fetch(url, opts)
        .then(function (r) {
          return r
            .json()
            .catch(function () {
              return {};
            })
            .then(function (body) {
              if (!r.ok) {
                var err = new Error((body && (body.error || body.message)) || "HTTP " + r.status);
                err.status = r.status;
                throw err;
              }
              return body;
            });
        })
        .then(function () {
          inflight = null;
          saved = snap;
          if (formSignature(form) !== saved) {
            schedule(0); // changed again while saving — persist the newer value
          } else {
            setStatus(form, "saved", "All changes saved");
          }
        })
        .catch(function (err) {
          inflight = null;
          if (err && err.name === "AbortError") return;
          setStatus(form, "error", (err && err.message) || "Could not save changes");
        });
    }

    function schedule(delay) {
      clearTimer();
      timer = setTimeout(save, delay);
    }

    /* Text fields debounce; everything else posts promptly. The icon picker's
       search box is not a settings field, so it is skipped. */
    form.addEventListener("input", function (ev) {
      var t = ev.target;
      if (!t || !t.matches) return;
      if (t.closest("[data-gd-icon-panel]")) return;
      if (
        t.matches(
          "input[type=text], input[type=search], input[type=email], input[type=url], input[type=tel], textarea"
        )
      ) {
        schedule(SAVE_DEBOUNCE_MS);
      }
    });

    form.addEventListener("change", function (ev) {
      var t = ev.target;
      if (!t || !t.matches) return;
      if (t.matches("select, input[type=checkbox], input[type=radio], input[type=color], input[type=hidden]")) {
        schedule(0);
      }
    });

    /* Custom controls that commit without dispatching a native change: the
       go-daisy icon picker (option/reset clicks) and the Alpine visibility
       listbox (option clicks / Enter / Space). */
    form.addEventListener("click", function (ev) {
      var t = ev.target;
      if (t && t.closest && t.closest("[data-gd-icon-option], [data-gd-icon-reset], [role=option]")) {
        schedule(0);
      }
    });
    form.addEventListener("keydown", function (ev) {
      if (ev.key !== "Enter" && ev.key !== " ") return;
      var t = ev.target;
      if (t && t.closest && t.closest("[role=listbox]")) schedule(1);
    });

    var retry = form.querySelector("[data-autosave-retry]");
    if (retry) retry.addEventListener("click", function () { schedule(0); });

    // The auto-saving General form no longer navigates on submit (Enter): save
    // in place instead. Exposed on the element so the document-level capture
    // handler below wins the race against htmx's boost listener.
    form.__autoSave = save;
    form.__clearTimer = clearTimer;
  }

  function flushAutosaves() {
    document.querySelectorAll("[data-agent-autosave]").forEach(function (form) {
      if (typeof form.__autoSave === "function") form.__autoSave();
    });
  }

  function cancelAutosaves() {
    document.querySelectorAll("[data-agent-autosave]").forEach(function (form) {
      if (typeof form.__clearTimer === "function") form.__clearTimer();
    });
  }

  // Intercept implicit submission (Enter) before htmx boost can navigate.
  document.addEventListener(
    "submit",
    function (ev) {
      var form = ev.target;
      if (form && typeof form.__autoSave === "function") {
        ev.preventDefault();
        ev.stopImmediatePropagation();
        form.__autoSave();
      }
    },
    true
  );

  // An in-app (boosted) navigation away from the form must not drop a pending
  // edit: flush it first. A real unload cancels the timer instead — the browser
  // would abort the request anyway.
  document.addEventListener("htmx:before:request", flushAutosaves);
  window.addEventListener("pagehide", cancelAutosaves);

  /* ---------- tool-approval inheritance ---------- */

  function policyLabel(value) {
    if (value === "allow") return "Allow";
    if (value === "ask") return "Ask";
    if (value === "deny") return "Deny";
    return "";
  }

  function setInheritLabel(select, label) {
    var opt = select.querySelector("option[data-inherit-option]") || select.options[0];
    if (opt) opt.textContent = label ? "Inherit (" + label + ")" : "Inherit";
  }

  /* Recompute every policy select's Inherit label from the current default and
     group selections. Resolution mirrors the server: explicit tool → owning
     group policy → agent default. Tool selects outside any capability group
     (source/relay/other blocks) inherit the default directly. */
  function syncToolPolicyLabels() {
    var defaultSel = document.querySelector('select[name="defaultToolPolicy"]');
    if (!defaultSel && !document.querySelector('select[name^="groupPolicy."]')) return;
    var defLabel = policyLabel(defaultSel ? defaultSel.value : "");

    document.querySelectorAll("[data-tool-group]").forEach(function (group) {
      var groupSel = group.querySelector('select[name^="groupPolicy."]');
      if (groupSel) setInheritLabel(groupSel, defLabel);
      var groupVal = groupSel && groupSel.value !== "inherit" ? groupSel.value : "";
      var groupLabel = groupVal ? policyLabel(groupVal) : defLabel;
      group.querySelectorAll('select[name^="toolPolicy."]').forEach(function (sel) {
        setInheritLabel(sel, groupLabel);
      });
    });

    document.querySelectorAll('select[name^="toolPolicy."]').forEach(function (sel) {
      if (sel.closest("[data-tool-group]")) return;
      setInheritLabel(sel, defLabel);
    });
  }

  document.addEventListener("change", function (ev) {
    var t = ev.target;
    if (t && t.matches && t.matches('select[name="defaultToolPolicy"], select[name^="groupPolicy."]')) {
      syncToolPolicyLabels();
    }
  });

  /* A <select> inside <summary> must not run the summary's activation behaviour
     (fold/unfold) when a group policy is chosen. stopPropagation cannot cancel
     it — only preventDefault can — so cancel it in the capture phase, before the
     activation check. Scoped to the group policy select; the native dropdown
     opens on the pointer gesture and is unaffected. */
  document.addEventListener(
    "click",
    function (ev) {
      var t = ev.target;
      if (!t || !t.closest) return;
      var sel = t.closest('select[name^="groupPolicy."]');
      if (sel && sel.closest("summary")) ev.preventDefault();
    },
    true
  );

  function boot() {
    syncToolPolicyLabels();
    document.querySelectorAll("[data-agent-autosave]").forEach(initAutosave);
  }

  document.addEventListener("DOMContentLoaded", boot);
  document.addEventListener("htmx:after:swap", boot);
  // If this script is loaded after DOMContentLoaded already fired (embedded
  // pages/tests), boot once anyway.
  if (document.readyState !== "loading") boot();
})();
