# Tasks — Object-driven agent work

<!-- openspec:archive-hold: ships in phases P1–P6 (spec + implementation in one PR on this branch, after add-agent-review-queues #1304 lands). Do not archive until the implementation phases are complete. -->

> **Sequencing:** `add-agent-review-queues` (#1304) lands first — it adds migration `00204`, the queue/priority columns, and per-queue workers. This change's migrations are `00205+`. Any new route requires `apps/server/route-authority.yaml`; any new bun model requires registration in `apps/server/internal/schemadrift/models.go`; new services/tools require fx `module.go` wiring.

## 1. P1 — Dispatch, claim, and linkage

- [x] 1.1 Migration `00205`: add `kb.graph_objects.assignee TEXT NULL` + index; add `kb.agent_runs.subject_object_id UUID NULL`, `subject_object_type TEXT`, `failure_class TEXT NULL` + index on `subject_object_id`. Verify `task migrate:up`.
- [ ] 1.2 Add `assignee` to graph object create/update/list DTOs and filters, meaningful only for board-enabled types. Verify `go test ./domain/graph/...`.
- [x] 1.3 Add `workConfig` to the agent definition (status map, `requiresReview`, `failureLimit`, `retryPolicy`) with defaults; expose in definition DTOs. Verify `go test ./domain/agents/...`.
- [x] 1.4 Change `triggers.go` so a matched reaction enqueues a run instead of calling `executor.Execute` inline; dedup against `kb.agent_processing_log` (`agent_id+graph_object_id+object_version+event_type`, with `graph_object_id` = the object's `canonical_id`); take HEAD `version` at dispatch (not from the batch payload). Gate on `workConfig`/`dispatchMode`. Verify unit + DB tests.
- [x] 1.5 Routing: enqueue only the listener matching `assignee` within the object's project; surface unroutable objects via a **derived** predicate (board-enabled + no matching listener) — no new persisted status. Enforce `ConcurrencyStrategy: skip`. Verify tests.
- [x] 1.6 Claim: `AcquireObjectUpsertLock` → `FindHeadByTypeAndKey` → `CreateVersion(in_progress)` in one tx; lost race → run `skipped`, no budget burn. Require non-null `key` for board-enabled types. Verify with a two-claim concurrency test (hard gate).
- [x] 1.7 Persist `subject_object_id`/`subject_object_type` on the run at enqueue. Verify DB test.

## 2. P2 — Lifecycle, mapping, and claim liveness

- [x] 2.1 Run-finalizing tools `work_complete` (→ done | review) and `work_block` (→ blocked + `kb.tasks`); platform ends the run on the terminator call. Verify tool tests.
- [x] 2.2 Implement the exhaustive run-end → item-transition → budget-impact mapping (design table); add a test per row.
- [x] 2.3 Single-work-status write path: advisory lock + `CreateVersion` + consistent `properties["status"]`; **reject** direct agent work-status writes (both the `status` field and `properties["status"]`) at every entry point — `Create`, `CreateOrUpdate`, `Patch`, `BulkUpdateStatus`, bulk actions. Verify tests.
- [x] 2.4 Work-status reaper: `in-progress` objects with no **live** run past threshold → `ready` (budget-aware) or `blocked`. Model on `StaleRunReaper`; define liveness explicitly (live = `queued`/`running`/**`paused`**), **exclude `paused` runs**, and coordinate with `MarkStaleRunsAsError`'s `last_step_at` threshold so the two reapers never race. Verify tests.
- [x] 2.5 Reconciler: enqueue `ready` board-enabled objects with no live run and no live job (live job = `pending`/`processing`), tolerating the window between a job completing as `skipped` and the item transition committing. Verify tests.
- [x] 2.6 Failure classification (retryable / deterministic / human); add the net-new provider→executor **quota/429 error typing** required for the "quota is agent-level" rule; per-item budget; unassign→ready with requeue backoff and attempt history retained. Verify DB tests.
- [x] 2.7 Extend the existing breaker (`ConsecutiveFailures`/auto-disable) with thresholds; poison-item vs broken-agent distinction. Verify tests.

## 3. P3 — Review, rework, and human actions

- [x] 3.1 `requiresReview` → `status=review` + `needs_review=true`; approve → `reviewed_by`/`reviewed_at` + `needs_review=false` + `status=done`. **Build the write path for these columns — they are declared and read but never written today.** Note the accepted trade-off: produced objects are visible/searchable before approval. Verify tests.
- [x] 3.2 Request changes → `status=revision` + append-only `feedback[] {round,author,text,runId}` (non-empty required) + rework enqueue carrying all prior feedback; revision cap → `kb.tasks` escalation. Verify tests.
- [x] 3.3 Human-action API: approve / request-changes / retry / reassign / cancel — handlers + DTOs + auth (project-member) + `route-authority.yaml` entries. Verify handler tests + `go run ./cmd/route-authority-guard`.
- [x] 3.4 Escalation surfaces through `kb.tasks` (`type=work-escalation`) with full feedback history. Verify tests.

## 4. P4 — Per-type configuration

- [x] 4.1 Per-type `boardEnabled` + allowed `status` values, validated on write (schemas registry — new surface). Verify tests.
- [x] 4.2 Per-type operational flags (`skipEmbeddings`/`skipExtraction`/`excludeFromSearch`) honoured by the respective pipelines; required for board-enabled types. Verify tests.
- [x] 4.3 Per-agent/per-type `failureLimit` and `retryPolicy` overrides. Verify tests.

## 5. P5 — Kanban projection and UI

- [ ] 5.1 Read projection: board-enabled objects (status, assignee) left-joined to runs on `subject_object_id`; expose an API (+ `route-authority.yaml`). Verify query tests.
- [ ] 5.2 Gateway board view: columns by status, execution badge, drag = transition/enqueue, live updates. Verify build + lint.
- [ ] 5.3 Card drawer: object, sessions/runs, artifacts, feedback; actions Approve / Request changes / Retry / Reassign / Cancel. Verify browser test.
- [ ] 5.4 Failed/Blocked surfaces + agent-health (breaker) readout. Verify.
- [ ] 5.5 Exclude sessions with no associated object from the board. Verify.

## 6. P6 — Contract validation (deferred)

- [ ] 6.1 Optional work contract: validate required deliverables before `work_complete` allows done.

## 7. Verify (per phase)

- [ ] 7.1 `go build ./...` + `go vet ./...` in `apps/server`.
- [ ] 7.2 `go test ./domain/agents/... ./domain/graph/... ./domain/schemas/... ./internal/schemadrift/...` (DB integration where applicable).
- [ ] 7.3 `task lint` / `lint-ratchet.sh`; `go run ./cmd/route-authority-guard`.
- [ ] 7.4 `openspec validate add-object-driven-agent-work`.
- [ ] 7.5 E2E: "Research for new Gemini model" object → researcher → report → review → done; plus a forced-failure → dead-letter → reassign path and a crash-after-claim → reaper → ready path.
