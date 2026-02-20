## Why

Currently, the ADK (Agent Development Kit) runner in Emergent uses `session.InMemoryService()` which means conversational history and state are strictly in-memory. When an agent is paused (for instance, to use the `ask_user` tool) and later resumed via the API, a new in-memory session is instantiated, causing the agent to lose its prior execution context and ephemeral workspace state. Building a `bun`-backed `session.Service` allows the ADK runner to persistently store and natively resume entire chat sessions directly from PostgreSQL, ensuring agents don't suffer from "amnesia" and can pick up exactly where they left off.

## What Changes

- Create a custom bun-based implementation of the ADK `session.Service` interface (comparable to ADK's native `gorm`-based database session service).
- Introduce new database models mapping ADK primitives (Sessions, Events, and States) to Postgres tables (`kb.adk_sessions`, `kb.adk_events`, `kb.adk_states`).
- Create a database migration for the new ADK session tables.
- Update `AgentExecutor` to inject the new `bun`-backed session service instead of `InMemoryService()`.
- Update `AgentExecutor.Resume` flow to allow ADK to natively fetch the historical event chain from the database.

## Capabilities

### New Capabilities

- `adk-database-sessions`: Provides a persistent `bun`-backed implementation of the ADK session service for tracking conversation state, actions, and memory across agent pause/resume boundaries.

### Modified Capabilities

- `agent-executor`: Requires modifications to seamlessly load persistent sessions on resume rather than generating a summarized catch-up prompt.

## Impact

- **Code:** Modifies `domain/agents/executor.go` and introduces a new module/package for `adk/session/bun` (or similar).
- **Database:** Adds new tables to the `kb` schema.
- **Agent Behavior:** Agents will now resume with full context instead of repeating work or starting with amnesia.
