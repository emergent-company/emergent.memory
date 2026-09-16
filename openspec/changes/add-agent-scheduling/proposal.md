## Why

The agent can only run on-demand today: a user chats in the web UI or talks over LiveKit. There is no way to run it automatically on a recurring schedule, so standing jobs (daily briefing, nightly cleanup, periodic research) require a human to kick off each run. Emergent Memory already runs background agents on a cron schedule server-side (`kb.agents` + a `TriggerService` over `robfig/cron`), but the gateway never exposes that, so Alfred users cannot create, edit, or see scheduled runs.

## What Changes

- The gateway proxies Emergent Memory's runtime-agent endpoints (`/api/projects/:id/agents`), so a user can create and edit a **scheduled agent** — a prompt plus a cron schedule — without touching the memory admin UI.
- A new "Schedules" surface in the gateway lists scheduled agents and lets the user create/edit a schedule (prompt, cron expression, enabled toggle, optional link to an existing agent definition) and trigger one manually.
- Scheduled runs are surfaced with a distinguishable **origin** (scheduled vs. manual vs. voice/chat), and the session/run browser gains a filter so a user can show only scheduled sessions.
- No Emergent Memory code changes — scheduling, cron registration, and run recording already exist behind the `agents:read`/`agents:write` scopes the gateway token holds.

## Capabilities

### New Capabilities

- `agent-scheduling`: the ability to create and manage scheduled agents (a prompt plus a cron schedule), run them on the configured schedule, and filter the session/run list to show only scheduled sessions.

### Modified Capabilities

- `session-log-api`: the session list gains an origin/source dimension and a filter so scheduled sessions can be distinguished and isolated from manual/voice sessions.

## Impact

- Gateway (`gateway/`): new `MemoryClient` methods + handlers for `/api/projects/:id/agents` (list, get, create, update, enable, delete, trigger, list runs) and the run list; new `ScheduledAgent`/`AgentRun` DTOs mirrored from memory; a Schedules UI (list + form) reusing the existing go-daisy/templ patterns; a session-origin filter on the session browser.
- Session model: `sessionSummary` gains an `origin`/`source` field so the UI can filter by scheduled vs. manual.
- iOS (`client/ios/VoiceAgent/Sessions/`): session browser gains the scheduled filter, mirroring the web surface (optional follow-up).
- No Emergent Memory service changes and no database migration in the gateway (memory is the source of truth; the gateway reads/writes it over HTTP).
