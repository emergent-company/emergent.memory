## Why

The object preview's History section lists the object's versions newest-first — each
with its version marker, status, and creation time — but an entry is completely inert:
there is no way to see *what* changed in a given version. The server already records a
per-version `change_summary` (added/removed/updated property paths, plus merge
provenance) and returns it on `GET /api/graph/objects/{id}/history`, but the gateway's
`GraphObject` drops the field on decode. Most board-work transitions (claim, complete,
approve, request-changes, reassign, cancel) record no summary at all, so the only way
to make those versions meaningful is to derive their status/assignee/property deltas
from the preceding version — which the timeline already has in hand.

## What Changes

- **Gateway client** (`memory_graph.go`): `GraphObject` gains `ChangeSummary`
  (`json:"change_summary"`) and `Assignee` (`json:"assignee"`) so the server's
  per-version data survives decoding.
- **History timeline** (`object_preview.templ` + `object_preview.go`): each entry
  becomes a collapsed, native `<details>/<summary>` disclosure (keyboard operable, no
  JS). Expanding an entry reveals that version's change list:
  - a recorded `change_summary` renders `added` as `+ <path>`, `removed` as
    `− <path>`, and `updated` as `<path>: <from> → <to>` (values JSON-rendered
    compactly and truncated), plus merge provenance (`source`/`strategy`/
    `enriched_keys`) as muted lines;
  - when no summary is recorded, the change list is derived from the preceding
    version: `status` and `assignee` as `from → to`, and top-level property diffs
    (scalar `from → to`, `changed` for nested/complex, `+`/`−` for added/removed
    paths);
  - when nothing is derivable, a muted "No recorded changes for this version." note.
- Unknown/unexpected `change_summary` shapes are tolerated — the renderer never panics
  or blanks an entry.

## Capabilities

### New Capabilities

- `web-object-preview-history`: a new requirement — "Per-version change disclosure in
  the object preview history" — adds expandable, per-version change disclosure to the
  history timeline (recorded `change_summary`, a derived status/assignee/property
  fallback, and a no-changes note).

## Impact

- **Gateway** (`apps/web-ui/gateway/`): `memory_graph.go`, `object_preview.go`,
  `object_preview.templ`, and their tests. No server write path is touched — the
  absence of `change_summary` on work-path transitions is a known, separately-filed
  gap, addressed here only by the derived fallback.
- **E2E**: a hermetic DOM-level Playwright spec (`specs/js/…`) asserting the disclosure
  expands and is keyboard operable.
