# 2026-09-03 — Schedule-started session resume (investigation)

## Goal
Answer whether a session spawned by a Paseo schedule can be reopened/continued manually, and how. No code changes requested — Q&A only.

## Outcome
Done. Confirmed resumability and documented the exact mechanics from live daemon state.

Findings (verified against the `Check Sentry issues` schedule `16d4a708`):
- Each schedule run is a **real, persistent agent**. The run record stores `agentId` + `workspaceId` (`paseo_inspect_schedule` → `runs[]`).
- Spawned agents keep `archiveOnFinish: false`, appear in `paseo_list_agents`, and are tagged with labels `paseo.schedule-id` and `paseo.schedule-run`.
- The agent holds a persistent OpenCode session (`snapshot.runtimeInfo.sessionId` / `persistence.sessionId`, e.g. `ses_fa1a83a09ffe68ZGOyNyv6j1Ym`), with `supportsSessionPersistence: true`.
- Resume = `send_agent_prompt(agentId, prompt)` — continues the existing session context, not a fresh agent.
- Even a **failed** run keeps its agent + session intact and resumable (run `f6e63825` → agent `54061d38`, status `idle`, run error "fetch failed").

## Decisions
- None (no product decision made this session).

## Changes
- No files changed. (Working-tree modifications in the repo belong to other parallel sessions.)

## Verification
- `paseo_list_schedules`, `paseo_list_agents`, `paseo_inspect_schedule`, `paseo_schedule_logs`, `paseo_get_agent_status` — all returned consistent live state confirming the above.

## Open questions / follow-ups
- `send_agent_prompt` is documented as "send to a running agent"; confirmed workable for `idle` schedule agents, but wake/edge behavior for an agent that errored at spawn should be confirmed before wiring UI.
- Budget-exceeded runs (e.g. run `1236041a`, 429) resume but re-hit the budget limit.
- The spec (`docs/spec/`) has **no coverage** of schedules / run history / agent-session persistence — pre-existing gap, not introduced here.

## Tasks
- [schedule-run-resume](../tasks/schedule-run-resume.md) — expose "continue/open session" from a schedule run row.
