## Why

Agents can currently only be triggered via manual admin action, cron schedules, or reaction events (the last of which isn't even wired up). There is no way for external systems — CI/CD pipelines, third-party SaaS tools, custom integrations, or partner platforms — to programmatically trigger an agent run. The existing `POST /api/admin/agents/:id/trigger` requires full admin auth (`admin:write` scope), making it unsuitable for machine-to-machine integrations that need scoped, revocable access.

Adding external trigger support via configurable webhook hooks unlocks agent-as-a-service patterns: an external system can POST to a stable URL with a secret token to kick off an agent, optionally passing context in the payload. This is a prerequisite for any serious integration story.

## What Changes

- Add a `webhook` trigger type to agents alongside existing `schedule`, `manual`, and `reaction` types
- Introduce a **webhook hook** entity: a configurable, per-agent incoming endpoint with its own auth token, payload schema expectations, and rate limiting
- Create a new **public webhook receiver endpoint** (`POST /api/webhooks/agents/:hookId`) that validates the hook token, resolves the target agent, and dispatches an `ExecuteRequest`
- Add webhook hook CRUD operations (create/list/update/delete/regenerate-token) to the admin API
- Add a UI surface for managing webhook hooks on the agent detail page (list hooks, create, copy URL+token, delete)
- Wire the existing but unused `TriggerService.HandleEvent()` to the `events.Service` to close the reaction trigger gap (prerequisite for a coherent trigger story)

## Capabilities

### New Capabilities

- `agent-webhook-hooks`: Configurable incoming webhook hook entities — CRUD, token-based auth, payload validation, rate limiting, and the public receiver endpoint that dispatches agent runs
- `agent-event-bridge`: Wiring the existing `events.Service` pub/sub to `TriggerService.HandleEvent()` so reaction-type agents actually fire on graph object events

### Modified Capabilities

- `agent-infrastructure`: Adding `webhook` to `AgentTriggerType` enum, associating hooks with agents, tracking webhook-triggered runs with source metadata in `AgentRun`

## Impact

- **Backend (Go)**: New `webhookhooks` sub-package or extension within `domain/agents` — entity, store, handler, routes. Modification to `entity.go` (new trigger type), `triggers.go` (register webhook triggers), `executor.go` (pass webhook context to runs). New public route group outside admin auth middleware.
- **Database**: New `kb.agent_webhook_hooks` table (hook ID, agent ID, project ID, token hash, label, enabled, rate limit config, created/updated). Migration to add `webhook` to trigger type enum.
- **Frontend (React)**: New webhook hooks management UI on agent detail page — list, create, copy URL/token, delete. Uses existing `useApi` hook and component patterns.
- **API surface**: New admin CRUD endpoints under `/api/admin/agents/:id/hooks`. New public endpoint `POST /api/webhooks/agents/:hookId` (no admin auth — token-based).
- **Security**: Webhook tokens are hashed at rest (bcrypt or SHA-256), shown once on creation. Rate limiting per hook to prevent abuse. Payload size limits.
