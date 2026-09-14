/* Memory web UI — app-level client.
   Handles: agent create/edit/delete dialogs (JSON fetch to /api/agents),
   toast notifications, and the sessions list helpers.
   Event delegation on document so bindings survive HTMX sidebar swaps. */
(function () {
  "use strict";

  /* ---------- sentry ---------- */
  /* Report an error to Sentry when the browser SDK is active (gated on
     window.Sentry, present only when the loader was injected because a
     Sentry DSN is configured). No-op otherwise. */
  function captureError(err) {
    if (window.Sentry && err) window.Sentry.captureException(err);
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

  /* ---------- toast ---------- */
  /* Push a toast into the Alpine toast queue (rendered by toastQueue in
     gateway/toast.templ). Alpine owns the queue, dismissal timer, countdown
     progress bar, and hover-to-pause. */
  function toast(kind, message) {
    var container = document.getElementById("toast-container");
    if (!container || !window.Alpine) return;
    var queue = window.Alpine.$data(container);
    if (queue && queue.add) queue.add({ type: kind, message: message, duration: 4200 });
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

  /* open with an agent id, or "" for create */
  function openAgentForm(id) {
    var d = formDialog();
    if (!d) return;
    var form = el("agent-form");
    form.reset();
    resetDelegationTargets();
    resetSkills();
    el("agent-id").value = id || "";
    el("agent-form-title").textContent = id ? "Edit agent" : "New agent";
    el("agent-form-subtitle").textContent = id
      ? "Adjust the persona, brain, or tools — Memory applies it live."
      : "Define a persona, a brain, and its tools.";
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
          el("agent-prompt").value = a.systemPrompt || "";
          setModelValue((a.model && a.model.name) || "");
          el("agent-temperature").value = a.model && a.model.temperature != null ? a.model.temperature : "";
          el("agent-max-tokens").value = a.model && a.model.maxTokens ? a.model.maxTokens : "";
          el("agent-tools").value = Array.isArray(a.tools) ? a.tools.join(", ") : "";
          hydrateDelegation(a);
          hydrateSkills(a);
        })
        .catch(function (err) {
          captureError(err);
          toast("error", "Could not load agent: " + err.message);
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

  /* clear the skill picker for a fresh open: uncheck every skill so an
     earlier edit's selections don't leak into create. */
  function resetSkills() {
    var cbs = document.querySelectorAll('input[name="skill"]');
    for (var i = 0; i < cbs.length; i++) cbs[i].checked = false;
    var emptyNote = el("agent-skills-empty");
    if (emptyNote) emptyNote.classList.toggle("hidden", cbs.length > 0);
  }

  /* hydrate the skill checkboxes from the agent def (a.skills = names) */
  function hydrateSkills(a) {
    var skills = Array.isArray(a.skills) ? a.skills : [];
    var cbs = document.querySelectorAll('input[name="skill"]');
    for (var i = 0; i < cbs.length; i++) {
      cbs[i].checked = skills.indexOf(cbs[i].value) !== -1;
    }
    var emptyNote = el("agent-skills-empty");
    if (emptyNote) emptyNote.classList.toggle("hidden", cbs.length > 0);
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
      systemPrompt: el("agent-prompt").value,
      tools: splitList(el("agent-tools").value),
    };
    // Always send skills (even an empty array) so clearing a skill on edit
    // actually reaches memory instead of being omitted.
    var skillCbs = document.querySelectorAll('input[name="skill"]:checked');
    var skills = [];
    for (var i = 0; i < skillCbs.length; i++) skills.push(skillCbs[i].value);
    body.skills = skills;
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
          if (!r.ok) throw new Error((j && (j.error || j.message)) || "HTTP " + r.status);
          return j;
        });
      })
      .then(function () {
        stashToast("success", id ? "Agent updated" : "Agent created");
        window.location.reload();
      })
      .catch(function (err) {
        setFormBusy(false);
        captureError(err);
        toast("error", "Save failed: " + err.message);
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
            throw new Error((j && (j.error || j.message)) || "HTTP " + r.status);
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
        captureError(err);
        toast("error", "Delete failed: " + err.message);
      });
  }

  /* ---------- shared helpers ---------- */
  function splitList(s) {
    return (s || "")
      .split(/[\n,]+/)
      .map(function (x) { return x.trim(); })
      .filter(Boolean);
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

  /* ---------- auto-grow textareas ---------- */
  /* Any textarea with data-autogrow starts single-line and grows to fit its
     content, capped by its CSS max-height (overflow scrolls past the cap).
     Mirrors the chat composer's auto-grow for free-form string fields (e.g.
     object properties that can hold long text). Delegated on document so it
     survives HTMX swaps. */
  function autogrow(el) {
    if (!el) return;
    el.style.height = "auto";
    var max = 0;
    try { max = parseInt(getComputedStyle(el).maxHeight, 10) || 0; } catch (e) {}
    var h = el.scrollHeight;
    if (max > 0 && h > max) h = max;
    el.style.height = h + "px";
  }

  function autogrowAll(root) {
    (root || document).querySelectorAll("textarea[data-autogrow]").forEach(autogrow);
  }

  document.addEventListener("input", function (ev) {
    var t = ev.target;
    if (t && t.matches && t.matches("textarea[data-autogrow]")) autogrow(t);
  });

  document.addEventListener("DOMContentLoaded", function () { autogrowAll(); });

  /* ---------- schema editor + derive dialogs (delegated) ---------- */
  /* Native <dialog> open/close driven by data attributes, so the markup needs
     no inline handlers and the behaviour survives HTMX swaps (the listeners
     live on document). */
  document.addEventListener("click", function (ev) {
    var opener = ev.target.closest("[data-dialog-open]");
    if (opener) {
      ev.preventDefault();
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

  /* expose for templ script blocks */
  window.MemoryApp = {
    openAgentForm: openAgentForm,
    openDeleteConfirm: openDeleteConfirm,
    toast: toast,
  };
})();
