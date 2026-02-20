## Context

The agent system currently supports three trigger types (`schedule`, `manual`, `reaction`), but only `schedule` and `manual` are fully wired. Reaction triggers exist but `TriggerService.HandleEvent()` is never called. There's no way for external systems to trigger agents via HTTP webhooks.

The codebase uses:

- **Go with Echo framework** for HTTP handlers
- **Bun ORM** with PostgreSQL for persistence
- **fx (Uber's dependency injection)** for module wiring
- **Existing patterns**: GitHub webhook handler (`domain/githubapp`) shows HMAC signature verification; events service (`domain/events`) has pub/sub with `ActorContext` for loop prevention

## Goals / Non-Goals

**Goals:**

- Add configurable webhook hooks per agent (create/list/delete, token-based auth)
- Create public webhook receiver endpoint (`POST /api/webhooks/agents/:hookId`)
- Wire `events.Service` to `TriggerService.HandleEvent()` for reaction triggers
- Track webhook trigger source metadata in `AgentRun` entity
- Rate limiting per webhook hook

**Non-Goals:**

- Outgoing webhooks (agent → external system notifications on completion)
- OAuth-based webhook auth (token-only for now)
- Retry logic for failed webhook deliveries (fire-and-forget)
- Webhook payload transformation/templating

## Decisions

### Decision 1: Webhook Hook as Separate Entity vs. Embedded Config

**Choice:** Create a separate `AgentWebhookHook` entity in `kb.agent_webhook_hooks` table.

**Rationale:**

- An agent can have multiple webhook hooks (dev, staging, prod; different integrations)
- Each hook needs its own token, label, enabled flag, and rate limit config
- Separation allows CRUD operations without modifying the agent entity
- Follows existing pattern (agents have separate `AgentDefinition` entities)

**Alternatives considered:**

- Embed hooks as JSONB array in `Agent.Config` → harder to query, no relational integrity, awkward CRUD
- Single webhook URL per agent → limits flexibility for multi-environment integrations

### Decision 2: Token Storage and Verification

**Choice:** Store bcrypt hash of token in database. Show plaintext token only once on creation (like GitHub personal access tokens).

**Rationale:**

- Bcrypt is already used elsewhere in the codebase (user passwords)
- Tokens are bearer credentials — hashing prevents DB leak from exposing all tokens
- One-time display forces users to store securely
- Verification is fast enough for webhook use case (<100ms)

**Alternatives considered:**

- HMAC signature verification (like GitHub webhooks) → more complex for users (need to implement signing), no existing Go SDK
- Store plaintext encrypted with application key → requires key rotation strategy, key compromise exposes all tokens

### Decision 3: Public Route Registration

**Choice:** Register webhook receiver on a new route group (`/api/webhooks/`) without admin auth middleware. Use custom middleware for token verification.

**Rationale:**

- Webhook callers are external systems without Zitadel session/JWT
- GitHub webhook handler already sets precedent (`/api/v1/settings/github/webhook` uses signature verification, not session auth)
- Separating `/api/webhooks/` namespace keeps public endpoints visually distinct

**Implementation:**

```go
// In domain/agents/routes.go
func RegisterWebhookRoutes(e *echo.Echo, handler *Handler) {
    webhooks := e.Group("/api/webhooks/agents")
    webhooks.POST("/:hookId", handler.ReceiveWebhook) // No RequireAuth middleware
}
```

### Decision 4: Rate Limiting Strategy

**Choice:** In-memory token bucket with project-level limits (per webhook hook). Use `golang.org/x/time/rate` package.

**Rationale:**

- Protects against webhook spam/abuse
- In-memory is acceptable (rate limit resets on server restart are non-critical)
- Per-hook limits allow different quotas (e.g., 10/min for dev hooks, 100/min for prod)
- Simple implementation, no new dependencies (stdlib package)

**Alternatives considered:**

- Redis-based distributed rate limiting → overkill for single-instance deployment, adds dependency
- Database-backed rate limiting → too slow, adds write load
- No rate limiting → risk of DoS via webhook spam

**Configuration:**

```go
type RateLimitConfig struct {
    RequestsPerMinute int `json:"requests_per_minute"` // Default: 60
    BurstSize         int `json:"burst_size"`          // Default: 10
}
```

### Decision 5: Event Bridge Architecture (Reaction Triggers)

**Choice:** Subscribe `TriggerService` to `events.Service` during startup via fx lifecycle hook. Filter events by `ActorContext` to prevent loops.

**Rationale:**

- `TriggerService.HandleEvent()` already exists with full implementation — just needs a caller
- `events.Service.Subscribe()` returns unsubscribe function — clean lifecycle management
- `ActorContext.ActorType == "agent"` check prevents agent-triggered events from re-triggering agents (infinite loops)
- Keeps event and agent domains decoupled (no direct import from events → agents)

**Implementation:**

```go
// In domain/agents/module.go
func registerEventBridge(lc fx.Lifecycle, ts *TriggerService, eventsSvc *events.Service) {
    var unsubscribe func()
    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            unsubscribe = eventsSvc.Subscribe("", func(event events.EntityEvent) {
                // Filter: only graph_object events, not agent-triggered
                if event.Entity == events.EntityGraphObject &&
                   (event.Actor == nil || event.Actor.ActorType != events.ActorTypeAgent) {
                    ts.HandleEvent(ctx, event)
                }
            })
            return nil
        },
        OnStop: func(ctx context.Context) error {
            if unsubscribe != nil {
                unsubscribe()
            }
            return nil
        },
    })
}
```

**Note:** `events.Service.Subscribe()` is currently project-scoped. For reaction triggers to work across all projects, we need to either:

- Subscribe once per project (dynamically as projects are created), OR
- Add a global subscription mode to events service (pass empty string for projectID → receives all events)

Choose: **Global subscription mode** (simpler, fewer subscriptions, all agents managed centrally).

### Decision 6: Webhook Payload Schema

**Choice:** Accept JSON body with optional `prompt` and `context` fields. Pass to `ExecuteRequest.UserMessage`.

**Rationale:**

- Flexible enough for most use cases (CI/CD can send commit message, external tools can send context)
- Simple schema reduces integration friction
- Maps cleanly to existing `ExecuteRequest` structure

**Schema:**

```json
{
  "prompt": "optional user message to agent",
  "context": {
    "arbitrary": "key-value pairs for agent context"
  }
}
```

Empty body is valid (agent runs with its default prompt).

### Decision 7: AgentRun Source Tracking

**Choice:** Add `TriggerSource` and `TriggerMetadata` JSONB fields to `AgentRun` entity.

**Rationale:**

- Allows tracing which webhook/schedule/manual action triggered a run
- JSONB is flexible for varying metadata per trigger type
- Useful for debugging and analytics

**Example metadata:**

```json
{
  "trigger_source": "webhook",
  "webhook_hook_id": "uuid",
  "webhook_hook_label": "CI/CD Pipeline",
  "request_ip": "192.0.2.1",
  "user_agent": "GitHub-Hookshot/abc123"
}
```

## Risks / Trade-offs

**Risk: Webhook token leakage**
→ **Mitigation:** Tokens are bcrypt-hashed at rest. One-time display on creation. Users should store in CI/CD secret vaults.

**Risk: Webhook spam causing excessive agent runs**
→ **Mitigation:** Per-hook rate limiting (60 req/min default). Returns 429 when exceeded.

**Risk: Event bridge infinite loops (agent triggers event → event triggers agent)**
→ **Mitigation:** Filter events by `ActorContext.ActorType != "agent"`. `events.Service` already tracks actor on emit.

**Risk: Webhook receiver endpoint DoS**
→ **Mitigation:** Rate limiting + payload size limit (1MB max). Consider adding fail2ban-style IP blocking if abuse detected.

**Risk: Reaction triggers fire for every graph object change (high volume)**
→ **Mitigation:** `ReactionConfig.ObjectTypes` filter (agents specify which object types to watch). `AgentProcessingLog` tracks processed objects to prevent duplicates.

**Trade-off: In-memory rate limiting resets on server restart**
→ **Acceptable:** Webhook abuse is short-term risk. Rate limits are per-minute, not critical to persist across restarts. If DoS becomes issue, migrate to Redis.

**Trade-off: No webhook delivery guarantees (fire-and-forget)**
→ **Acceptable:** Webhooks trigger agent runs asynchronously. Caller receives 202 Accepted immediately. If agent run fails, it's logged in `AgentRun.LastRunStatus`. For critical workflows, callers can poll run status via `/api/projects/:projectId/agent-runs/:runId`.

## Migration Plan

**Phase 1: Database schema**

1. Migration: `00028_create_agent_webhook_hooks.sql`
   - Create `kb.agent_webhook_hooks` table
   - Add `trigger_source` TEXT and `trigger_metadata` JSONB to `kb.agent_runs`
   - No data backfill needed (new fields)

**Phase 2: Backend implementation** 2. Create `domain/agents/webhook_hooks.go` (entity, repo methods) 3. Add webhook hook CRUD handlers to `domain/agents/handler.go` 4. Add webhook receiver handler (`ReceiveWebhook`) 5. Register webhook routes in `routes.go` 6. Wire event bridge in `module.go`

**Phase 3: Frontend UI** 7. Add webhook hooks section to agent detail page 8. List hooks (label, URL, enabled, created date) 9. "Create Hook" button → modal with label input → show token once 10. "Delete Hook" button with confirmation

**Phase 4: Testing** 11. E2E test: create hook, POST to webhook URL with token, verify run created 12. E2E test: rate limit enforcement (send 100 requests, verify 429) 13. E2E test: reaction trigger via event bridge

**Rollback:** Drop table, remove fields from `agent_runs`. No data loss (new feature).

## Open Questions

1. **Should webhook hooks have expiry dates?**

   - Pro: Reduces risk of forgotten/leaked tokens
   - Con: Adds complexity (background job to disable expired hooks)
   - **Decision:** Not in v1. Add if requested by users.

2. **Should we support webhook signatures (HMAC) in addition to bearer tokens?**

   - Pro: More secure (request body integrity verification)
   - Con: More complex for users to implement
   - **Decision:** Not in v1. Bearer tokens are simpler. Add HMAC as opt-in enhancement if needed.

3. **What's the max number of hooks per agent?**

   - **Decision:** 10 per agent (soft limit via UI validation). DB has no constraint.

4. **Should webhook receiver return run ID in response?**
   - **Decision:** Yes. Return `{"run_id": "uuid", "status": "queued"}` in 202 response. Allows caller to poll run status.
