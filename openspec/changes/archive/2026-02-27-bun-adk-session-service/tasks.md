## 1. Database Schema & Models

- [x] 1.1 Create Go struct models for `ADKSession`, `ADKEvent`, and `ADKState` in the appropriate package.
- [x] 1.2 Write a new Goose SQL migration in `apps/server-go/migrations` to create `kb.adk_sessions`, `kb.adk_events`, and `kb.adk_states` tables.
- [x] 1.3 Add foreign key constraints between `kb.adk_events` -> `kb.adk_sessions` and `kb.adk_states` -> `kb.adk_sessions`.
- [x] 1.4 Add necessary database indexes (e.g., on `session_id`, `app_name`, `user_id`) for performant lookups.

## 2. ADK Session Service Implementation

- [x] 2.1 Scaffold the `session.Service` bun implementation package (`pkg/adk/session/bun` or similar).
- [x] 2.2 Implement the `Create` method to insert a new session record into the database.
- [x] 2.3 Implement the `Get` method to retrieve an existing session and hydrate its associated states and events.
- [x] 2.4 Implement the `List` and `Delete` methods as required by the `session.Service` interface.
- [x] 2.5 Implement the `AppendEvent` method utilizing `bun` transactions to ensure consistency when saving new messages.
- [x] 2.6 Implement the internal state update mechanism to persist changes to `kb.adk_states` upon events.

## 3. Agent Executor Integration

- [x] 3.1 Update `AgentExecutor` to accept the new bun-backed `session.Service` interface in its constructor/factory.
- [x] 3.2 Update `AgentExecutor.execute` (or where the runner is initialized) to initialize ADK `runner.New` with the database session service instead of `InMemoryService`.
- [x] 3.3 Ensure that `AgentExecutor.Resume` accurately loads the previous session state using the new service (so agents retain their context).
- [x] 3.4 Ensure the new database service handles the fallback/graceful transition if a prior run doesn't have an ADK session record (e.g., for backward compatibility or testing).

## 4. Testing & Verification

- [x] 4.1 Write unit tests for the `bun` session service simulating `Create`, `Get`, and `AppendEvent` workflows.
- [x] 4.2 Update existing agent executor unit tests to pass a mock or test-database version of the `session.Service`.
- [x] 4.3 Write/update an integration test that creates a paused agent and resumes it, ensuring that previous conversation context was maintained.
- [x] 4.4 Run backend linting and test suites to verify no regressions (`nx run server-go:test` and `nx run server-go:test-e2e`).
