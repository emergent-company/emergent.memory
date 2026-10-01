# Task Board

A sample Kanban-board blueprint: it installs one board-enabled object type
(`Task`), one reaction-triggered worker agent (`task-worker`), and a single
example `Task` seed object, so the object-driven board at `/board` has a
concrete, working lane out of the box.

## What it installs

- **Schema pack `task-board`** — a `Task` object type with
  `boardEnabled: true`, allowed statuses
  `ready / in_progress / review / revision / blocked / done`, the operational
  flags `skipEmbeddings / skipExtraction / excludeFromSearch` set, and a
  `blocks` self-relationship (`Task → Task`).
- **Agent `task-worker`** — a queued, reaction-triggered agent that wakes on
  `created` events for `Task` objects, claims the item, does the work, writes
  deliverables, and ends the run with `work_complete` (or `work_block`). Its
  `workConfig` requires human review before done and requires artifacts.
- **Seed `Task`** — one `example-task` object in `ready` state, assigned to
  `task-worker`.

## Applying it

```bash
memory blueprints blueprints/task-board
```

Or install it from the web UI's blueprint gallery. The apply is idempotent for
the pack, the agent, and the keyed seed object.

## How a Task flows

`ready` → `in_progress` (the worker claims it) → `review` (the worker calls
`work_complete`; review is required) → `done` (a human approves). A
`work_block` call or an exhausted failure budget moves the Task to `blocked`,
and a human request-for-changes moves it to `revision` with a rework run.

## Creating another Task

Create a new object of type `Task` with a non-empty `key` and `status: ready`
(from the UI object form or the graph API). Leave `assignee` empty to let any
listening agent claim it, or set it to `task-worker` to route it to this
worker.
