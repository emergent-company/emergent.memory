## Why

Two points of feedback on the agent settings surface (#1276, #1274):

- The General settings page (#1276) carries an explicit **Save changes** button,
  but every field there is safe to persist as the user edits. The button is an
  unnecessary step and an easy way to lose an edit by navigating away.
- The Tools settings page (#1274) shows each policy select's inherited value
  (`Inherit (<value>)`) but that indication is server-rendered once: changing the
  default approval or a capability group's policy does not update the rest of the
  list, so the displayed inheritance no longer matches what a save would store.
  Separately, the group policy select lives inside the disclosure's `<summary>`,
  so choosing an option also runs the summary's activation behaviour and folds /
  unfolds the group.

## What Changes

- The agent **General** settings form auto-saves: text fields after a short
  debounce (~700 ms) once typing stops, select/picker/listbox changes promptly.
  Unchanged values never post. A subtle status node reports saving / saved /
  error, a failed save keeps the in-progress values with a retry affordance, and
  a pending debounce is flushed before an in-app navigation and cancelled on
  unload. The form keeps its PRG POST route as the no-JS fallback; the explicit
  save button is dropped on this page only (the shared footer stays on the other
  settings subpages).
- A new JSON endpoint `POST /agents/:id/settings/general/autosave` applies the
  General section and reports `200 {"ok":true}` or `422 {"ok":false,"error":...}`
  instead of redirecting, so the page can surface validation errors inline.
- The Tools picker recomputes the `Inherit (<value>)` label on every policy
  select client-side when the default policy or a group policy changes, using the
  same resolution order as the server (explicit tool → owning group → default).
- A click on a group policy select inside a disclosure `<summary>` no longer
  cancels into a fold/unfold: the summary activation is cancelled for that
  control without affecting the select's own behaviour.

## Capabilities

### Modified Capabilities

- `web-agent-settings`: the General form auto-saves via a debounced client and a
  JSON endpoint; the explicit save button is removed from that form.
- `agent-dashboard-ui`: the Tools picker's inheritance indicators recompute live
  and selecting a group policy no longer collapses the group.

## Impact

- **Gateway** (`apps/web-ui/gateway`): new `webui/static/js/agent-settings.js`
  (loaded from `ui.templ`), `agent.templ` (General autosave wiring + status node,
  inherit-option marker, default-select marker), `agent.go`
  (`uiAgentAutosaveGeneral`), `main.go` (route).
- **Tests**: Go render/handler tests, a new hermetic js-dom Playwright spec
  (`specs/js/agent-settings-wiring.spec.ts`), and the live General-edit e2e spec
  updated to assert auto-save instead of the removed button.
- No server/domain change; no schema or API contract change beyond the new
  gateway route.
