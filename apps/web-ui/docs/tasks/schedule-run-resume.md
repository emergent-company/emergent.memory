# Schedule run — continue/open the underlying session

**Status:** proposed
**Created:** 2026-09-03
**Source:** [2026-09-03-schedule-session-resume](../sessions/2026-09-03-schedule-session-resume.md)

## What
In the schedules table, add a per-run affordance ("Continue" / "Open session") that reopens or resumes the agent session a schedule run spawned.

## Why
Every run record already stores `agentId` + `workspaceId`, and the spawned agent persists (`archiveOnFinish: false`) tagged with `paseo.schedule-id` / `paseo.schedule-run`. The agent's session (`sessionId`) is persistent, so a user can jump back in and steer the run's context manually — but no UI path exists to do so today.

## Depends on
- none (data already available in run records)

## Notes
- Resume mechanism: `send_agent_prompt(agentId, prompt)` continues the existing session (not a fresh agent).
- Edge cases to confirm before shipping: resuming an agent whose run `failed` at spawn (agent may be `idle` with no session activity), and `429` budget-exceeded runs (resume re-hits budget).
- May overlap with in-flight parallel work on the schedules table; reconcile scope before implementing.
