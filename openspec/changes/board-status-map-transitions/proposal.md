## Why

Board drag actions (`webui/static/js/app.js`) and the preview drawer's
work-item actions (`board.templ` `boardDrawerActions`) hardcoded the literal
statuses `blocked`/`ready`/`review`/`revision`/`done`. A project whose agents
map the work path to custom statuses (`workConfig.status`, e.g. `todo`/`doing`)
renders custom lanes — `boardStatusesFromCompiled` already derives those from the
schema's `allowedStatuses` — but then exposes no drag targets or drawer actions,
because the literal names never match the item statuses. Follow-up to
#1379/#1421 (#1428).

## What Changes

- Read the project's work-path status mapping from its agent definitions'
  `workConfig.status` (the status value each lifecycle phase maps to) and emit it
  into `#board[data-board-status-map]`.
- Derive the board's drag transitions in `app.js` from that map instead of the
  literal statuses, falling back to the built-in statuses when the map is absent
  or malformed.
- Gate the preview drawer's and the item dialog's work-item actions
  (approve / request-changes / retry / reassign / cancel) on the mapped phases
  server-side, so a custom board exposes the same actions as the default one.
- A phase is only remapped when every agent declaring it agrees on a single
  value; otherwise that phase keeps its built-in default. An unconfigured project
  therefore behaves exactly as before.

## Capabilities

### Modified Capabilities

- `object-driven-agent-work`: the Kanban projection derives its draggable
  transitions and its drawer actions from the project's work-path status mapping
  rather than the built-in status literals.

## Impact

- **UI** (`apps/web-ui/gateway/`): `memory.go` (agent work-config types +
  `WorkConfig` on the definitions summary), `board.go` (`boardStatusMap`
  derivation + `#board` data attribute), `board.templ` (mapped action gating),
  `object_preview.go` (mapped preview actions), `webui/static/js/app.js`
  (map-driven drag transitions).
- **Tests**: gateway unit tests for the derivation + custom-mapped drawer
  actions; hermetic js-dom specs for the custom-mapped drag wiring.
- **No server / memory-service change**: the mapping is read from the existing
  agent-definitions list response (`workConfig.status`).
- **Known limitation**: the mapping is project-wide (a union of the agents'
  declared phases). Per-object-type mappings are out of scope; a phase declared
  inconsistently by two agents falls back to the built-in status.
