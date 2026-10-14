## Why

Two tightly-coupled gaps in the object-driven work path:

1. **Work-path transitions never write `change_summary` (#1850).** The general
   update path builds a `change_summary` for every version, but
   `transitionWorkObject` — the single writer that produces a new version for
   every claim / complete / block / approve / request-changes / reassign /
   set-status — constructs its new version without one. The board history
   therefore depends entirely on the gateway's *derived* fallback (shipped in
   #1827) for exactly the events users care about most: who moved the item and
   when. Anything not derivable (e.g. why) is unavailable, and the derived
   status/assignee diff is inferred rather than recorded.
2. **The status vocabulary has one unguarded generic door (#1851).** A
   board-enabled type declares its allowed work statuses, and the single-object
   generic doors (create, create-or-update, patch) and the bulk status update
   reject a status outside that set. The subgraph/bulk create door
   (`POST /api/graph/subgraph`) did **not**: an API client could create a
   board-enabled object in a status the board deliberately does not offer, and an
   agent could set a work status there too. Two entry points disagreed about what
   a valid status is.

## What Changes

- **Record the work-path change summary.** `transitionWorkObject` now builds the
  new version's `change_summary` with the same RFC 6901 shape the general path
  uses (reusing `computeChangeSummary`): the status move is recorded as an
  updated `/status` (previous → new), and the assignee move — a built-in column,
  not a property — is appended as a synthetic `/assignee` path. A transition that
  changes neither records no summary. Because the board history prefers a
  recorded summary over its derived fallback, the recorded summary must be
  complete, so a transition that moves both the status and the assignee (the
  unassign path) records both and can never hide the assignee change.
- **Close the subgraph status-vocabulary door.** `CreateSubgraph` now applies the
  same per-type work-status guard as the single-object create path — the declared
  allowed-status vocabulary (`validateTypeStatus`) and the agent-write rule
  (`rejectAgentWorkStatusWrite`) — for every object in the batch, before the
  transaction, so an off-vocabulary or agent status write rejects the whole
  request atomically with no partial state.

## Non-goals

- No change to the work-path validation semantics of the exact-status endpoint
  (`POST /api/projects/:projectId/work-items/:canonicalId/status`), which already
  validates against the declared vocabulary.
- No gateway change: `object_preview.go` already prefers a recorded
  `change_summary` and still derives one for versions that have none.
