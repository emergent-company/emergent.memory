---
name: task-workflow
description: How a Task flows through the Kanban board from ready to done.
---
# Task workflow

A Task is a board-enabled graph object. Its lifecycle is driven by the
platform work path, not by the agent writing the status directly.

## Status flow

- **ready** — the Task has a stable `key`, is assigned (or unassigned), and is
  waiting to be picked up.
- **in_progress** — a listening worker (the task-worker agent) has claimed the
  Task and is working it.
- **review** — the worker called `work_complete` and its `workConfig` sets
  `requiresReview: true`, so a human must approve before it is done.
- **revision** — a human requested changes; a rework run is enqueued with the
  prior feedback.
- **blocked** — the worker called `work_block` (or the failure budget was
  exhausted); a human-facing task surfaces the reason.
- **done** — the Task is complete and, if review was required, approved.

## Who moves it

The task-worker agent is the only writer of work status, and it does so
indirectly by calling `work_complete(summary, artifacts)` or
`work_block(reason, kind)`. Humans move it between review / revision / done via
the board's approve and request-changes actions.

## Creating another Task

Create a new Task object of type `Task` with a non-empty `key` and a `status`
of `ready` (via the UI object form or the graph API). If the `assignee` is left
empty, any listening agent for the `Task` type may claim it; if it is set to
`task-worker`, only the task-worker agent is enqueued.
