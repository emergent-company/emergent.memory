// Google Picker folder selection for a Drive data source.
//
// The picker runs in a daisyUI <dialog> on the source detail page. The
// short-lived OAuth token, developer key, and Cloud project id are fetched from
// the gateway each time the picker opens, so nothing is embedded in the page
// HTML and the token is minted from the source's own refresh token (the picker
// therefore browses exactly the account the source syncs).
//
// Only folder ids travel to the server on save: names and shared-drive ids are
// resolved server-side against the source's credentials.
(function () {
  "use strict";

  var dialog = document.getElementById("ds-folder-picker");
  if (!dialog) return;

  var list = document.getElementById("ds-folder-picker-selected");
  var errorBox = document.getElementById("ds-folder-picker-error");
  var countEl = dialog.querySelector('[data-testid="ds-folder-picker-count"]');
  var openBtn = dialog.querySelector("[data-ds-picker-open]");
  var saveBtn = dialog.querySelector("[data-ds-picker-save]");
  var sessionURL = dialog.getAttribute("data-picker-session-url");
  var saveURL = dialog.getAttribute("data-save-url");

  // folderID -> { id, name, driveId }
  var selected = new Map();

  function seed() {
    selected.clear();
    Array.prototype.forEach.call(list.querySelectorAll("[data-folder-id]"), function (li) {
      var id = li.getAttribute("data-folder-id");
      if (!id) return;
      selected.set(id, {
        id: id,
        name: li.getAttribute("data-folder-name") || id,
        driveId: li.getAttribute("data-drive-id") || ""
      });
    });
    render();
  }

  function chip(folder) {
    var li = document.createElement("li");
    li.className = "inline-flex items-center gap-2 rounded-box border border-base-content/10 px-3 py-1.5";
    li.setAttribute("data-folder-id", folder.id);

    var name = document.createElement("span");
    name.className = "text-sm font-medium";
    name.textContent = folder.name;
    li.appendChild(name);

    var remove = document.createElement("button");
    remove.type = "button";
    remove.className = "btn btn-ghost btn-xs";
    remove.setAttribute("aria-label", "Remove " + folder.name);
    remove.textContent = "×";
    remove.addEventListener("click", function () {
      selected.delete(folder.id);
      render();
    });
    li.appendChild(remove);
    return li;
  }

  function render() {
    list.innerHTML = "";
    selected.forEach(function (folder) {
      list.appendChild(chip(folder));
    });
    if (countEl) countEl.textContent = selected.size + " selected";
    if (saveBtn) saveBtn.disabled = selected.size === 0;
    if (openBtn) {
      openBtn.textContent = selected.size === 0 ? "Open Google Picker" : "Add more folders";
    }
  }

  function showError(message) {
    if (!errorBox) return;
    if (!message) {
      errorBox.textContent = "";
      errorBox.classList.add("hidden");
      return;
    }
    errorBox.textContent = message;
    errorBox.classList.remove("hidden");
  }

  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var existing = document.querySelector('script[src="' + src + '"]');
      if (existing) {
        if (existing.getAttribute("data-loaded") === "true") {
          resolve();
          return;
        }
        existing.addEventListener("load", function () { resolve(); });
        existing.addEventListener("error", function () { reject(new Error("Could not load " + src)); });
        return;
      }
      var script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.onload = function () {
        script.setAttribute("data-loaded", "true");
        resolve();
      };
      script.onerror = function () { reject(new Error("Could not load " + src)); };
      document.head.appendChild(script);
    });
  }

  function loadPicker() {
    if (window.google && window.google.picker) return Promise.resolve();
    return loadScript("https://apis.google.com/js/api.js").then(function () {
      return new Promise(function (resolve, reject) {
        if (!window.gapi) {
          reject(new Error("The Google API loader did not load."));
          return;
        }
        window.gapi.load("picker", {
          callback: resolve,
          onerror: function () { reject(new Error("Could not load the Google Picker.")); }
        });
      });
    });
  }

  function fetchSession() {
    return fetch(sessionURL, {
      credentials: "same-origin",
      headers: { Accept: "application/json" }
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) {
          throw new Error(body.error || "Could not start the folder picker.");
        }
        return body;
      });
    });
  }

  function onPicked(data) {
    var picker = window.google && window.google.picker;
    if (!picker || !data) return;
    var action = data[picker.Response.ACTION];
    if (action === picker.Action.ERROR) {
      // Google reports a bad developer key, app id, origin, or an API it cannot
      // reach here; without this branch the dialog just looks empty.
      showError(
        "Google Picker could not load your Drive folders. Check Settings → Credentials: " +
          "the developer key must be an API key from a Cloud project with the Google Picker API " +
          "enabled and this site allowed as an origin, and the app ID must be that project's " +
          "numeric number."
      );
      return;
    }
    if (action !== picker.Action.PICKED) return;
    var docs = data[picker.Response.DOCUMENTS] || [];
    docs.forEach(function (doc) {
      if (!doc) return;
      var id = doc[picker.Document.ID];
      if (!id) return;
      selected.set(id, {
        id: id,
        name: doc[picker.Document.NAME] || id,
        driveId: ""
      });
    });
    // Multi-select is enabled, but accumulating across openings keeps the flow
    // usable if a Google account only returns one selection per dialog.
    render();
  }

  function openPicker() {
    showError("");
    if (openBtn) openBtn.disabled = true;
    fetchSession()
      .then(function (session) {
        return loadPicker().then(function () { return session; });
      })
      .then(function (session) {
        var picker = window.google.picker;
        var view = new picker.DocsView(picker.ViewId.FOLDERS);
        view.setIncludeFolders(true);
        view.setSelectFolderEnabled(true);
        view.setEnableDrives(true);

        var builder = new picker.PickerBuilder()
          .setOAuthToken(session.accessToken)
          .setDeveloperKey(session.developerKey)
          .addView(view)
          .setCallback(onPicked);
        if (session.appId) builder.setAppId(session.appId);
        // The picker verifies the origin of the page that mounted it, not the
        // server's landing host: preview deployments serve the UI from
        // pr-<n>.<base>, which never matches a landing URL configured for the
        // long-lived host. Bind to the browser origin, falling back to the
        // server value only when the origin is unavailable (file:// pages).
        builder.setOrigin(window.location.origin || session.origin);
        builder.enableFeature(picker.Feature.MULTISELECT_ENABLED);
        builder.build().setVisible(true);
      })
      .catch(function (err) {
        showError(err && err.message ? err.message : "Could not open the Google Picker.");
      })
      .then(function () {
        if (openBtn) openBtn.disabled = false;
      });
  }

  function save() {
    showError("");
    if (selected.size === 0) {
      showError("Choose at least one folder.");
      return;
    }
    if (saveBtn) saveBtn.disabled = true;
    fetch(saveURL, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ folderIds: Array.from(selected.keys()) })
    })
      .then(function (response) {
        if (response.status === 204) {
          window.location.assign(window.location.pathname + "?updated=1");
          return;
        }
        return response.json().catch(function () { return {}; }).then(function (body) {
          throw new Error(body.error || "Could not save the folders.");
        });
      })
      .catch(function (err) {
        showError(err && err.message ? err.message : "Could not save the folders.");
        if (saveBtn) saveBtn.disabled = false;
      });
  }

  function openModal() {
    seed();
    showError("");
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "open");
    }
  }

  document.addEventListener("click", function (event) {
    if (!(event.target instanceof Element)) return;
    var trigger = event.target.closest("[data-ds-open-folder-picker]");
    if (!trigger) return;
    event.preventDefault();
    openModal();
  });

  if (openBtn) openBtn.addEventListener("click", openPicker);
  if (saveBtn) saveBtn.addEventListener("click", save);

  seed();

  // /data-sources/:id/resources and older "choose a folder" links land here with
  // ?picker=1; open the modal so the flow continues where it left off.
  if (new URLSearchParams(window.location.search).get("picker") === "1") {
    openModal();
  }
})();
