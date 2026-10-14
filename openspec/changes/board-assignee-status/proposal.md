## Why

The board exposes human work actions (approve, request-changes, retry, reassign,
cancel), but two operator flows are missing:

1. **No way to set an item to an exact status.** The human can approve
   (review→done), retry (blocked→ready), or cancel (→blocked), but cannot move an
   item to an arbitrary phase — e.g. push a stuck item back to `in_progress`, or
   mark it `blocked` without cancelling its run. The work-path already validates
   and transitions statuses internally; only the endpoint and UI affordance are
   missing.
2. **Assignees are typed free-form.** The drawer's reassign control is a plain
   text input, so an operator can silently typo an assignee and route the item to
   a non-existent agent (or a different agent than intended). The board knows the
   project's real assignees — its runtime agents and its members — but does not
   offer them.

## What Changes

- **Server — exact-status endpoint.** A new `POST
  /api/projects/:projectId/work-items/:canonicalId/status` (body `{status}`)
  reuses the existing work-path validation and transition machinery to set a work
  item to an exact status. Empty status → 400; a disallowed status → 400/403
  (reusing the existing `validateTypeStatus` semantics). Setting the review
  status marks the item `needs_review`; any other target clears it (review
  attribution is left untouched). The transition emits `status_changed` (waking
  matching listeners) but does **not** enqueue a run. The existing agent-write
  guard still applies: an agent-originated write is rejected.

- **Gateway — ticket-style assignee picker.** The drawer's free-text reassign
  input becomes a dropdown listing `Unassigned`, the project's runtime agents
  (by exact name), and its members (by display name, falling back to email).
  Selecting an option posts the exact value to the existing reassign route.

- **Gateway — set-status control.** The drawer's metadata row gains a "Set
  status" dropdown listing the work-path vocabulary minus the item's current
  status; selecting an option posts to the new set-status route.

## Capabilities

### Modified Capabilities

- `object-driven-agent-work`: work items are settable to an exact status, and the
  board's assignee/status controls become constrained dropdowns.
