# Tasks — Object-driven agent work

<!-- openspec:archive-hold: this change ships in phases P1–P6 (spec + implementation in one PR via add-object-driven-agent-work's branch). Do not archive until the implementation phases are complete. -->

## 1. P1 — Dispatch, claim, and linkage

- [ ] 1.1 Migration: add `kb.graph_objects.assignee TEXT NULL` + index; add `kb.agent_runs.subject_object_id UUID NULL`, `subject_object_type TEXT`, `failure_class TEXT NULL` + index on `subject_object_id`. Verify `task migrate:up`.
- [ ] 1.2 Add `assignee` to graph object create/update/list DTOs and filters (`domain/graph`); include it in the content hash alongside status. Verify `go test ./domain/graph/...`.
- [ ] 1.3 Add `workConfig` to the agent definition (status map, `requiresReview`, `failureLimit`, `retryPolicy`) with defaults; expose in definition DTOs. Verify `go test ./domain/agents/...`.
- [ ] 1.4 Change `triggers.go` so a matched reaction enqueues a run (respecting queue + idempotency `agentId+canonicalId+version`) instead of calling `executor.Execute` inline; gate on `workConfig`/`dispatchMode`. Verify with unit + DB tests.
- [ ] 1.5 Enforce `ConcurrencyStrategy: skip` and route by `assignee` (matched listener only; else any listener). Verify with tests.
- [ ] 1.6 Atomic claim: CAS object status ready→in-progress on job claim; no-op when already claimed/finished. Verify with a concurrency test (two claims, one runs).
- [ ] 1.7 Persist `subject_object_id`/`subject_object_type` on the run at enqueue. Verify with a DB test.
- [ ] 1.8 Spike + implement isolated attempt: per-attempt graph branch (+ sandbox workspace), written on claim. Verify merge/discard semantics against `domain/branches`.

## 2. P2 — Lifecycle and failure

- [ ] 2.1 Builtin tools `work_complete` and `work_block` (write status via `canonical_id`, attach summary/artifacts, create human task on block). Verify with tool tests.
- [ ] 2.2 Protocol-violation detection: a run that ends without a terminator is retried bounded, then blocked as agent-quality. Verify with tests.
- [ ] 2.3 Failure classification on run failure (transient/agent-quality/terminal/needs-input/capability) reusing `FailRun`/job backoff. Verify unit tests.
- [ ] 2.4 Per-work-item failure budget (default 3) persisted; exhaustion → blocked dead-letter + `kb.tasks`/notification. Verify DB tests.
- [ ] 2.5 Capability failure → clear assignee → ready; transient → retry without budget tick; terminal → immediate block. Verify DB tests.
- [ ] 2.6 Compact the failure into the retry/rework context; ensure produced objects are upserted by `key` (idempotent). Verify with a retry test.

## 3. P3 — Review and rework

- [ ] 3.1 `requiresReview` sends completion to the review status; branch staged, not merged. Verify tests.
- [ ] 3.2 Approve → merge branch, status done. Verify DB test.
- [ ] 3.3 Request changes → status revision + recorded feedback + explicit rework enqueue reusing the retained branch. Verify tests.
- [ ] 3.4 Revision cap → escalate to human (no infinite loop). Verify test.

## 4. P4 — Per-agent and per-type configuration

- [ ] 4.1 Per-type allowed `status` values validated on write (schemas registry). Verify tests.
- [ ] 4.2 Per-type `boardEnabled` flag and operational flags (skip embeddings/extraction/default search) honored by the respective pipelines. Verify tests.
- [ ] 4.3 Per-agent/per-type `failureLimit` and `retryPolicy` overrides. Verify tests.

## 5. P5 — Kanban projection and UI

- [ ] 5.1 Read projection query: board-enabled objects (status, assignee) left-joined to runs on `subject_object_id`; expose an API. Verify query tests.
- [ ] 5.2 Gateway board view: columns by status, execution badge, drag = transition/enqueue, live updates. Verify build + lint.
- [ ] 5.3 Card drawer: object, sessions/runs, artifacts, feedback; actions Retry / Reassign / Cancel / Approve / Request changes. Verify browser test.
- [ ] 5.4 Failed/Blocked lanes and agent-health (circuit breaker) surfacing. Verify.
- [ ] 5.5 Exclude sessions with no associated object from the board. Verify.

## 6. P6 — Contract validation and compensation

- [ ] 6.1 Optional work contract: validate required deliverables before `work_complete` allows done.
- [ ] 6.2 Compensation/rollback for partial effects across attempts.

## 7. Verify (per phase)

- [ ] 7.1 `go build ./...` + `go vet ./...` in `apps/server`.
- [ ] 7.2 `go test ./domain/agents/... ./domain/graph/... ./domain/schemas/...` (DB integration where applicable).
- [ ] 7.3 `task lint` / `lint-ratchet.sh` (no new architectural debt).
- [ ] 7.4 `openspec validate add-object-driven-agent-work`.
- [ ] 7.5 E2E: "Research for new Gemini model" object → researcher → report → review → done; and a forced-failure → dead-letter → reassign path.
