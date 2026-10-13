## Context

The board is a projection of board-enabled graph objects (`Task`) whose status is
advanced by agents. Transitions therefore happen *outside* the browser request
that triggered them, which is the root of the staleness complaint. The gateway
already owns two reusable seams: a verbatim SSE proxy of the server's project
event stream, and a shared object-preview drawer used by both chat and board.

## Decisions

### Refresh is event-driven, not polled

The board subscribes to the existing project event stream rather than polling
`/board/partial` on a timer.

- The stream already exists and is already used by the inbox client, so this
  adds no server surface.
- The server emits `entity.created|updated|deleted` for `graph_object` (the work
  item) and `agent_run`, which is exactly the change signal the board needs.
- Polling would re-render `#board` on a fixed cadence regardless of change and
  would fight native drag-and-drop for no benefit.

Alternatives considered: HTMX `hx-trigger="every 5s"` polling (simpler, but
constant traffic and mid-drag swaps) — rejected.

### Refresh must not fight the drag interaction

A refresh re-renders `#board` via an `outerHTML` swap. A native HTML5 drag in
flight would be broken by that, so the client suppresses refresh while a drag is
active and while the tab is hidden. The drag state is exposed on the DOM
(`data-board-dragging`) rather than shared through a module closure, because
`app.js` and `board.js` are separate classic scripts.

### The drawer, not the board, owns refresh-safety

The preview drawer lives outside `#board` (`#object-preview-root`), so a board
swap never tears it down. The legacy OOB-cleared `#board-drawer` is unaffected
by this change.

### `ContainerFull` is an upstream variant, not a local wrapper

The width cap comes from `layout.Container`, and `ContainerSize` has only
`ContainerMD`/`ContainerLG`. A local full-bleed wrapper would fork the page shell
convention, so the variant is added upstream (`max-w-none`) and consumed from
there. Because the width is applied through `string(size)` (a runtime value),
Tailwind cannot see the literal, so the gateway declares `max-w-none` in its
`@source inline(...)` candidate safelist — the same mechanism already used for
runtime-built disclosure classes.

### Change history is lazy

The history section is a separate request fired when the drawer opens
(`hx-trigger="load"`), not part of the initial preview render. Opening a preview
is a hot path (chat references and board cards both use it) and the version
chain can grow without bound; a lazy section keeps the initial render one
request and lets the timeline fail independently.

### History is shared, not board-only

The drawer is shared between chat and board previews. Version history is a
property of the object, not of the board, so the section renders in both
contexts.

### Status labels are humanised at the render seam

The schema stores statuses as bare ids (`allowedStatuses: [ready, in_progress,
…]`) with no label field, so the only consistent presentation is the existing
humanisation used by the schema browser and board lanes. The raw value is kept
in attributes for machine fidelity and tests.

### Board runs are identified by their subject, not their trigger source

Board runs are created by the reconciler with `SubjectObjectID`/`SubjectObjectType`
set; there is no dedicated trigger-source value for them. A project-wide listing
of runs with a non-null `subject_object_id` is therefore the exact board-origin
set. Work-item context (key/type) is joined once in the gateway from the existing
`ListWorkItems` projection rather than per-run, to avoid an N+1.

## Risks / Trade-offs

- **SSE connection count**: the board opens one `EventSource` on top of whatever
  the inbox opens. Browsers cap concurrent connections per origin (~6); the board
  keeps exactly one and closes it on unload.
- **Event-name coupling**: the client matches on `entity.*` frame names and the
  `entity` field of the payload. This is the documented server contract
  (`domain/events`), and the refresh is debounced, so an unexpected extra frame
  only costs one redundant fetch.
- **Cross-repo ordering**: the go-daisy variant must merge before the gateway can
  bump its pin, so the layout task lands last in this change's branch.
- **New public endpoint**: the board-runs listing is a new server route; it
  follows the existing project-member + `agents:read` gating of the work-items
  group and needs a swagger block.

## Open Questions

- Whether the `AgentRunDTO.subjectObjectId` field should be populated for every
  run or only by the new listing. Decision: populate it in `ToDTO` (it is part of
  the run), so other surfaces can use it later.
