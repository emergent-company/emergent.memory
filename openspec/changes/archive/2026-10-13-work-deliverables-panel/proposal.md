## Why

An agent that finishes a board task reports `work_complete(summary, artifacts, deliverables)`, but the UI never surfaces that report. After a run completes, an operator cannot see what the agent produced — the card drawer (`BoardDrawer`) shows identity, feedback, and a run list only. The declared deliverables and the completion note live in the run summary but are invisible.

## What Changes

- The card drawer renders the completed run's **declared deliverables** with a per-item resolution state: a deliverable whose `(type, key)` resolves to an existing object becomes a link to that object's detail page; a declared-but-not-found deliverable renders muted (never silently dropped).
- The drawer renders the newest run's **completion note** (`summary`) as sanitized markdown.
- A run that declared no deliverables shows an explicit empty state.
- Resolution is best-effort presentation: a lookup miss or failure degrades that row to `missing` and never blocks or fails the drawer.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `object-driven-agent-work`: the card drawer surfaces a completed run's declared deliverables (with resolution state) and its completion note.

## Impact

- **Gateway only** (`apps/web-ui/gateway/`): a new `LookupGraphObjectByKey` client method (best-effort object resolution by `(type, key)`), pure `boardDeliverables`/`boardCompletionSummary` view-model helpers in `board.go`, and new `board.templ` sections. The `MemoryBackend` interface gains one method.
- **No server change**: renders from `WorkItemRun.Summary`, which already carries the `work_complete` payload.
- **Blueprint**: `task-board` instructs the worker to create and link each deliverable inline via a `delivers` edge (Task → Note) created at runtime, rather than declaring it as a relationship type — the CLI install preflight rejects a relationship target that is not an objectType in the same pack.
- **Non-goals**: no `requiredDeliverableTypes` enforcement or `validateWorkContract` tightening (deferred); no artifacts rendering.
- **Testing**: gateway unit tests (flatten/dedupe helper, `LookupGraphObjectByKey` httptest, drawer contract for resolved/missing/empty states) + build + lint.
