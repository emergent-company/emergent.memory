## Context

See `proposal.md` — Why. Alfred's web console (echo + templ gateway proxying the Emergent Memory REST API) has no notifications inbox or background-task monitor; the previous Memory UI had both, notifications with real-time SSE. This change adds those two surfaces on top of `add-auth-project-frame`, which already gives the gateway a signed-in session (`{access_token, refresh_token, active_project_id}`) and per-request Memory credentials. Memory (`/root/emergent.memory`, OpenAPI) already exposes the full surface: `GET /notifications` (+ counts/stats), `POST /notifications/{id}/read|unread|dismiss|resolve|snooze|unsnooze`, `DELETE /notifications/{id}`, and `GET /tasks`, `GET /tasks/counts`, `GET /tasks/{id}`, `POST /tasks/{id}/resolve|cancel`. Real-time delivery is Memory's `/events/stream` SSE endpoint (`GET /api/events/stream?projectId=...`), which emits `entity.*` events for `notification`, `document`, `chunk`, `extraction_job`, `graph_object`, `sync_job`, and `agent_run` entities.

Two scope facts drive the shape:

- **Notifications are user-scoped**: `GET /notifications` and its siblings take no `project_id` — Memory resolves them for the current user from the bearer token.
- **Tasks are project-scoped**: `GET /tasks` and `/tasks/counts` require a `project_id` query param (with an elevated `/tasks/all` for cross-project access).
- **The SSE stream is project-scoped** and requires `projectId` as a query param because browser `EventSource` cannot set the `Authorization` header.

## Goals / Non-Goals

**Goals:**

- Add notifications: list (tab/filter/search), unread counts, mark read/unread/all-read, dismiss/restore, resolve actionable notifications, with real-time updates via SSE.
- Add tasks: list (type/status filter, pagination), counts, detail, resolve (with notes), cancel.
- Scope both to the signed-in session: user token for notifications, user token + active project for tasks and the SSE stream.
- Reuse the gateway's existing SSE proxy pattern (io.Pipe + `c.Stream`), not a new transport.

**Non-Goals:**

- No snooze scheduling UI (endpoints exist; out of scope for this slice).
- No cross-project task view (`/tasks/all`) or elevated-admin surface.
- No merge-suggestion chat / merge-chat execution UI (`/tasks/{id}/merge-suggestion`, `/merge-chat/*`) — the resolve accept path is enough; full merge tooling is a separate change.
- No change to iOS or the supervisor/bridge workers.

## Decisions

### D1 — Gateway-proxied SSE for real-time notifications

The browser opens `EventSource` against a gateway endpoint (`/api/notifications/stream`); the gateway resolves the session's Memory token and active project, opens upstream `{memory}/api/events/stream?projectId=<active>`, and pipes entity events through (optionally forwarding only `entity == "notification"`). Auto-reconnect is the browser's `EventSource` default.

- **Why:** the browser cannot attach `Authorization` to `EventSource`; proxying keeps the token server-side and reuses the same origin as the rest of the console. Memory already requires `projectId` as a query param for exactly this reason. Matches the existing `/api/chat` SSE proxy in `handlers.go`.
- **Alternative rejected:** direct browser `EventSource` to Memory's `/events/stream` (exposes the Memory origin and can't carry the user token); pure polling with no stream (higher latency and constant load, loses the "real-time" requirement in the proposal).

### D2 — Notifications user-scoped, tasks project-scoped (explicit credential split)

Notification client methods send only the session bearer token. Task client methods send the session bearer token **and** the active project via `project_id` query (and `X-Project-ID` where Memory reads it from headers, mirroring `memory.go`).

- **Why:** Memory's API is split this way — notifications resolve the user from the token; tasks require an explicit project. Sending a `project_id` to the notifications endpoint (or omitting it from tasks) would break the call.
- **Alternative rejected:** treat both as project-scoped for uniformity (wrong for notifications — they are user-wide).

### D3 — Tasks refresh via polling plus SSE-triggered refetch

There is no dedicated `task` entity in Memory's SSE `EntityType` set (only `notification`, `document`, `chunk`, `extraction_job`, `graph_object`, `sync_job`, `agent_run`). The task monitor therefore loads/refreshes over REST and also refetches its list+counts when a notification or agent-run event arrives on the stream (task completions surface as notifications).

- **Why:** honest match to what Memory emits; no fabricated task stream.
- **Alternative rejected:** inventing a task event channel in the gateway (would require Memory changes, out of scope).

### D4 — Pass-through SSE (no event rewrite layer)

The gateway forwards Memory's SSE entity events as-is; the client filters by `entity`/`type`. No transform comparable to `rewriteChatStream` is needed because notification events need no re-shaping for display.

- **Why:** least code, preserves Memory's event fidelity (heartbeats, `connected`, `entity.*`).
- **Alternative rejected:** a rewrite/filter layer in Go to emit only notification events (more code, and a future notification-shape change would require a gateway change; filtering is cheap client-side).

### D5 — Reuse per-request session credentials from the auth frame

Notification and task methods are added to `MemoryClient` using the per-call token + project context introduced by `add-auth-project-frame` (D3 there), not new process-global fields.

- **Why:** a single client serves many sessions; credentials are parameters. No divergence from the auth change's model.

## Risks / Trade-offs

- **[SSE holds a long-lived upstream connection per browser]** many open inboxes → many open Memory connections → Mitigation: use the request context to cancel on disconnect, forward Memory's heartbeat, and rely on Memory's own connection cap (`/events/connections/count`); no gateway-side fan-out hub in this slice.
- **[Notifications list is user-wide but the SSE stream is project-scoped]** a user with multiple projects may see a badge that only reflects the active project's events → Mitigation: fetch counts per active project on load and on project switch; document the scoping in the spec (real-time events are for the active project).
- **[Task resolve enum mismatch]** OpenAPI resolve status enum (`pending/accepted/rejected/cancelled`) differs from the user guide's `pending/resolved/cancelled` → Mitigation: display status from the API response verbatim and treat unknown statuses as their raw string; verify against the live API during implementation.
- **[Reconnect storms on drop]** many clients retrying at once can spike Memory → Mitigation: `EventSource` default retry plus a jittered backoff in the client reconnect logic.
- **[`mark-all-read` endpoint drift]** the user guide documents `POST /notifications/mark-all-read` but the OpenAPI only lists per-item `read`/`unread` and tab-level `DELETE /notifications` → Mitigation: implement mark-all-read against the documented endpoint and fall back to per-item reads if the endpoint is absent; confirm during implementation.

## Migration Plan

1. Add notification + task client methods and the SSE proxy to `MemoryClient`/handlers behind the existing session middleware — additive, no config flag needed (unauthenticated access already redirects via the auth frame).
2. Add routes (`/api/notifications*`, `/api/tasks*`, `/api/notifications/stream`) and templ pages for the inbox and task monitor; land them disabled-by-default in nav if the auth frame is not yet fully rolled out.
3. Wire the client JS: `EventSource` for the stream, REST for lists/mutations, and task refetch on relevant events.
4. Rollback: the change is additive; removing the nav entries and routes restores the prior surface without reverting the auth frame.

## Open Questions

- Exact `mark-all-read` endpoint name/shape in the current Memory build (docs vs OpenAPI) — confirm at implementation time.
- Canonical task-status vocabulary (resolved vs accepted/rejected) and whether Memory will add a `task` SSE entity type — affects whether task real-time becomes stream-driven later.
