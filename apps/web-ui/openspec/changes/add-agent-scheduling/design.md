## Context

Emergent Memory already runs background agents on a cron schedule server-side: a `kb.agents` row holds `prompt` + `cron_schedule` + `trigger_type` (`schedule`/`manual`/`reaction`/`webhook`) + `enabled`, optionally linked to an `agent_definition_id` for model/system-prompt/tools. A `TriggerService` registers each enabled scheduled agent as a cron task in a `robfig/cron` scheduler and fires it; each execution is recorded in `kb.agent_runs` (with a `trigger_source` column). The gateway today only edits `agent-definitions` (the dashboard "agent") and never touches `kb.agents`, so none of this is reachable from Alfred. See proposal.md for motivation; specs/agent-scheduling/spec.md + specs/session-log-api/spec.md for requirements.

## Goals / Non-Goals

**Goals:**

- Expose memory's scheduled-agent model through the gateway: create/edit/enable/disable/delete/trigger a scheduled agent (prompt + cron), and see its last-run status.
- Give sessions/runs an origin so scheduled runs are distinguishable and filterable.
- Reuse the existing go-daisy/templ UI patterns (agent dashboard settings panels, chat-rail filter) — no new UI framework.

**Non-Goals:**

- No Emergent Memory service changes (memory is external; the gateway reads/writes it over HTTP).
- No reimplementation of the cron engine in the gateway — the gateway passes the cron string through and surfaces memory's validation.
- No reaction/webhook trigger types in this slice — only `schedule` (and manual trigger).
- No cross-project or org-level scheduling; scheduled agents stay project-scoped like the rest of the gateway.

## Decisions

### D1 — Proxy memory's runtime-agent endpoints through the gateway

The gateway gains `MemoryClient` methods + handlers for memory's `/api/projects/:id/agents` routes: list, get, create, update, enable, delete, trigger, and list runs (`/agents/:id/runs`). Mirror DTOs `ScheduledAgent` (id, name, prompt, cronSchedule, enabled, triggerType, agentDefinitionId, lastRunAt, lastRunStatus, consecutiveFailures) and `ScheduledAgentRun` (id, status, startedAt, completedAt, summary, triggerSource, errorMessage). They reuse the server-side memory token; writes additionally need the `agents:write` scope (reads already use `agents:read`, per the agent-run-preview work).

*Alternatives considered:* call memory directly from the browser — rejected, would leak the memory token.

### D2 — Cron is a pass-through string (robfig cron, seconds-precision)

Memory schedules agents with `robfig/cron` (`cron.WithSeconds()`, 6-field `sec min hour dom mon dow`) and validates agent cron with a 5-field parser plus a minimum-interval safeguard (default 15m). The gateway does not reimplement cron parsing authoritatively: it validates the field is non-empty and passes it through, surfacing memory's rejection verbatim. The 5-vs-6-field discrepancy is a documented risk (below), not something the gateway resolves.

### D3 — Origin is derived from the scheduled agent's `trigger_type`, not `trigger_source`

`agent_runs.trigger_source` is set for manual (`"manual"`), webhook (`"webhook:<id>"`), and ACP (`"acp"`) runs, but `executeTriggeredAgent` does not set it for cron runs — so a scheduled run's `trigger_source` is currently NULL. The gateway therefore derives "scheduled" origin from the scheduled agent's `trigger_type == 'schedule'` (and the run's owning agent), which is reliable and needs no memory change. If memory later starts setting `trigger_source = "schedule"`, the gateway can switch to that field.

*Alternatives considered:* patch memory to set `trigger_source` on cron runs — rejected, memory is out of scope.

### D4 — Scheduled runs surface in the session list alongside conversations

Scheduled executions are `agent_runs`, not `chat_conversations`, so they do not appear in the existing conversation-based session browser. The gateway's session list (`listSessions`/`sessionSummary`) gains an `origin` field and merges scheduled runs in: conversations map to `manual` (voice/text treated as on-demand), and scheduled runs are fetched via `GET /agents/:id/runs` for agents with `trigger_type='schedule'` and mapped to the same session-summary shape with `origin=scheduled`. A `?origin=` query param filters the list, satisfying "show only scheduled sessions" without a memory change. The timeline for a scheduled session reuses the run-transcript endpoints from `agent-run-preview`.

*Alternatives considered:* (a) require memory to also create a conversation per scheduled run — rejected (memory change); (b) show scheduled runs only in the runs view, not the session list — rejected, the user asked for session filtering.

### D5 — UI: a "Schedules" view + an origin filter on the session browser

A new Schedules page (list + create/edit form: name, prompt, cron expression, enabled toggle, optional agent-definition selector) follows the agent dashboard settings-panel pattern. The session browser (and the agent-runs list) gains an origin/type filter with a "Scheduled" option, mirroring the existing chat-rail agent filter approach.

## Risks / Trade-offs

- [Scheduled runs lack `trigger_source`] → derive origin from `trigger_type` (D3); document and revisit if memory changes.
- [Cron 5- vs 6-field parser mismatch] → the gateway only guarantees non-empty + passes through; memory is the authority and its error is shown to the user.
- [Minimum-interval safeguard (15m)] → schedules firing more often are rejected by memory; the UI copy should state the constraint rather than silently accept.
- [Token scope] → write endpoints need `agents:write`; if the server-side token lacks it, that is a one-line token-scope change, flagged during implementation.
- [Merging runs into the session list] → extra round-trip to `/agents/:id/runs` on list; mitigate by only merging agents with `trigger_type='schedule'` and keeping the default list unchanged when no filter is set.

## Migration Plan

Additive: new gateway `MemoryClient` methods, handlers, routes, and a Schedules UI, plus the session-origin field/filter. No data migration, no memory changes. Rollback = revert the gateway/UI change.

## Open Questions

- Whether the iOS session browser (`client/ios/VoiceAgent/Sessions/`) should gain the same scheduled filter now or as a follow-up.
- Whether to distinguish `voice` vs `text` conversations in `origin` (needs a conversation source; today both are on-demand and treated as `manual`).
