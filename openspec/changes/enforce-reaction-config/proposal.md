## Why

`ReactionConfig` advertises three controls that are never read: `IgnoreAgentTriggered`, `IgnoreSelfTriggered`, and `ConcurrencyStrategy`. The reaction path instead drops **all** agent-originated events in one unconditional check (`TriggerService.onEntityEvent`), so an operator who opts in to agent-originated triggering cannot, and `"skip"` never prevents a duplicate concurrent run. Dead configuration is silently ignored behaviour.

## What Changes

- Move the agent-origin gate out of the global early return and into per-agent matching, carrying the originating actor down to each candidate agent.
- Encode the safe default in the API shape: `ignoreAgentTriggered`/`ignoreSelfTriggered` become optional pointers (`omitempty`). Unset (`nil`) means the historical behaviour — agent-originated events are ignored (and self-triggers are ignored when agent-originated triggering is enabled).
- Enforce `ConcurrencyStrategy`: empty and `"parallel"` mean no concurrency control (today's behaviour); `"skip"` drops a new trigger when a non-terminal run already exists for the same agent and target object.
- Update the Go SDK struct, go-sdk reference docs, and regenerated Swagger to match the pointer/omitempty shape.

## Capabilities

### New Capabilities

- `agent-reaction-triggers`: enforcement of the per-agent `ReactionConfig` controls (`ignoreAgentTriggered`, `ignoreSelfTriggered`, `concurrencyStrategy`) on the inline event-driven reaction path.

### Modified Capabilities

- None.

## Impact

- Server: `apps/server/domain/agents/entity.go` (pointer fields), `triggers.go` (per-agent gate, actor carry, concurrency skip), `repository.go` (`HasActiveRunForAgentObject`).
- Go SDK: `apps/server/pkg/sdk/agents/client.go`.
- Docs/Swagger: `docs/site/go-sdk/reference/agents.md`, `apps/server/docs/swagger/*`.
- No database migration: run↔object linkage for the concurrency key is recorded in the existing `kb.agent_runs.trigger_metadata` (`subjectObjectId`/`subjectObjectType`).
- Out of scope: the queued/claim object-driven dispatch path (`add-object-driven-agent-work`); this change covers the existing inline reaction path.
