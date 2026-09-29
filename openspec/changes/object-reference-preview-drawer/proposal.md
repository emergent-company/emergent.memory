## Why

Chat answers link to knowledge-graph objects, and clicking a reference currently navigates straight to the object **edit** view. That is a heavy, editing-oriented destination for what is usually a quick "what is this?" glance: it leaves the conversation, loads the full edit form, and invites accidental edits. Users want a compact read-only summary without leaving chat, with an explicit way to jump to editing only when they mean to.

## What Changes

- Clicking an inline object reference in a chat message (and a chat sources-list entry) opens a **read-only preview drawer** from the right instead of navigating to the edit view.
- The drawer shows the object's **icon and type**, its **display label**, its **properties as read-only label/value pairs**, and key metadata — never form fields. Empty values are de-emphasised; hiding/folding them is explicitly deferred.
- The drawer header exposes an **edit action** that jumps to the existing `/objects/<id>` edit view.
- Modified clicks (cmd/ctrl/shift/alt, middle button, `target`/`download`) keep **native navigation**; `/objects/` links rendered outside chat content are unaffected.
- A new gateway partial route `GET /objects/<id>/preview` serves the drawer body, reusing the existing object/schema fetch seam.
- The drawer is a **global slide-over** (lives outside `#main-content`, so it survives HTMX in-app navigation) with backdrop/Escape/close dismissal, structured to accept future actions (comments, etc.).

## Capabilities

### New Capabilities

- `web-object-reference-preview`: read-only, in-context preview of object references from chat, with a jump-to-edit action.

### Modified Capabilities

<!-- none -->

## Impact

- **UI** (`apps/web-ui/gateway/`): new `object_preview.templ` (drawer shell + read-only summary) and `object_preview.go` (partial handler); new `webui/static/js/object-preview.js` (open/close, focus handling, scoped click interception); mounted in `ui.templ` (appShell, beside `sidePanel`); route registered in `main.go`.
- **No server / memory-service change**: reuses `GET /api/graph/objects/:id`, `GET /api/graph/objects/:id/edges`, and the compiled-types endpoint. The server-side citation neutralization (`markdown.go`) is unchanged.
- **Spec**: new capability `web-object-reference-preview`; legacy design note `docs/spec/36-chat-object-references.md` updated to describe the preview behaviour.
- **Non-goals**: commenting or acting on objects; hide/fold of empty fields; changes to the citation parser or the object edit view.
- **Testing**: gateway unit tests for the partial (read-only, no form controls, formatting, not-found, wiring); build + lint; browser smoke test.
