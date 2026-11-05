/* Memory web UI — app-level client.
   Handles: agent create/edit/delete dialogs (JSON fetch to /api/agents),
   toast notifications, and the sessions list helpers.
   Event delegation on document so bindings survive HTMX sidebar swaps. */
(function () {
  "use strict";

  /* ---------- scoped API calls ---------- */
  /* Org/project context lives in the URL, not a cookie. Same-origin /api/*
     calls are unscoped paths, so propagate the shell's resolved scope from the
     <body> data attributes. fetch gets headers; EventSource (which cannot set
     headers) gets query params, which the gateway accepts and validates. */
  function resolveApiURL(input) {
    try {
      var raw = typeof input === "string" ? input : (input && input.url) || "";
      if (!raw) return null;
      var u = new URL(raw, window.location.href);
      if (u.origin !== window.location.origin) return null;
      if (u.pathname.indexOf("/api/") !== 0) return null;
      return u;
    } catch (e) {
      return null;
    }
  }

  var scopeProject = document.body
    ? document.body.getAttribute("data-project-id") || ""
    : "";
  var scopeOrg = document.body ? document.body.getAttribute("data-org-id") || "" : "";

  /* Scoped-path helper for URLs built in JS (fetch/htmx targets). Mirrors the
     gateway's link scoping: app paths get the scope base; API/auth/asset/global
     paths and external/fragment URLs are returned unchanged. */
  window.MemoryScope = {
    org: scopeOrg,
    project: scopeProject,
    base: scopeProject
      ? "/o/" + scopeOrg + "/p/" + scopeProject
      : scopeOrg
        ? "/o/" + scopeOrg
        : "",
    path: function (p) {
      if (!p || p.charAt(0) !== "/" || p.charAt(1) === "/") return p;
      if (
        p.indexOf("/o/") === 0 ||
        p.indexOf("/api/") === 0 ||
        p.indexOf("/auth/") === 0 ||
        p.indexOf("/share/") === 0 ||
        p.indexOf("/static/") === 0 ||
        p.indexOf("/assets/") === 0 ||
        p.indexOf("/webhooks/") === 0 ||
        p.indexOf("/internal/") === 0 ||
        p.indexOf("/dev/") === 0 ||
        p.indexOf("/profile") === 0 ||
        p.indexOf("/orgs") === 0 ||
        p.indexOf("/invites") === 0 ||
        p.indexOf("/partial/invite-user-search") === 0
      ) {
        return p;
      }
      return (this.base || "") + p;
    },
  };

  if (scopeProject || scopeOrg) {
    var nativeFetch = window.fetch.bind(window);
    window.fetch = function (input, init) {
      if (resolveApiURL(input)) {
        init = init || {};
        try {
          var headers = new Headers(
            init.headers || (input && input.headers) || undefined
          );
          if (scopeProject) headers.set("X-Project-ID", scopeProject);
          if (scopeOrg) headers.set("X-Org-ID", scopeOrg);
          init.headers = headers;
        } catch (e) {
          /* Headers unavailable: leave the request unscoped. */
        }
      }
      return nativeFetch(input, init);
    };

    var NativeEventSource = window.EventSource;
    if (NativeEventSource) {
      window.EventSource = function (url, config) {
        var u = resolveApiURL(url);
        if (u) {
          if (scopeProject && u.searchParams.get("project_id") === null) {
            u.searchParams.set("project_id", scopeProject);
          }
          if (scopeOrg && u.searchParams.get("org_id") === null) {
            u.searchParams.set("org_id", scopeOrg);
          }
          url = u.toString();
        }
        return new NativeEventSource(url, config);
      };
      window.EventSource.prototype = NativeEventSource.prototype;
    }
  }

  /* ---------- sentry ---------- */
  /* Report an error to Sentry when the browser SDK is active (gated on
     window.Sentry, present only when the loader was injected because a
     Sentry DSN is configured). No-op otherwise. */
  function captureError(err) {
    if (window.Sentry && err) window.Sentry.captureException(err);
  }

  /* True for expected transient memory failures (brief restart / 502-504),
     which must not reach Sentry. Delegates the classification to the shared
     MemoryChatHost helper (chat-host.js is loaded globally in ui.templ on the
     same pages as this file); degrades safely if that script is unavailable. */
  function isTransientError(err) {
    var host = window.MemoryChatHost;
    return !!(host && typeof host.isTransientError === "function" && host.isTransientError(err));
  }

  /* ---------- PWA standalone detection ---------- */
  /* iOS reports installed standalone web apps as (display-mode: fullscreen)
     (WebKit bug 264218) and never matches (display-mode: standalone), so trust
     navigator.standalone first, then the two media queries. Tag <html> so CSS
     and templates can adapt chrome in PWA mode. */
  var standalone =
    window.navigator.standalone === true ||
    window.matchMedia("(display-mode: standalone)").matches ||
    window.matchMedia("(display-mode: fullscreen)").matches;
  if (standalone) document.documentElement.setAttribute("data-standalone", "");

  /* ---------- visual viewport (software keyboard) ---------- */
  /* dvh tracks browser toolbars but not the software keyboard. Publish the
     visual viewport height as --app-vh and flag an open keyboard so CSS can
     shrink the shell and keep bottom-anchored composers visible. */
  (function () {
    var root = document.documentElement;
    function syncViewportHeight() {
      var vv = window.visualViewport;
      /* visualViewport reports CSS pixels *at the current zoom*, so a pinch
         zoom shrinks it exactly like a software keyboard does and would be
         read as one: the shell collapsed and both drawers went blank while
         the page reflowed at every zoom step. Scale back to layout pixels
         before comparing, so zoom leaves --app-vh alone and only a real
         keyboard (a layout-space loss) flags the shell. */
      var h = vv ? vv.height * (vv.scale || 1) : window.innerHeight;
      root.style.setProperty("--app-vh", Math.round(h) + "px");
      var keyboard = !!vv && window.innerHeight - h > 120;
      root.classList.toggle("keyboard-open", keyboard);
    }
    syncViewportHeight();
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", syncViewportHeight);
      window.visualViewport.addEventListener("scroll", syncViewportHeight);
    }
    window.addEventListener("resize", syncViewportHeight);
    window.addEventListener("orientationchange", syncViewportHeight);
  })();

  /* ---------- toast ---------- */
  /* Push a toast into the Alpine toast queue (rendered by
     ui.ToastQueueWithProps in the app shell). Alpine owns the queue, dismissal
     timer, countdown progress bar, and hover-to-pause.
     Errors are persistent (duration 0 = no auto-dismiss): the queue renders a
     close button for them. Every other kind auto-dismisses. Mirrors
     toastDurationMs in ui.go. */
  function toastDuration(kind) {
    return kind === "error" ? 0 : 4200;
  }
  function toast(kind, message) {
    var container = document.getElementById("toast-container");
    if (!container || !window.Alpine) return;
    var queue = window.Alpine.$data(container);
    if (queue && queue.add) queue.add({ type: kind, message: message, duration: toastDuration(kind) });
  }

  /* stash a toast across a page reload (CRUD flow) */
  function stashToast(kind, message) {
    try { sessionStorage.setItem("memory-toast", JSON.stringify({ kind: kind, message: message })); } catch (e) {}
  }
  function flushStashedToast() {
    try {
      var raw = sessionStorage.getItem("memory-toast");
      if (!raw) return;
      sessionStorage.removeItem("memory-toast");
      var t = JSON.parse(raw);
      if (t && t.message) toast(t.kind || "info", t.message);
    } catch (e) {}
  }
  document.addEventListener("DOMContentLoaded", flushStashedToast);

  /* Inline-save feedback: the server signals a toast via the HX-Trigger
     response header ("memory-toast"), carrying {kind, message} as the event
     detail. Delegated on document.body so it survives HTMX swaps. */
  document.body.addEventListener("memory-toast", function (evt) {
    var d = evt.detail || {};
    if (d.message) toast(d.kind || "info", d.message);
  });

  /* Provider/model test results: the server signals an alert modal via the
     HX-Trigger response header ("memory-alert"), carrying {kind, title, message}
     as the event detail. A modal keeps long provider replies readable and
     dismissible (a toast auto-dismisses). Delegated on document.body so it
     survives HTMX swaps. */
  document.body.addEventListener("memory-alert", function (evt) {
    var d = evt.detail || {};
    alertModal(d.kind || "info", d.title, d.message);
  });

  /* ---------- agent form dialog ---------- */

  function el(id) { return document.getElementById(id); }

  function formDialog() { return el("agent-form-modal"); }
  function deleteDialog() { return el("delete-confirm-modal"); }

  function closeFormDialog() { var d = formDialog(); if (d) d.close(); }
  function closeDeleteDialog() { var d = deleteDialog(); if (d) d.close(); }

  function setFormBusy(busy) {
    var btn = el("agent-form-submit");
    if (!btn) return;
    btn.classList.toggle("loading", busy);
    btn.disabled = busy;
  }

  /* Sync a go-daisy IconPicker to a value. The bundled runtime only syncs on
     user clicks, so hydration from an agent payload must mirror its commit():
     write the hidden input, refresh the trigger label/glyph, and mark the tile
     selected. No-op when the picker is absent (other pages reuse this file). */
  function setIconPickerValue(id, value) {
    var input = el(id);
    if (!input) return;
    var root = input.closest("[data-gd-icon-picker]");
    if (!root) return;
    var v = value || "";
    input.value = v;
    var panel = root.querySelector("[data-gd-icon-panel]");
    var option = null;
    var opts = panel ? panel.querySelectorAll("[data-gd-icon-option]") : [];
    for (var i = 0; i < opts.length; i++) {
      if ((opts[i].getAttribute("data-gd-icon-value") || "") === v) { option = opts[i]; break; }
    }
    var defaultVal = root.getAttribute("data-gd-icon-picker-default-value") || "";
    var isDefault = v === defaultVal;
    var labelEl = root.querySelector("[data-gd-icon-label]");
    if (labelEl) {
      labelEl.textContent = option
        ? option.getAttribute("data-gd-icon-label")
        : root.getAttribute("data-gd-icon-picker-default-label") || "";
      /* Pin `text-base-content/50` — NOT the muted-scale `text-muted`. This
         mirror must toggle the SAME class go-daisy's own server render
         (icon-picker.templ) and runtime commit (go-daisy-icon-picker.js) apply,
         or the remove here targets a class that is never present and the label
         stays dimmed after a non-default selection. */
      labelEl.classList.toggle("text-base-content/50", isDefault);
    }
    var glyphEl = root.querySelector("[data-gd-icon-glyph]");
    if (glyphEl) {
      var glyph = option
        ? option.getAttribute("data-gd-icon-class")
        : root.getAttribute("data-gd-icon-picker-default-class") || "";
      while (glyphEl.firstChild) glyphEl.removeChild(glyphEl.firstChild);
      var span = document.createElement("span");
      span.className = "iconify size-5 " + glyph;
      span.setAttribute("aria-hidden", "true");
      glyphEl.appendChild(span);
    }
    for (var j = 0; j < opts.length; j++) {
      var active = (opts[j].getAttribute("data-gd-icon-value") || "") === v;
      opts[j].setAttribute("aria-selected", active ? "true" : "false");
      opts[j].setAttribute("tabindex", active ? "0" : "-1");
    }
    if (panel) {
      var reset = panel.querySelector("[data-gd-icon-reset]");
      if (reset) reset.style.display = isDefault ? "none" : "";
    }
  }

  /* Sync a go-daisy ColorPicker's native swatch to its text value (the text
     input is the real form control; hydration sets both). */
  function setColorPickerValue(id, value) {
    var input = el(id);
    if (!input) return;
    var v = value || "";
    input.value = v;
    var root = input.closest("[data-gd-color-picker]");
    var swatch = root ? root.querySelector("[data-gd-color-swatch]") : null;
    if (!swatch) return;
    var h = v.trim();
    if (/^#[0-9a-fA-F]{3}$/.test(h)) h = "#" + h[1] + h[1] + h[2] + h[2] + h[3] + h[3];
    else if (/^#[0-9a-fA-F]{8}$/.test(h)) h = h.slice(0, 7);
    if (/^#[0-9a-fA-F]{6}$/.test(h)) swatch.value = h;
  }

  /* The agent's stored short description, hydrated when editing and echoed back
     on save. The dialog has no description field; carrying the value keeps an
     edit from clearing a description set on the agent's General settings form
     (the write is a full PUT). */
  var agentDialogDescription = "";

  /* Tools and skills are configured on the agent's settings subpages, not in
     this dialog (#1305), but the save is a full PUT: carry the stored lists so
     editing name/prompt here never clears settings made elsewhere. A create
     starts from the server defaults (empty whitelists). */
  var agentDialogTools = [];
  var agentDialogSkills = [];

  /* open with an agent id, or "" for create */
  function openAgentForm(id) {
    var d = formDialog();
    if (!d) return;
    var form = el("agent-form");
    form.reset();
    agentDialogDescription = "";
    agentDialogTools = [];
    agentDialogSkills = [];
    resetDelegationTargets();
    setIconPickerValue("agent-icon", "");
    setColorPickerValue("agent-color", "");
    el("agent-id").value = id || "";
    el("agent-form-title").textContent = id ? "Edit agent" : "New agent";
    el("agent-form-subtitle").textContent = id
      ? "Adjust the persona or brain — Memory applies it live."
      : "Define a persona and a brain.";
    el("agent-form-submit").textContent = id ? "Save changes" : "Create agent";
    setFormBusy(false);

    if (id) {
      // hydrate from the full definition
      fetch("/api/agents/" + encodeURIComponent(id), { headers: { "Accept": "application/json" } })
        .then(function (r) {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json();
        })
        .then(function (a) {
          if (!a || !a.name) throw new Error("bad agent payload");
          el("agent-name").value = a.name || "";
          agentDialogDescription = a.description || "";
          el("agent-prompt").value = a.systemPrompt || "";
          setIconPickerValue("agent-icon", a.uiConfig && a.uiConfig.icon);
          setColorPickerValue("agent-color", a.uiConfig && a.uiConfig.color);
          setModelValue((a.model && a.model.name) || "");
          el("agent-temperature").value = a.model && a.model.temperature != null ? a.model.temperature : "";
          el("agent-max-tokens").value = a.model && a.model.maxTokens ? a.model.maxTokens : "";
          agentDialogTools = Array.isArray(a.tools) ? a.tools.slice() : [];
          agentDialogSkills = Array.isArray(a.skills) ? a.skills.slice() : [];
          hydrateDelegation(a);
        })
        .catch(function (err) {
          if (isTransientError(err)) {
            console.warn("app: agent load failed (transient, not reported):", err);
          } else {
            captureError(err);
          }
          toast("error", "Could not load agent: " + (err && err.message ? err.message : "unknown error"));
          closeFormDialog();
        });
    }
    syncDelegationUI();
    d.showModal();
  }

  /* preselect a model in the catalog dropdown; if the agent's model is no
     longer in the catalog, keep it as a synthetic "current" option so the
     value round-trips unchanged on save. */
  function setModelValue(name) {
    var sel = el("agent-model");
    if (!sel || !name) return;
    var existing = false;
    for (var i = 0; i < sel.options.length; i++) {
      if (sel.options[i].value === name) { existing = true; break; }
    }
    if (existing) {
      sel.value = name;
      return;
    }
    var opt = document.createElement("option");
    opt.value = name;
    opt.textContent = name + " (current)";
    sel.appendChild(opt);
    sel.value = name;
  }

  /* ---------- delegation ---------- */

  /* clear the target picker for a fresh open: uncheck, re-enable, and
     un-hide every target (an earlier edit may have hidden the self row). */
  function resetDelegationTargets() {
    var cbs = document.querySelectorAll('input[name="delegation-target"]');
    for (var i = 0; i < cbs.length; i++) {
      cbs[i].checked = false;
      cbs[i].disabled = false;
      var lbl = cbs[i].closest("label");
      if (lbl) lbl.classList.remove("hidden");
    }
    var emptyNote = el("agent-delegation-empty");
    if (emptyNote) emptyNote.classList.toggle("hidden", cbs.length > 0);
  }

  /* hydrate the delegation toggle + target checkboxes from the agent def.
     The agent being edited never appears in its own target list. */
  function hydrateDelegation(a) {
    var toggle = el("agent-delegation-enabled");
    if (!toggle) return;
    var del = a.delegation;
    var targets = (del && Array.isArray(del.targets)) ? del.targets : [];
    toggle.checked = !!(del && del.enabled);
    var cbs = document.querySelectorAll('input[name="delegation-target"]');
    var visible = 0;
    for (var i = 0; i < cbs.length; i++) {
      var cb = cbs[i];
      if (cb.value === a.name) {
        var lbl = cb.closest("label");
        if (lbl) lbl.classList.add("hidden");
        cb.checked = false;
        continue;
      }
      cb.checked = targets.indexOf(cb.value) !== -1;
      visible++;
    }
    var emptyNote = el("agent-delegation-empty");
    if (emptyNote) emptyNote.classList.toggle("hidden", visible > 0);
    syncDelegationUI();
  }

  /* when the toggle is off, dim + disable the target picker so its values
     are clearly ignored; submit only reads it while the toggle is on. */
  function syncDelegationUI() {
    var toggle = el("agent-delegation-enabled");
    var targets = el("agent-delegation-targets");
    if (!toggle || !targets) return;
    var on = toggle.checked;
    targets.classList.toggle("opacity-50", !on);
    var cbs = targets.querySelectorAll('input[name="delegation-target"]');
    for (var i = 0; i < cbs.length; i++) cbs[i].disabled = !on;
  }

  function submitAgentForm(ev) {
    ev.preventDefault();
    var id = el("agent-id").value;
    var name = el("agent-name").value.trim();
    if (!name) { el("agent-name").focus(); return; }
    var modelName = el("agent-model").value.trim();
    var temp = parseFloat(el("agent-temperature").value);
    var maxTok = parseInt(el("agent-max-tokens").value, 10);

    var body = {
      name: name,
      description: agentDialogDescription,
      systemPrompt: el("agent-prompt").value,
      tools: agentDialogTools.slice(),
    };
    // Appearance: always send the uiConfig blob (an empty {} clears any prior
    // appearance), carrying only the values that are set.
    var iconValue = el("agent-icon") ? el("agent-icon").value.trim() : "";
    var colorValue = el("agent-color") ? el("agent-color").value.trim() : "";
    body.uiConfig = {};
    if (iconValue) body.uiConfig.icon = iconValue;
    if (colorValue) body.uiConfig.color = colorValue;
    // Carry the stored skills back so this dialog's save never clears a
    // selection made on the agent's Skills settings subpage (full PUT).
    body.skills = agentDialogSkills.slice();
    if (modelName) {
      body.model = {
        name: modelName,
        temperature: isFinite(temp) ? temp : 0.7,
        maxTokens: isFinite(maxTok) && maxTok > 0 ? maxTok : 4096,
      };
    }
    var delToggle = el("agent-delegation-enabled");
    if (delToggle && delToggle.checked) {
      var targets = [];
      var cbs = document.querySelectorAll('input[name="delegation-target"]:checked');
      for (var i = 0; i < cbs.length; i++) targets.push(cbs[i].value);
      if (!targets.length) {
        toast("error", "Pick at least one agent to delegate to, or turn off delegation.");
        return;
      }
      body.delegation = { enabled: true, targets: targets };
    }

    setFormBusy(true);
    var url = "/api/agents" + (id ? "/" + encodeURIComponent(id) : "");
    var method = id ? "PUT" : "POST";
    fetch(url, {
      method: method,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
      .then(function (r) {
        if (r.status === 204) return null;
        return r.json().catch(function () { return null; }).then(function (j) {
          if (!r.ok) {
            var e = new Error((j && (j.error || j.message)) || "HTTP " + r.status);
            e.status = r.status;
            throw e;
          }
          return j;
        });
      })
      .then(function () {
        stashToast("success", id ? "Agent updated" : "Agent created");
        window.location.reload();
      })
      .catch(function (err) {
        setFormBusy(false);
        if (isTransientError(err)) {
          console.warn("app: agent save failed (transient, not reported):", err);
        } else {
          captureError(err);
        }
        toast("error", "Save failed: " + ((err && err.message) || String(err)));
      });
  }

  /* ---------- delete confirm dialog ---------- */
  var deleteTargetID = "";

  function openDeleteConfirm(id, name) {
    var d = deleteDialog();
    if (!d) return;
    deleteTargetID = id;
    el("delete-agent-name").textContent = name;
    d.showModal();
  }

  function confirmDelete() {
    if (!deleteTargetID) return;
    var btn = el("delete-agent-go");
    btn.classList.add("loading");
    btn.disabled = true;
    fetch("/api/agents/" + encodeURIComponent(deleteTargetID), { method: "DELETE" })
      .then(function (r) {
        if (r.status !== 204) {
          return r.json().catch(function () { return null; }).then(function (j) {
            var e = new Error((j && (j.error || j.message)) || "HTTP " + r.status);
            e.status = r.status;
            throw e;
          });
        }
      })
      .then(function () {
        stashToast("success", "Agent deleted");
        window.location.reload();
      })
      .catch(function (err) {
        btn.classList.remove("loading");
        btn.disabled = false;
        if (isTransientError(err)) {
          console.warn("app: agent delete failed (transient, not reported):", err);
        } else {
          captureError(err);
        }
        toast("error", "Delete failed: " + ((err && err.message) || String(err)));
      });
  }

  /* ---------- wire up (delegated, idempotent) ---------- */
  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest("[data-action]");
    if (!btn) return;
    switch (btn.getAttribute("data-action")) {
      case "open-agent-form":
        openAgentForm(btn.getAttribute("data-id") || "");
        break;
      case "close-agent-form":
        closeFormDialog();
        break;
      case "open-delete-confirm":
        openDeleteConfirm(btn.getAttribute("data-id") || "", btn.getAttribute("data-name") || "this agent");
        break;
      case "close-delete-confirm":
        closeDeleteDialog();
        break;
    }
  });

  document.addEventListener("click", function (ev) {
    // Whole-row/card links: table rows and agent cards carry data-href, so a
    // tap anywhere on them navigates (agents cards open chat). Clicks on
    // interactive controls (the name link, Run now button, the enable switch,
    // etc.) keep their own behaviour.
    var row = ev.target.closest("[data-href]");
    if (!row) return;
    if (ev.target.closest("a, button, input, label, select, textarea, form")) return;
    window.location.href = row.getAttribute("data-href");
  });

  document.addEventListener("submit", function (ev) {
    // `.matches("#agent-form")` uses the id *attribute*, not the shadowed
    // `.id` property — the form's <input name="id"> shadows `form.id` in the
    // browser, so `ev.target.id === "agent-form"` would never match.
    if (ev.target && ev.target.matches && ev.target.matches("#agent-form")) submitAgentForm(ev);
  });

  document.addEventListener("change", function (ev) {
    if (ev.target && ev.target.id === "agent-delegation-enabled") syncDelegationUI();
  });

  document.addEventListener("click", function (ev) {
    if (ev.target && ev.target.id === "delete-agent-go") confirmDelete();
  });

  /* ---------- auto-grow textareas + long-text character counter ---------- */
  /* Any textarea with data-autogrow starts as a small multi-line box and grows
     to fit its content, capped by its CSS max-height (overflow scrolls past the
     cap). Mirrors the chat composer's auto-grow for free-form string fields
     (e.g. object properties that can hold long text). Delegated on document so
     it survives HTMX swaps, and re-run after swaps below. */
  function autogrow(el) {
    if (!el) return;
    // Preserve a height the user dragged the resize handle to (long-text fields
    // are resize-y): capture it before we reset the inline height.
    var floor = el.dataset.userResized === "1" ? Math.round(el.getBoundingClientRect().height) : 0;
    el.style.height = "auto";
    var max = 0;
    try { max = parseInt(getComputedStyle(el).maxHeight, 10) || 0; } catch (e) {}
    var h = el.scrollHeight;
    if (max > 0 && h > max) h = max;
    if (floor > h) h = floor;
    el.style.height = h + "px";
  }

  function autogrowAll(root) {
    (root || document).querySelectorAll("textarea[data-autogrow]").forEach(autogrow);
  }

  /* The live character count lives in the field's [data-char-counter] wrapper,
     written into its [data-char-count-value] element. Comma-grouped to match the
     server-rendered initial value. */
  function charCountText(n) {
    var s = String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return s + (n === 1 ? " character" : " characters");
  }

  function updateCharCount(el) {
    if (!el) return;
    var wrap = el.closest && el.closest("[data-char-counter]");
    if (!wrap) return;
    var out = wrap.querySelector("[data-char-count-value]");
    if (out) out.textContent = charCountText(Array.from(el.value).length);
  }

  function charCountAll(root) {
    (root || document).querySelectorAll("textarea[data-autogrow]").forEach(updateCharCount);
  }

  document.addEventListener("input", function (ev) {
    var t = ev.target;
    if (t && t.matches && t.matches("textarea[data-autogrow]")) {
      autogrow(t);
      updateCharCount(t);
    }
  });

  /* Remember manual resize-y drags so a later keystroke's autogrow does not
     snap the box back down to the content height. */
  document.addEventListener("pointerdown", function (ev) {
    var t = ev.target;
    if (t && t.matches && t.matches("textarea[data-autogrow]")) {
      t.dataset.resizeStart = Math.round(t.getBoundingClientRect().height);
    }
  }, true);
  document.addEventListener("pointerup", function (ev) {
    var t = ev.target;
    if (!t || !t.matches || !t.matches("textarea[data-autogrow]")) return;
    var start = parseInt(t.dataset.resizeStart, 10);
    if (!isNaN(start) && Math.abs(Math.round(t.getBoundingClientRect().height) - start) > 4) {
      t.dataset.userResized = "1";
    }
  }, true);

  document.addEventListener("DOMContentLoaded", function () {
    autogrowAll();
    charCountAll();
  });

  /* Boosted client-side navigation swaps #main-content without firing
     DOMContentLoaded, so re-initialise auto-grow and counters after every swap. */
  document.addEventListener("htmx:after:swap", function () {
    autogrowAll();
    charCountAll();
  });

  /* ---------- schema editor + derive dialogs (delegated) ---------- */
  /* Native <dialog> open/close driven by data attributes, so the markup needs
     no inline handlers and the behaviour survives HTMX swaps (the listeners
     live on document). A dialog can be opened from inside a menu (a go-daisy
     popover or a CSS dropdown), so close any open popover and drop focus
     first: otherwise the menu lingers behind the modal, and native focus
     restoration on close would return focus into it and reopen it. */
  document.addEventListener("click", function (ev) {
    var opener = ev.target.closest("[data-dialog-open]");
    if (opener) {
      ev.preventDefault();
      if (window.goDaisy && window.goDaisy.popover && typeof window.goDaisy.popover.closeAll === "function") {
        window.goDaisy.popover.closeAll();
      }
      var active = document.activeElement;
      if (active && typeof active.blur === "function") active.blur();
      var dlg = document.getElementById(opener.getAttribute("data-dialog-open"));
      if (!dlg) return;
      if (typeof dlg.showModal === "function") dlg.showModal();
      else dlg.setAttribute("open", "");
      return;
    }
    var closer = ev.target.closest("[data-dialog-close]");
    if (closer) {
      ev.preventDefault();
      var owner = closer.closest("dialog");
      if (owner) owner.close();
    }
  });

  /* Object-type editor: append a property row cloned from the named <template>
     and remove the row a remove button belongs to. */
  document.addEventListener("click", function (ev) {
    var add = ev.target.closest("[data-add-property]");
    if (add) {
      ev.preventDefault();
      var tpl = document.getElementById(add.getAttribute("data-add-property"));
      var body = document.getElementById(add.getAttribute("data-property-rows"));
      if (!tpl || !body || !tpl.content) return;
      var row = tpl.content.firstElementChild.cloneNode(true);
      body.appendChild(row);
      var empty = document.getElementById("property-empty");
      if (empty) empty.remove();
      var first = row.querySelector('input[name="property_name"]');
      if (first) first.focus();
      return;
    }
    var rm = ev.target.closest("[data-remove-property]");
    if (rm) {
      ev.preventDefault();
      var target = rm.closest(".property-row");
      if (target) target.remove();
    }
  });

  /* Blueprint-derived edit gate: a form carrying data-requires-confirm opens
     the named <dialog> instead of submitting. Confirming re-submits exactly
     once; dismissing leaves the edited values untouched (nothing is posted). */
  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form || !form.getAttribute || !form.getAttribute("data-requires-confirm")) return;
    if (form.dataset.confirmed === "1") { form.dataset.confirmed = ""; return; }
    var dlg = document.getElementById(form.getAttribute("data-requires-confirm"));
    if (!dlg) return;
    ev.preventDefault();
    ev.stopImmediatePropagation();
    if (typeof dlg.showModal === "function") dlg.showModal();
  }, true);

  document.addEventListener("close", function (ev) {
    var dlg = ev.target;
    if (!dlg || dlg.tagName !== "DIALOG") return;
    var form = document.querySelector('form[data-requires-confirm="' + dlg.id + '"]');
    if (form && dlg.returnValue === "confirm") {
      form.dataset.confirmed = "1";
      if (typeof form.requestSubmit === "function") form.requestSubmit();
      else form.submit();
    }
    dlg.returnValue = "";
  }, true);

  /*
   * openDialog opens a native <dialog> by id. No-op when the element is missing
   * or is not a dialog (or is already open). Single entry point for every
   * dialog-opening call site: templ handlers with a literal id call
   * MemoryApp.openDialog, and templ inline row-dialog handlers call the
   * window.openDialogByID global (both delegate here).
   */
  function openDialog(id) {
    var d = id && document.getElementById(id);
    if (!d || typeof d.showModal !== "function" || d.open) return;
    d.showModal();
  }

  /*
   * openDialogByID opens a dialog whose id is computed at render time, matching
   * the row-dialog convention: openDialogByID("backup-delete-" + id). Exposed
   * as window.openDialogByID (below) for the templ inline row-dialog handlers
   * that cannot call openDialog with a literal id.
   */
  function openDialogByID(id) {
    openDialog(id);
  }

  /*
   * openAutoOpenDialogs opens every dialog marked with
   * data-dialog-autoopen. Called on load and after htmx swaps (components
   * .DialogAutoOpen replaces the per-page showModal() IIFEs that used to
   * re-open a dialog after a mutation swapped it in).
   */
  function openAutoOpenDialogs(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var dialogs = scope.querySelectorAll("dialog[data-dialog-autoopen]");
    for (var i = 0; i < dialogs.length; i++) openDialog(dialogs[i].id);
  }

  document.addEventListener("DOMContentLoaded", function () { openAutoOpenDialogs(document); });
  /* htmx dispatches htmx:after:swap on the swap SOURCE element (the element
     that carried hx-get/hx-post), which usually does NOT contain the swapped-in
     markup — a button targeting a sibling container is the common shape. Scoping
     the scan to ev.target therefore missed every lazily-fetched dialog (the
     extraction merge preview inserted into its sibling #merge-preview-target-*
     div stayed closed). Scan the whole document instead: openDialog is a no-op
     for dialogs that are already open. */
  document.addEventListener("htmx:after:swap", function () { openAutoOpenDialogs(document); });

  /*
   * alertModal fills and opens the shell's #memory-alert-modal
   * (components.AlertModal) with a provider/model test outcome. The banner tone
   * class and icon are driven by kind; title and message are written with
   * textContent because provider replies are untrusted and must never be parsed
   * as markup. No-op when the modal is absent (the shell always renders it, but
   * app.js is shared with pages/tests that may not).
   */
  function alertModal(kind, title, message) {
    var dialog = el("memory-alert-modal");
    if (!dialog || typeof dialog.showModal !== "function") return;
    var tones = {
      success: { cls: "alert-success", glyph: "lucide--check-circle" },
      error: { cls: "alert-error", glyph: "lucide--circle-alert" },
      warning: { cls: "alert-warning", glyph: "lucide--alert-triangle" },
    };
    var tone = tones[kind] || { cls: "alert-info", glyph: "lucide--info" };
    var banner = el("memory-alert-banner");
    var icon = el("memory-alert-icon");
    var titleEl = el("memory-alert-title");
    var messageEl = el("memory-alert-message");
    if (banner) banner.className = "alert " + tone.cls + " items-start gap-3";
    if (icon) icon.className = "iconify " + tone.glyph + " size-5 shrink-0";
    if (titleEl) titleEl.textContent = title || "Test result";
    if (messageEl) messageEl.textContent = message || "";
    if (!dialog.open) dialog.showModal();
  }

  /* Kanban board drag = the existing board action that matches the move. Every
     card is draggable; a valid (fromStatus, targetLane) pair maps to one of the
     backend's work-path transitions. Invalid pairs are rejected in the UI with
     no request. Delegated on document so it survives htmx swaps. */
  var boardDrag = null;

  // boardStatusDefaults mirrors the built-in work-path statuses. A project that
  // configures custom ones (e.g. todo/doing) renders them into
  // #board[data-board-status-map], which boardStatusMap() reads.
  var boardStatusDefaults = {
    ready: "ready",
    inProgress: "in_progress",
    review: "review",
    revision: "revision",
    blocked: "blocked",
    done: "done",
  };

  // boardStatusMap reads the board's configured work-path status mapping from
  // the #board swap region, falling back to the built-in statuses when the
  // attribute is absent or malformed. Read per gesture so it tracks re-renders.
  function boardStatusMap() {
    var board = document.getElementById("board");
    var raw = board && board.getAttribute("data-board-status-map");
    if (!raw) return boardStatusDefaults;
    try {
      var parsed = JSON.parse(raw);
      var map = {};
      for (var key in boardStatusDefaults) {
        map[key] = parsed[key] || boardStatusDefaults[key];
      }
      return map;
    } catch (e) {
      return boardStatusDefaults;
    }
  }

  /* boardMoveAction maps a drag (from status -> target lane) to the board action
     that implements it, or "" when the pair is not a move at all. Statuses
     resolve through the board's configured work-path mapping (sm), so a
     custom-mapped board exposes the same transitions as the default one:
       blocked -> ready     execute/retry  POST .../retry
       review  -> done      approve        POST .../approve
       *       -> blocked   cancel         POST .../cancel   (not from done)
       review  -> revision  request changes: needs feedback, so open the item
                            action dialog rather than firing a blind POST.
     Every other lane pair is a plain status change ("status"): the lanes are
     rendered from the compiled schema, and the exact-status endpoint validates
     them against the item's type. This is what makes a custom-status board
     draggable — before, any pair outside the canonical mapping was rejected
     client-side with no request and no feedback.

     The move decision alone is not enough for that fallback: the lanes are the
     union of every board-enabled type's statuses, so a lane can exist for
     another type and still refuse an exact-status write for this card.
     boardAllowedStatuses reads the card's own type's declared set and
     boardLaneAccepts withholds the rest, so the marked targets are the lanes the
     write can actually accept. */
  function boardMoveAction(from, to, sm) {
    if (!from || !to || from === to) return "";
    if (from === sm.blocked && to === sm.ready) return "retry";
    if (from === sm.review && to === sm.done) return "approve";
    if (from === sm.review && to === sm.revision) return "dialog";
    if (to === sm.blocked && from !== sm.done) return "cancel";
    return "status";
  }

  /* boardLanes returns the rendered lanes in visual order. Drag eligibility is
     derived from them (not from the canonical defaults) so the lanes marked as
     valid targets are exactly the lanes the drop will accept. */
  function boardLanes() {
    return document.querySelectorAll("#board [data-board-column]");
  }

  /* Work board (global Work → Board) uses status × execution cells instead of
     the single-lane rows the legacy #board uses. Cards are still draggable but
     a drop performs an exact-status move through the Work endpoint, then
     reloads the matrix (keeping the type filter in the URL). */
  function workBoardRoot(el) {
    return el && el.closest ? el.closest("[data-work-board]") : null;
  }

  function workBoardColumns(root) {
    return (root || document).querySelectorAll("[data-work-board] [data-board-column]");
  }

  function markWorkDropTargets(root, from) {
    var cols = workBoardColumns(root);
    for (var i = 0; i < cols.length; i++) {
      if (cols[i].getAttribute("data-board-column") === from) continue;
      cols[i].setAttribute("data-drop-eligible", "");
    }
  }

  function clearWorkDropTargets() {
    var cols = workBoardColumns();
    for (var i = 0; i < cols.length; i++) {
      cols[i].classList.remove("memory-board__lane--drop");
      cols[i].removeAttribute("data-drop-eligible");
    }
  }

  /* Work matrix lane preferences (Active / Attention / Idle open-closed).
     The server renders the initial open state from this cookie; a toggle
     writes the new state back so it survives reloads and navigation. */
  var WORK_LANES_COOKIE = "memory_work_lanes";

  function workLanePrefs() {
    try {
      var parts = document.cookie ? document.cookie.split("; ") : [];
      for (var i = 0; i < parts.length; i++) {
        var eq = parts[i].indexOf("=");
        if (eq === -1) continue;
        if (parts[i].slice(0, eq) !== WORK_LANES_COOKIE) continue;
        var raw = decodeURIComponent(parts[i].slice(eq + 1));
        var parsed = JSON.parse(raw);
        return parsed && typeof parsed === "object" ? parsed : {};
      }
    } catch (e) {
      return {};
    }
    return {};
  }

  function saveWorkLanePref(key, open) {
    if (!key) return;
    var prefs = workLanePrefs();
    prefs[key] = !!open;
    try {
      document.cookie =
        WORK_LANES_COOKIE + "=" + encodeURIComponent(JSON.stringify(prefs)) +
        "; path=/; max-age=31536000; SameSite=Lax";
    } catch (e) {
      /* preference is best-effort */
    }
  }

  document.addEventListener("toggle", function (ev) {
    var el = ev.target;
    if (!el || !el.matches || !el.matches("[data-work-lane-key]")) return;
    saveWorkLanePref(el.getAttribute("data-work-lane-key"), el.open);
  }, true);

  function submitWorkMove(id, to) {
    if (!id || !to) return;
    fetch(MemoryScope.path("/work/items/" + encodeURIComponent(id) + "/status"), {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ status: to }).toString()
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok) throw new Error(body.error || "Move failed");
        window.location.reload();
      });
    }).catch(function (err) {
      window.alert(err && err.message ? err.message : "Move failed");
    });
  }

  /* boardAllowedStatuses reads the card's own type's accepted statuses from
     data-board-allowed (a JSON array, like the status map). It returns null for a
     card whose type declares no set — an unconstrained type accepts any status,
     so every lane stays a target — and for a malformed payload, which must not be
     read as "no lane is acceptable". */
  function boardAllowedStatuses(card) {
    var raw = card && card.getAttribute("data-board-allowed");
    if (!raw) return null;
    try {
      var parsed = JSON.parse(raw);
      return parsed && parsed.length ? parsed : null;
    } catch (e) {
      return null;
    }
  }

  /* boardLaneAccepts is the single predicate behind the marking, the hover
     highlight and the drop: the move decision for the pair, narrowed to the
     statuses the card's type accepts. All three call it, so a lane cannot be
     painted as a target and then refuse the drop. The narrowing applies to an
     exact-status move only: retry/approve/cancel/request-changes are dedicated
     endpoints that move the item to their own configured status, so they are not
     subject to the type's allowed-status set. */
  function boardLaneAccepts(from, to, sm, allowed) {
    var action = boardMoveAction(from, to, sm);
    if (!action) return "";
    if (action !== "status" || !allowed) return action;
    for (var i = 0; i < allowed.length; i++) {
      if (allowed[i] === to) return action;
    }
    return "";
  }

  /* markBoardDropTargets tags every lane that accepts the held card with
     data-drop-eligible / data-drop-action. CSS renders the tag, and the drop
     handler re-derives the same action from the same predicate. Cleared on
     drop/dragend. */
  function markBoardDropTargets(from, allowed) {
    var sm = boardStatusMap();
    var lanes = boardLanes();
    for (var i = 0; i < lanes.length; i++) {
      var to = lanes[i].getAttribute("data-board-column");
      var action = boardLaneAccepts(from, to, sm, allowed);
      if (!action) {
        lanes[i].removeAttribute("data-drop-eligible");
        lanes[i].removeAttribute("data-drop-action");
        continue;
      }
      lanes[i].setAttribute("data-drop-eligible", "");
      lanes[i].setAttribute("data-drop-action", action);
    }
  }

  function clearBoardDropTargets() {
    var lanes = boardLanes();
    for (var i = 0; i < lanes.length; i++) {
      lanes[i].classList.remove("memory-board__lane--drop");
      lanes[i].removeAttribute("data-drop-eligible");
      lanes[i].removeAttribute("data-drop-action");
    }
  }

  // boardRefreshPending records that a live board frame was dropped because a
  // drag was in flight; dragend replays one refresh so the board cannot stay
  // stale after a cancelled drag.
  var boardRefreshPending = false;

  // Live board frames (htmx hx-sse) morph #board. Morphing the columns mid-drag
  // would move the card being dragged out from under the pointer, so those
  // frames are deferred; the drop's own POST re-renders, and dragend replays a
  // refresh otherwise. Only the #board frame is deferred: the count and health
  // frames cannot move a card, and deferring them would leave those readouts
  // stale whenever no later event happens to arrive.
  //
  // The drag state lives only in boardDrag (the object the drop handlers use).
  // There is deliberately no DOM copy: a flag left set by an aborted drag would
  // silently suppress every later update — the exact "board is not live"
  // failure this change removes.
  document.addEventListener("htmx:sse:before:message", function (ev) {
    if (!boardDrag) return;
    var data = ev.detail && ev.detail.message ? ev.detail.message.data || "" : "";
    if (data.indexOf('hx-target="#board"') === -1) return;
    if (ev.detail) ev.detail.cancelled = true;
    boardRefreshPending = true;
  });

  document.addEventListener("dragstart", function (ev) {
    var card = ev.target.closest("[data-board-card]");
    if (!card) return;
    var id = card.getAttribute("data-canonical-id");
    var root = workBoardRoot(card);
    boardDrag = {
      id: id,
      fromStatus: card.getAttribute("data-board-status"),
      allowed: root ? null : boardAllowedStatuses(card),
      work: !!root,
      root: root
    };
    if (ev.dataTransfer) {
      ev.dataTransfer.setData("text/plain", id || "");
      ev.dataTransfer.effectAllowed = "move";
    }
    card.classList.add("memory-board__card--dragging");
    if (root) markWorkDropTargets(root, boardDrag.fromStatus);
    else markBoardDropTargets(boardDrag.fromStatus, boardDrag.allowed);
  });
  document.addEventListener("dragover", function (ev) {
    var col = ev.target.closest ? ev.target.closest("[data-board-column]") : null;
    if (!col) return;
    if (boardDrag && boardDrag.work) {
      if (boardDrag.root && !boardDrag.root.contains(col)) return;
      var workTo = col.getAttribute("data-board-column");
      if (!workTo || workTo === boardDrag.fromStatus) return;
      ev.preventDefault();
      if (ev.dataTransfer) ev.dataTransfer.dropEffect = "move";
      var workCols = workBoardColumns(boardDrag.root);
      for (var wi = 0; wi < workCols.length; wi++) {
        workCols[wi].classList.remove("memory-board__lane--drop");
      }
      col.classList.add("memory-board__lane--drop");
      return;
    }
    var lanes = boardLanes();
    for (var i = 0; i < lanes.length; i++) lanes[i].classList.remove("memory-board__lane--drop");
    var action = boardDrag && boardLaneAccepts(boardDrag.fromStatus, col.getAttribute("data-board-column"), boardStatusMap(), boardDrag.allowed);
    if (!action) return; // unsupported move: do not accept the drop
    ev.preventDefault();
    if (ev.dataTransfer) ev.dataTransfer.dropEffect = "move";
    col.classList.add("memory-board__lane--drop");
  });
  document.addEventListener("drop", function (ev) {
    var col = ev.target.closest ? ev.target.closest("[data-board-column]") : null;
    if (!col || !boardDrag) return;
    var to = col.getAttribute("data-board-column");
    if (boardDrag.work) {
      if (boardDrag.root && !boardDrag.root.contains(col)) return;
      if (!to || to === boardDrag.fromStatus) return;
      ev.preventDefault();
      var workID = boardDrag.id;
      boardDrag = null;
      clearWorkDropTargets();
      submitWorkMove(workID, to);
      return;
    }
    var action = boardLaneAccepts(boardDrag.fromStatus, to, boardStatusMap(), boardDrag.allowed);
    if (!action) return; // invalid pair: reject the drop, no request
    ev.preventDefault();
    clearBoardDropTargets();
    var id = boardDrag.id || (ev.dataTransfer ? ev.dataTransfer.getData("text/plain") : "");
    boardDrag = null;
    boardRefreshPending = false;
    if (!id || !window.htmx) return;
    if (action === "dialog") {
      // Open the item action dialog (review -> revision needs feedback).
      htmx.ajax("GET", MemoryScope.path("/board/items/" + encodeURIComponent(id)), {
        target: "#board-drawer",
        swap: "innerHTML"
      });
      return;
    }
    if (action === "status") {
      // Exact-status move (custom lanes included): the type validates it.
      htmx.ajax("POST", MemoryScope.path("/board/items/" + encodeURIComponent(id) + "/status"), {
        target: "#board",
        swap: "outerHTML",
        values: { status: to }
      });
      return;
    }
    htmx.ajax("POST", MemoryScope.path("/board/items/" + encodeURIComponent(id) + "/" + action), {
      target: "#board",
      swap: "outerHTML"
    });
  });
  document.addEventListener("dragend", function (ev) {
    clearBoardDropTargets();
    clearWorkDropTargets();
    var card = ev.target.closest ? ev.target.closest("[data-board-card]") : null;
    if (card) card.classList.remove("memory-board__card--dragging");
    boardDrag = null;
    if (boardRefreshPending) {
      boardRefreshPending = false;
      if (window.htmx) {
        htmx.ajax("GET", MemoryScope.path("/board/partial"), { target: "#board", swap: "outerHTML" });
      }
    }
  });

  /* Board card click/keyboard opens the shared object preview drawer (the same
     component chat uses) with the board action slot. The preview is a read-only
     summary plus the status-gated work-item actions. Falls back to the object
     page when the preview client is unavailable. */
  function openBoardCardPreview(card) {
    var id = card.getAttribute("data-canonical-id");
    if (!id) return;
    if (window.MemoryObjectPreview && typeof window.MemoryObjectPreview.open === "function") {
      window.MemoryObjectPreview.open({ id: id, editHref: MemoryScope.path("/objects/" + id), actions: "board" });
      return;
    }
    window.location.assign(MemoryScope.path("/objects/" + encodeURIComponent(id)));
  }

  /* Mobile card "Move": opens the item drawer, which carries the status
     control and the other work-item actions. HTML5 drag never fires on touch. */
  document.addEventListener("click", function (ev) {
    var move = ev.target.closest ? ev.target.closest("[data-board-open-drawer]") : null;
    if (!move) return;
    ev.preventDefault();
    var moveID = move.getAttribute("data-board-open-drawer");
    if (!moveID || !window.htmx) return;
    htmx.ajax("GET", MemoryScope.path("/board/items/" + encodeURIComponent(moveID)), {
      target: "#board-drawer",
      swap: "innerHTML"
    });
  });

  document.addEventListener("click", function (ev) {
    if (ev.defaultPrevented) return;
    if (ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    if (ev.target.closest && ev.target.closest("a, button, input, textarea, select, label, form, [data-board-open-drawer]")) return;
    var card = ev.target.closest ? ev.target.closest("[data-board-open-preview]") : null;
    if (!card) return;
    ev.preventDefault();
    openBoardCardPreview(card);
  });
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Enter" && ev.key !== " ") return;
    var card = ev.target && ev.target.closest ? ev.target.closest("[data-board-open-preview]") : null;
    if (!card || card !== ev.target) return; // only the card itself, not nested controls
    ev.preventDefault();
    openBoardCardPreview(card);
  });

  /* ---------- API-token scope group toggles (delegated) ---------- */
  /* The scope picker renders one bordered block per scope area: a header
     checkbox (data-scope-group-toggle) and one scope row per option
     (data-scope-group-row). Checking the header selects/clears every row in
     that block; checking/unchecking any row keeps the header reflecting
     all/none/partial (indeterminate). Delegated on document so it survives
     HTMX swaps; the server pre-checks the header for a fully-selected group,
     and syncScopeGroups re-derives indeterminate on load and after swaps. */
  function syncScopeGroupToggle(group) {
    if (!group) return;
    var toggle = group.querySelector("[data-scope-group-toggle]");
    if (!toggle) return;
    var boxes = group.querySelectorAll("[data-scope-group-row]");
    var checked = 0;
    for (var i = 0; i < boxes.length; i++) if (boxes[i].checked) checked++;
    toggle.checked = boxes.length > 0 && checked === boxes.length;
    toggle.indeterminate = checked > 0 && checked < boxes.length;
  }

  function syncScopeGroups(root) {
    var scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll("[data-scope-group]").forEach(syncScopeGroupToggle);
  }

  document.addEventListener("change", function (ev) {
    var t = ev.target;
    if (!t || !t.matches) return;
    if (t.matches("[data-scope-group-toggle]")) {
      var group = t.closest("[data-scope-group]");
      if (!group) return;
      var boxes = group.querySelectorAll("[data-scope-group-row]");
      for (var i = 0; i < boxes.length; i++) boxes[i].checked = t.checked;
      t.indeterminate = false;
      return;
    }
    if (t.matches("[data-scope-group-row]")) syncScopeGroupToggle(t.closest("[data-scope-group]"));
  });

  document.addEventListener("DOMContentLoaded", function () { syncScopeGroups(document); });
  document.addEventListener("htmx:after:swap", function (ev) { syncScopeGroups(ev.target); });

  /* expose for templ script blocks */
  window.MemoryApp = {
    openAgentForm: openAgentForm,
    openDeleteConfirm: openDeleteConfirm,
    openDialog: openDialog,
    openDialogByID: openDialogByID,
    openAutoOpenDialogs: openAutoOpenDialogs,
    toast: toast,
    alert: alertModal,
  };

  /*
   * templ row-dialog handlers emit inline onclick handlers via
   * templ.JSFuncCall("openDialogByID", ...), which resolves the name against
   * window, not this IIFE's scope. app.js is loaded on every page, so this one
   * global is the single implementation; no per-page dialog-open script
   * component is needed and none was added.
   */
  window.openDialogByID = openDialogByID;
})();
