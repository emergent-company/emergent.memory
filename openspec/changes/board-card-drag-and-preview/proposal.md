## Why

The `/board` Kanban page has two reported defects (#1379):

1. **Cards cannot be dragged between columns.** `app.js` cancelled `dragstart` for
   every card whose `data-board-status` was not `blocked`, so only blocked cards
   were draggable at all, and the drop target was hardcoded to the `ready` lane.
   The intended board behaviour (object-driven-agent-work design: "drag sets the
   object status and enqueues when entering the ready lane") was never reachable
   for the other lanes.
2. **Clicking a card opens the board's own action dialog**, not the read-only
   object preview the chat surface already uses, so operational work items had no
   "what is this?" glance consistent with the rest of the app.

## What Changes

- **All board cards become draggable.** A drop maps the `(fromStatus → target
  lane)` pair to the **existing board action** for that work-path transition:
  `blocked → ready` = retry/execute, `review → done` = approve, any non-`done →
  blocked` = cancel. `review → revision` needs feedback a drag cannot supply, so
  it opens the existing item action dialog. Unsupported pairs are rejected in the
  UI (visual reject, no request). No new endpoint: the backend has no generic
  set-status route and the spec requires status changes to go through the work
  path.
- **A valid lane shows a drop affordance** (primary outline) while the held card
  dims; state-only CSS, reduced-motion safe.
- **Clicking (or Enter/Space on) a card opens the shared read-only object preview
  drawer** (`#object-preview-panel`, `object-preview.js`) on the object's
  canonical id, instead of the board's native dialog. The work item's
  status-gated actions (approve / request-changes / retry / reassign / cancel)
  render into the preview's existing `#object-preview-actions-slot` via a new
  `?actions=board` selector, so the actions stay reachable and no parallel
  preview or action component is created. Chat previews carry no selector and
  remain read-only.
- The board's `/board/items/:id` dialog route and `BoardDrawer` templ remain,
  re-pointed to the `review → revision` feedback path rather than removed.

## Capabilities

### Modified Capabilities

- `object-driven-agent-work`: the Kanban projection's drag contract is widened
  from "ready lane only" to the board's supported work-path transitions, and the
  card click opens the shared object preview with its actions.

## Impact

- **UI** (`apps/web-ui/gateway/`): `webui/static/js/app.js` (drag mapping +
  preview open), `webui/static/js/object-preview.js` (`actions` selector +
  close-after-action), `webui/css/app.css` (drop affordance), `board.templ`
  (card attrs), `object_preview.templ`/`object_preview.go` (actions slot seam).
- **Tests**: gateway unit tests for the card/preview contract; a hermetic js-dom
  Playwright spec for the drag mapping and click-to-preview wiring.
- **No server / memory-service change.**
- **Known limitation**: because the action model has no arbitrary status move, a
  `ready` card can still only be dragged to `blocked`; a card whose move has no
  work-path action is not draggable to that lane.
