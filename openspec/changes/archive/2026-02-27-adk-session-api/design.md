## Context

In the previous change (`bun-adk-session-service`), we implemented a persistent database-backed ADK session service. Agents now automatically save their `session.Session` metadata, `session.State` objects, and `session.Event` chains to PostgreSQL (`kb.adk_sessions`, `kb.adk_states`, `kb.adk_events`).

However, there is currently no way for users, developers, or external systems to read this data. The data remains locked in the database, viewable only via raw SQL queries. To unlock the value of this persistence—for debugging doom loops, auditing agent reasoning, or building future resume/continue UIs—we must expose this data via standard API boundaries, our Go SDK, and our CLI.

## Goals / Non-Goals

**Goals:**

- Provide read-only API endpoints for listing ADK sessions within a project.
- Provide a read-only API endpoint for fetching a specific ADK session and its events.
- Expose corresponding methods in the Go SDK (`GetADKSession`, `ListADKSessions`).
- Add commands to the Emergent CLI to easily fetch and inspect session data (`emergent-cli sessions list`, `emergent-cli sessions get <id>`).

**Non-Goals:**

- Creating/updating/deleting ADK sessions via the API. (ADK sessions are strictly managed by the agent executor lifecycle).
- Rendering these sessions in the frontend UI. (This change is strictly for the backend API, SDK, and CLI; UI integration can follow in a subsequent change).
- Restructuring the ADK Event payload. We will serve the ADK Event JSON exactly as stored.

## Decisions

**Decision 1: Endpoint Scoping**
_Rationale:_ ADK sessions belong to an application (`appName="agents"`) and a user (`userID="system"` for agents). While the underlying tables do not explicitly have a `project_id`, they are conceptually linked to `agent_runs`. However, to keep it simple and aligned with standard API structure, we will expose them via project-scoped routes.
Actually, looking closely at `kb.adk_sessions`, there is no `project_id`. The simplest approach to map them is to expose a generic `GET /api/admin/adk-sessions` requiring `admin:read` scopes, or we join with `agent_runs` to verify project access. Given that agents execute on behalf of projects, we must restrict access to project members.
_Refined Decision:_ We will query `kb.adk_sessions` and join with `kb.agent_runs` (where `agent_runs.id = adk_sessions.id` or via `resumed_from` chains) to enforce project-level tenant isolation, exposing them under `GET /api/projects/:projectId/adk-sessions`. If the IDs diverge, we may need a lightweight mapping or just expose them globally to admins first.
_Alternative:_ Expose strictly as an admin-only endpoint (`/api/admin/adk-sessions`). We will choose the Project-scoped approach and join on `agent_runs` to ensure tenant safety, as users need to debug their own agents.

**Decision 2: SDK/CLI grouping**
_Rationale:_ Instead of overloading the `agents` CLI command, we will introduce a new top-level `sessions` (or `adk-sessions`) CLI group to distinctly represent raw ADK session data. E.g., `emergent-cli adk-sessions list --project-id <id>`.

## Risks / Trade-offs

- **Risk:** **Payload Size.** ADK events contain full LLM prompts, tool definitions, and tool results. Fetching a session with 100 events could result in a multi-megabyte JSON payload.
  - **Mitigation:** We will return the `session` object without events in the list endpoint. The `Get` endpoint will return events, and we will rely on pagination/offset limits if the payload size becomes a systemic issue (though for debugging, fetching the full blob is often desired).
- **Risk:** **Tenant Isolation.** `kb.adk_sessions` lacks a `project_id` column.
  - **Mitigation:** The repository layer MUST perform an `EXISTS` check or a `JOIN` against `kb.agent_runs` (and subsequently `kb.agents`) to verify the session belongs to an agent residing within the requested `project_id`.
