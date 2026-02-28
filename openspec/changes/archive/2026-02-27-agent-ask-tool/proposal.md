## Why

Agents in Emergent currently run to completion or failure with no way to request human input mid-execution. When an agent encounters ambiguity (e.g., an entity extraction agent unsure whether "Mercury" refers to the planet, the element, or the company), it must guess, skip, or fail. There is no mechanism for an agent to pause, surface a question to a user, and resume once answered.

The Cord coordination protocol (github.com/kimjune01/cord) demonstrates that agents naturally attempt to call `ask(question, options)` when they hit ambiguity -- Cord's authors added `pause`, `resume`, and `modify` tools specifically because Claude independently tried to call them before they existed. Emergent already has the infrastructure pieces needed (agent pause/resume via `AgentRun.status = "paused"`, notification system with actions/actionStatus, step-based execution with real-time persistence), but they aren't wired together into a coherent human-in-the-loop flow.

## What Changes

- New `ask_user` ADK tool that agents can call to pose a question with optional structured choices to a human
- When `ask_user` is called, the agent run pauses (`status: paused`) and a notification is created in the user's inbox with the question and response options
- New `agent_questions` database table to persist questions, link them to runs, and store responses
- New API endpoint for users to respond to agent questions (from the notification or a dedicated UI)
- When a user responds, the paused agent run is automatically resumed with the answer injected as context
- Admin UI components: question notification card with inline response buttons, agent run detail view showing question/answer history

## Capabilities

### New Capabilities

- `agent-ask-tool`: The ADK tool implementation, question persistence, pause/resume lifecycle, notification integration, response API, and admin UI for human-in-the-loop agent interaction

### Modified Capabilities

- `agent-infrastructure`: Agent runs gain a new pause reason (`awaiting_user_input`) and the resume flow is extended to support injecting user responses as context into the resumed run

## Impact

- **Backend (Go)**: New `agent_questions` table + migration, new ADK tool in `domain/agents/`, new response handler endpoint, modifications to `executor.go` resume logic
- **Frontend (React)**: New notification card component for agent questions, response UI with button/text input, agent run detail updates to show Q&A history
- **Notifications module**: New notification type `agent_question` with structured action buttons that map to response options
- **Database**: One new table (`kb.agent_questions`), no changes to existing tables beyond using existing `paused` status on `agent_runs`
- **No breaking changes**: The `ask_user` tool is opt-in per agent definition; existing agents are unaffected
