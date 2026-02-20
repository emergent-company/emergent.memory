## 1. Database Migration

- [x] 1.1 Create Goose migration for `kb.agent_questions` table with columns: `id`, `run_id`, `agent_id`, `project_id`, `question`, `options` (JSONB), `response`, `responded_by`, `responded_at`, `status`, `notification_id`, `created_at`, `updated_at`
- [x] 1.2 Add foreign key indexes on `run_id`, `agent_id`, and composite index on `(project_id, status)` for the pending-questions query
- [x] 1.3 Run migration and verify table exists with `\d kb.agent_questions`

## 2. Entity and Repository

- [x] 2.1 Add `AgentQuestion` Bun entity struct in `domain/agents/entity.go` with all columns, JSON tags, and Bun annotations
- [x] 2.2 Add `AgentQuestionStatus` type constants: `pending`, `answered`, `expired`, `cancelled`
- [x] 2.3 Add `AgentQuestionOption` struct for the options JSONB: `Label`, `Value`, `Description`
- [x] 2.4 Add repository methods in `domain/agents/repository.go`: `CreateQuestion`, `FindQuestionByID`, `FindPendingQuestionsByRunID`, `CancelPendingQuestionsForRun`, `AnswerQuestion`, `ListQuestionsByRunID`, `ListQuestionsByProject`
- [x] 2.5 Add `ToolNameAskUser = "ask_user"` constant and register it in the tool name maps (alongside `spawn_agents`, `list_available_agents`)

## 3. Ask User ADK Tool

- [x] 3.1 Create `domain/agents/ask_user_tool.go` with `AskUserToolDeps` struct (repo, logger, projectID, agentID, runID, pauseFlag)
- [x] 3.2 Implement `BuildAskUserTool()` that creates a `functiontool.New` with parameters: `question` (string, required), `options` (array of {label, value}, optional)
- [x] 3.3 In the tool handler: cancel any existing pending questions for the run, create the new `AgentQuestion` record, create a notification (insert into `kb.notifications` via the agents repo), set the pause flag, and return `{question_id, status: "paused", message: "..."}`
- [x] 3.4 Define the pause signaling mechanism: add an `askPauseRequested` atomic bool field to a per-run context struct passed through `AskUserToolDeps`

## 4. Executor Integration

- [x] 4.1 Add pause flag check to `beforeModelCb` in `executor.go`: after the step-limit check, check if `askPauseRequested` is set, and if so call `repo.PauseRun()` with summary `{reason: "awaiting_user_input", question_id: "..."}` and return a synthetic LLM response
- [x] 4.2 Add `buildAskUserTool()` method to `AgentExecutor` that checks if the agent definition's `tools` array contains `"ask_user"` and builds/returns the tool
- [x] 4.3 Call `buildAskUserTool()` in `runPipeline()` after coordination tools are added, appending the ask_user tool to `resolvedTools` if applicable
- [x] 4.4 Pass the pause flag and question ID through the tool deps so `beforeModelCb` can access them

## 5. Response API and Resume Logic

- [x] 5.1 Add `RespondToQuestionRequest` DTO in `domain/agents/dto.go` with `Response` field
- [x] 5.2 Add `HandleRespondToQuestion` handler method in `domain/agents/handler.go`: validate question belongs to project, check `status: pending`, check run `status: paused`, update question, update notification `actionStatus`, build `ExecuteRequest` with Q&A user message, launch `executor.Resume()` in goroutine, return `202 Accepted`
- [x] 5.3 Register route `POST /api/projects/:projectId/agent-questions/:questionId/respond` in `domain/agents/routes.go` with appropriate auth middleware
- [x] 5.4 Construct the resume `UserMessage` with format: `Previously you asked: "..." / The user responded: "..." / Continue from where you left off.`

## 6. Question List API

- [x] 6.1 Add `HandleListQuestionsByRun` handler: `GET /api/projects/:projectId/agent-runs/:runId/questions` returning questions ordered by `created_at`
- [x] 6.2 Add `HandleListQuestionsByProject` handler: `GET /api/projects/:projectId/agent-questions` with optional `status` query filter
- [x] 6.3 Register both routes in `domain/agents/routes.go` under project-scoped agent runs

## 7. Notification Integration

- [x] 7.1 Add `CreateNotification` method to the agents repository that inserts directly into `kb.notifications` (following existing cross-domain insert patterns)
- [x] 7.2 Build notification payload in the `ask_user` tool: set `type: agent_question`, `sourceType: agent_run`, `sourceID: runID`, `relatedResourceType: agent_question`, `relatedResourceID: questionID`, `importance: important`, map options to `actions` JSONB array
- [x] 7.3 For open-ended questions (no options), set `actionURL` to `/agents/questions/{questionId}` instead of populating `actions`
- [x] 7.4 Store the notification ID back on the `agent_question` record for linking

## 8. Admin UI - Notification Question Card

- [x] 8.1 Create `AgentQuestionNotification` component in `apps/admin/src/components/organisms/` that renders a question card with the agent name, question text, and response controls
- [x] 8.2 Render option buttons when `actions` array is populated -- each button calls `POST .../respond` with the option value via `useApi`
- [x] 8.3 Render a text input with submit button when `actions` is empty (open-ended question)
- [x] 8.4 Show loading/success/error states after responding; disable buttons after response is sent
- [x] 8.5 Integrate the component into the notification list: detect `type: agent_question` notifications and render `AgentQuestionNotification` instead of the default card

## 9. Admin UI - Run Detail Q&A History

- [x] 9.1 Add `useAgentRunQuestions` hook that fetches `GET /api/projects/:projectId/agent-runs/:runId/questions` via `useApi`
- [x] 9.2 Create `AgentRunQAHistory` component that displays a timeline of questions with status badges (pending/answered/cancelled) and responses
- [x] 9.3 Integrate `AgentRunQAHistory` into the agent run detail page (in the existing run detail view)

## 10. Backend Tests

- [x] 10.1 Write E2E test: create an agent definition with `ask_user` in tools, trigger execution with a prompt that causes the agent to call `ask_user`, verify the run pauses and question record is created
- [x] 10.2 Write E2E test: respond to a pending question via the API, verify the question is updated to `answered`, and a new resumed run is created
- [x] 10.3 Write E2E test: respond to an already-answered question, verify `409 Conflict`
- [x] 10.4 Write E2E test: respond to a question while run is still `running`, verify `409 Conflict`
- [x] 10.5 Write unit tests for repository methods: `CreateQuestion`, `CancelPendingQuestionsForRun`, `AnswerQuestion`, `ListQuestionsByRunID`
- [x] 10.6 Write unit test for `BuildAskUserTool` verifying it creates a valid ADK tool with correct parameter schema
