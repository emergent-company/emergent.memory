## Why

`EntityEvent.Actor` is nil in production: the `events.Service` emit methods never read the actor stamped on the context, and no emitter passes `EmitOptions.Actor`. The actor *is* stamped at every run boundary (`auth.WithActor(ctx, ActorAgent|ActorSystem, id)` in the agent executor, the per-agent MCP endpoint, and the extraction worker), but the events package never consults it.

Consequence: every event looks non-agent-originated. The per-agent agent-origin gate shipped in `enforce-reaction-config` (#1321) is **dormant** — `ReactionConfig.IgnoreAgentTriggered`/`IgnoreSelfTriggered` have no observable effect, and the loop-prevention they advertise is not actually enforced.

## What Changes

- **Single choke point.** `events.Service` resolves the event actor at emit time: an explicit `EmitOptions.Actor` is authoritative; otherwise the actor stamped on the context (via the existing `auth.ActorFromContext`) is stamped onto the event. The typed emit methods (`EmitCreated`/`EmitUpdated`/`EmitDeleted`/`EmitBatch`) take a `context.Context` so the stamped actor is reachable.
- Every emit call site passes its `context.Context` (`domain/graph`, `domain/notifications`, `domain/agents` a2a stream + ask-user tool).
- No new `pkg/auth` accessor is required — `auth.ActorFromContext` already exists and is the read accessor.

## Behaviour Change

Today `EntityEvent.Actor` is nil, so every event is treated as non-agent and reactions fire. After this change, agent-originated events carry `Actor{ActorType:"agent", ActorID:<agent definition id>}` and, per `enforce-reaction-config`, the reaction gate **ignores them by default**.

- Agent-triggered reactions that fire today **stop firing by default**.
- Operators opt back in with `ignoreAgentTriggered: false` (and, for self-triggers, `ignoreSelfTriggered: false`).

This activates the behaviour specified in `agent-reaction-triggers`; it is intentional and matches the flags' documented default. Rollout alternative if this default is judged too risky: gate the context-actor read behind a config flag defaulting to the current (nil actor) behaviour.

## Capabilities

### New Capabilities

- `events-actor-attribution`: the events bus stamps `EntityEvent.Actor` from an explicit `EmitOptions.Actor` or, failing that, the `auth` actor on the context.

### Modified Capabilities

- None.

## Impact

- Server: `apps/server/domain/events/service.go` (actor resolution + ctx-taking emit methods), call sites in `domain/graph/service.go`, `domain/notifications/service.go`, `domain/agents/a2a_stream.go`, `domain/agents/ask_user_tool.go`.
- Tests: `domain/events/service_test.go`, `domain/events/handler_test.go`, `domain/agents/triggers_test.go`, `domain/agents/ask_user_tool_test.go`.
- No database migration. No API/schema change. `EmitOptions.Actor` keeps its meaning (explicit actor wins).
