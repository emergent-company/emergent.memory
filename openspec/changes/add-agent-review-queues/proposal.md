## Why

Memory already runs queued agent work: `kb.agent_run_jobs` is a durable dispatch ledger claimed with `FOR UPDATE SKIP LOCKED` by a single global `WorkerPool`, and agents have `schedule`/`reaction`/`webhook`/`manual` triggers. What is missing is any notion of a **named queue** — every job lands in one implicit queue, routing is implicit in `run.agent_id`, there is no per-queue concurrency, priority, or visibility, and there is no first-class way to point *specialized* agents (security review, design review, product analysis) at a stream of work and have them pull from it.

This change adds named, priority-aware work queues on top of the existing ledger and introduces **review agents** — specialized agents that consume a queue, inspect a subject (a pull request, a design artifact, a product spec), and emit findings plus configurable output actions (comment, task, action). The pilot is a **security code review** agent.

## What Changes

- **Named queues with routing** — `kb.agent_queues` defines queues (name, concurrency, priority, enabled); `kb.agent_run_jobs` gains `queue` and `priority`; a run's queue is resolved from the triggering agent's binding. Claim is queue-scoped and priority-ordered.
- **Per-queue worker pools** — the `WorkerPool` becomes a supervisor that maintains each enabled queue's configured concurrency, keeping a `default` queue sized by the existing `AGENT_WORKER_POOL_SIZE` for backward compatibility.
- **Queue API + observability** — list queues with live depth, per-queue configuration, and a queue view; agents can be bound to a queue.
- **Review agents** — agent definitions declare a queue and a review subject type; a shared `self`/tool contract lets them write structured **findings**.
- **Findings store** — `kb.review_findings` holds structured results (subject, severity, category, location, title, detail, status) linked to the producing run.
- **Producers** — manual API/CLI enqueue, graph-object reaction triggers, cron sweeps, and (phase 2) a GitHub producer that ingests pull requests and posts review comments back.
- **Output actions** — a post-run dispatcher executes the agent's configured actions against its findings (phase 1: create a human review task / notify; phase 2: post a GitHub review comment, write graph objects).

## Capabilities

### New Capabilities

- `agent-work-queues`: named, priority-aware agent work queues with per-queue concurrency, routing from agent→queue, queue-scoped claiming, and a queue management/observability API.
- `agent-review-workflow`: specialized review agents that consume a queue, inspect a typed review subject, persist structured findings, and dispatch configurable output actions.

### Modified Capabilities

<!-- none — dispatch routing and per-queue concurrency are carried by the new `agent-work-queues` capability; `agent-execution` is not yet a tracked spec. -->

## Impact

- Server (`apps/server/domain/agents/`): `entity.go`, `repository.go` (queue resolution, queue-scoped claim, queue CRUD), `worker_pool.go` (per-queue supervisor), `module.go` (pool lifecycle), `handler.go`/`routes.go`/`dto.go` (queue API + agent binding), new `review_findings` model + store, post-run action dispatcher.
- Migrations: new `kb.agent_queues`, `kb.review_findings`; `queue`/`priority` columns + index on `kb.agent_run_jobs`; `default_queue` on `kb.agent_definitions`.
- Config: queue defaults and per-queue overrides; `AGENT_WORKER_POOL_SIZE` remains the `default` queue size.
- Web UI (`apps/web-ui/gateway/`): queue list/detail and review-findings surfaces (phase 3).
- Blueprints: a `security-review` agent definition (+ later `design-review`, `product-review`) with queue binding and output-action config.
- CLI: `memory agents` gains queue binding / enqueue flags (phase 2/3).
- No breaking change: absent configuration, all runs resolve to the `default` queue and behave exactly as today.

## Non-Goals

- Replacing `kb.agent_run_jobs` (it stays the ledger; queues are a routing/organisation layer on top).
- A general-purpose work-queue DSL or arbitrary workflow engine.
- GitHub App provisioning itself — the GitHub producer (phase 2) consumes the existing `core.github_app_config`; connecting the App is covered by `add-integrations`.
