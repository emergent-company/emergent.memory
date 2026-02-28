## Why

Now that we have successfully introduced the `bun`-backed ADK session service, agents persistently log their conversational context and runtime states to the database (`kb.adk_sessions`, `kb.adk_events`). However, this raw session data is currently inaccessible to end-users and developers through standard interfaces. To enable deep debugging, conversation inspection, and future capabilities like cross-session continuation, we must expose these persistent ADK sessions through the Emergent API, Go SDK, and CLI tools.

## What Changes

- Add new REST API endpoints to list and retrieve raw ADK sessions and their associated events.
- Update the Go SDK to support querying ADK sessions.
- Update the Emergent CLI to allow fetching ADK sessions and dumping their event histories.

## Capabilities

### New Capabilities

- `adk-session-api`: Exposes ADK session history via the backend API, allowing users to query raw chat threads, state variables, and model interactions that the agent experienced.
- `adk-session-sdk`: Adds methods to the Go SDK for interacting with the new ADK session endpoints.
- `adk-session-cli`: Adds CLI commands to list and inspect ADK sessions directly from the terminal.

## Impact

- **API:** Adds new read-only endpoints (e.g., `GET /api/projects/:projectId/adk-sessions`).
- **SDK:** Extends `agents.Client` or adds a new `sessions` client.
- **CLI:** Adds a new `sessions` command group (e.g., `emergent-cli sessions list`).
- **Database:** No schema changes; only read operations against existing `kb.adk_sessions` and `kb.adk_events` tables.
