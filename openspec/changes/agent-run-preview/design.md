## Context

Delegation (`agent-delegation`) spawns child runs that live in a separate `agent_runs` tree, invisible to the human chat UI. A source investigation (exp-1) confirmed Emergent Memory already persists and exposes everything needed to preview these runs: `agent_run_messages` + `agent_run_tool_calls` are written in real time during execution, and REST endpoints return them under the `agents:read` scope the gateway token already holds. No memory-service code changes are needed. See proposal.md for motivation; specs/agent-run-preview/spec.md for requirements.

## Goals / Non-Goals

**Goals:**

- Proxy memory's agent-run endpoints through the gateway and surface a run/transcript view in the UI.
- Make the delegation tree traceable (parent → children, grouped by root run).
- Show in-progress runs via polling (memory has no live message stream).

**Non-Goals:**

- No live streaming/SSE of run messages (memory does not provide one; polling is the mechanism).
- No raw ADK-event dump; we render the structured `messages` + `tool-calls` transcript, not the ADK session log.
- No writes to runs (read-only observability).

## Decisions

### D1 — Proxy memory's run endpoints through the gateway; read-only

The gateway gains `MemoryClient` methods + handlers for:
`GET /api/projects/:id/agent-runs` (list) and `/agent-runs/:id/{messages,tool-calls,steps,full}`. They reuse the existing server-side memory token (already `agents:read`-scoped). The gateway exposes these as its own `/api/agent-runs`-style routes behind the same auth as the rest of `/api/*`.

*Alternatives considered:* call memory directly from the browser — rejected, would leak the memory token to the client.

### D2 — Poll for live tail (no streaming exists)

Memory's SSE bus (`/api/events/stream`) carries only `agent_run` lifecycle events, never message text; the `/logs`/`/steps-stream` endpoints flush a snapshot once and say "clients should poll if live tailing is needed". So the UI polls `/agent-runs/:id/messages` (or `/full`) on an interval while a run's status is `running`, rendering the growing transcript. Persistence is real-time (messages/tool-calls written per step), so polling yields a correct partial view.

### D3 — Tree assembly: list runs + group by `rootRunId` client-side

Every run DTO exposes `parentRunId` / `rootRunId` / `traceId`. The gateway lists all project runs; the UI groups them by `rootRunId` and links parent → children via `parentRunId`, so a full delegation tree is reconstructible from one list call.

*Alternatives considered:* (a) `GET /adk-sessions/<rootRunId>` merged event log — rejected, it's raw ADK events not cleanly attributed per agent/run; (b) `FindChildRuns` — server-side only, no HTTP route. `GetProjectRunFull` returns only the immediate parent, so it's not a tree source.

### D4 — Mirror memory's DTOs in the gateway

Add Go structs mirroring `AgentRunDTO` (`id`, `agentId`, `status`, `parentRunId`, `rootRunId`, `traceId`, `startedAt`/timestamps, `summary`), `AgentRunMessageDTO` (`role`, `content.text` + `function_calls`/`function_responses`, `stepNumber`, `createdAt`), and `AgentRunToolCallDTO` (`toolName`, `input`, `output`, `status`). The transcript view renders message text + tool-call rows in `stepNumber`/`createdAt` order; delegation calls (`spawn_agents`/`trigger_agent`) render as "→ target agent: task" rows.

### D5 — UI surface: a Runs view off the agent/conversation

A "Runs" list (grouped by root run, parent → children indented) with a run-detail transcript. Entry point: an agent dashboard link + a top-level nav item. Reuses the existing go-daisy/templ patterns.

## Risks / Trade-offs

- [Polling load] → only poll while the run status is `running`; use a modest interval and stop on terminal status.
- [Large transcripts] → the list shows summaries (status, agent, started-at); the detail view can cap/paginate messages.
- [Spawned runs not in human chat] → this view is the dedicated surface; the human chat UI is deliberately unchanged (scope).
- [No per-agent attribution in ADK events] → avoided by rendering structured per-run messages, not the ADK session.

## Migration Plan

Additive: new gateway endpoints + UI view; no data migration, no memory changes. Rollback = revert the gateway/UI change.

## Open Questions

None that would change the specs, approach, or task breakdown.
