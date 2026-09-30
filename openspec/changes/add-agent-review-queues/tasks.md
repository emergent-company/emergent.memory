# Tasks — Queue-backed review agents

<!-- openspec:archive-hold: phase 1 (named queues, routing, per-queue workers, queue API, agent binding) ships in this PR; phases 2–3 (review findings store, output-action dispatcher, GitHub producer, design/product agents, UI/CLI surfaces) are deferred. -->

## 1. Queue data model

- [x] 1.1 Migration `kb.agent_queues` (`name`, `project_id`, `display_name`, `description`, `concurrency`, `priority`, `enabled`, timestamps; PK `(project_id, name)`); seed a `default` queue for every existing project. Verify with `task migrate:up`.
- [x] 1.2 Migration: add `queue TEXT NOT NULL DEFAULT 'default'` and `priority INT NOT NULL DEFAULT 100` to `kb.agent_run_jobs`; add claim index `(queue, status, priority, next_run_at) WHERE status='pending'`. Verify with `task migrate:up` + `task migrate:status`.
- [x] 1.3 Migration: add `default_queue TEXT NOT NULL DEFAULT 'default'` to `kb.agent_definitions`. Verify with `task migrate:up`.
- [x] 1.4 `AgentQueue` entity + `AgentRunJob.Queue`/`Priority` + `AgentDefinition.DefaultQueue` fields in `domain/agents/entity.go`. Verify with `go build ./...`.

## 2. Repository — queue CRUD + routing + claim

- [x] 2.1 `Repository` methods: `ListQueues(projectID)`, `GetQueue(projectID, name)`, `UpsertQueue`, `DeleteQueue` (reject when pending/processing jobs exist), `EnsureDefaultQueue(projectID)`. Verify with `go test ./domain/agents/...`.
- [x] 2.2 Resolve queue+priority at enqueue: extend `CreateRunQueued`/`CreateRunJob` (and their options) to persist `queue`+`priority` from agent binding/override. Verify with `go test`.
- [x] 2.3 `ClaimNextJobInQueue(ctx, queue)` — queue-scoped, priority-ordered, `FOR UPDATE SKIP LOCKED`, preserving the cancel-wins run guard; `ClaimNextJob` delegates with `""` (any queue). Verify with unit tests covering priority order and queue isolation.
- [x] 2.4 `QueueDepth(projectID)` returning pending/processing counts per queue for the list API. Verify with `go test`.

## 3. Worker pool — per-queue supervisor

- [x] 3.1 Refactor `WorkerPool` into a supervisor that loads enabled queues and maintains `concurrency` workers per queue via `ClaimNextJobInQueue`; refresh on an interval; `default` concurrency falls back to `AGENT_WORKER_POOL_SIZE`; `size=0` still disables everything. Verify with `go test ./domain/agents/...`.
- [x] 3.2 Config: add queue refresh interval; document that `AGENT_WORKER_POOL_SIZE` sizes the `default` queue. Verify `go build ./...`.
- [x] 3.3 Unit tests: bound concurrency per queue; two queues progress independently; disabled queue not polled; `default` fallback sizing. Verify with `go test`.

## 4. API — queue management + enqueue

- [x] 4.1 DTOs + handlers: `GET/POST /api/projects/:projectId/agent-queues`, `PATCH/DELETE /agent-queues/:name`, `POST /agent-queues/:name/enqueue`. Auth behind existing `agents:read`/`agents:write` scopes and project membership. Verify with `go build ./...`.
- [x] 4.2 Wire routes in `domain/agents/routes.go`; return 409 on duplicate/undrained-delete, 404 on unknown queue, 400 on invalid concurrency. Verify with handler tests.
- [x] 4.3 Expose `defaultQueue` on agent-definition create/update DTOs so a definition can be bound to a queue. Verify with `go test`.

## 5. Security-review pilot (config)

- [x] 5.1 Blueprint `blueprints/security-review/` with a `security-review` agent definition (queue binding, review-contract system prompt, graph-write tools for phase-1 findings), plus the `security-review` queue seed. Verify with `memory blueprints` dry-run / `openspec validate`.
- [ ] 5.2 Wire a producer that enqueues on `security-review`: document manual/CLI enqueue; add a reaction-trigger example on graph objects. Verify manually against a running server.

## 6. Verify

- [x] 6.1 `go build ./...` + `go vet ./...` + `go test ./domain/agents/...` in `apps/server`.
- [ ] 6.2 `task migrate:up` against the dev DB; confirm `default` queue seeded and columns present.
- [ ] 6.3 Smoke test: enqueue two work items on two queues, confirm independent processing and per-queue concurrency.

## 7. Findings store (phase 2 — deferred)

- [ ] 7.1 Migration `kb.review_findings` + entity + store CRUD.
- [ ] 7.2 `GET /api/projects/:projectId/review-findings` with queue/run/subject/severity/status filters.
- [ ] 7.3 A write path for agents to record findings (tool) or a post-run extraction step.

## 8. Output actions (phase 2 — deferred)

- [ ] 8.1 Post-run action dispatcher reading definition `config.outputActions`; error-isolated, best-effort.
- [ ] 8.2 Actions: `create_task` (kb.tasks), `notify`, `graph_write`; then `github_comment`.
- [ ] 8.3 Unit tests for dispatcher ordering and per-action failure isolation.

## 9. GitHub producer (phase 2 — deferred)

- [ ] 9.1 GitHub App token minting from `core.github_app_config` (reuse/extend `domain/sandbox/checkout.go`).
- [ ] 9.2 Webhook ingress for `pull_request` (opened/synchronize) → enqueue security-review with PR subject metadata.
- [ ] 9.3 Post review results back to the PR (review comment / status check).

## 10. Design & product agents + UI/CLI (phase 3 — deferred)

- [ ] 10.1 `design-review` and `product-review` agent definitions + queues + prompts.
- [ ] 10.2 Web UI: queue list/detail with live depth; findings browser.
- [ ] 10.3 CLI: queue bind/list/enqueue flags under `memory agents`.
