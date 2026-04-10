## 1. Database Migration & Entities

- [x] 1.1 Create migration `00082_acp_support.sql`: add `kb.acp_sessions` table (id, project_id, agent_name, created_at, updated_at), add `kb.acp_run_events` table (id, run_id, event_type, data JSONB, created_at), add `acp_session_id` nullable FK column to `kb.agent_runs`, add index on `agent_runs(acp_session_id)`
- [x] 1.2 Add `RunStatusCancelling AgentRunStatus = "cancelling"` to `entity.go`
- [x] 1.3 Add `ACPSession` Bun model struct to `entity.go` with table `kb.acp_sessions`
- [x] 1.4 Add `ACPRunEvent` Bun model struct to `entity.go` with table `kb.acp_run_events`
- [x] 1.5 Add `ACPSessionID` nullable UUID field to `AgentRun` struct
- [x] 1.6 Run migration against local dev database and verify schema

## 2. ACP DTOs & Status Mapping

- [x] 2.1 Create `acp_dto.go` with ACP wire types: `ACPAgentManifest`, `ACPMessage`, `ACPMessagePart`, `ACPRunObject`, `ACPSessionObject`, `ACPRunSummary`, `ACPAwaitRequest`, `ACPSSEEvent`, `TrajectoryMetadata`
- [x] 2.2 Implement `MapMemoryStatusToACP(status AgentRunStatus) string` function with the full status mapping table
- [x] 2.3 Implement `AgentDefinitionToManifest(def *AgentDefinition, status *AgentStatusMetrics) ACPAgentManifest` — slug normalization, field mapping, omitempty for optional fields
- [x] 2.4 Implement `ACPSlugFromName(name string) string` — RFC 1123 DNS label normalization (lowercase, replace non-alnum with hyphens, collapse, trim, truncate 63 chars)
- [x] 2.5 Implement `RunToACPObject(run *AgentRun, messages []AgentRunMessage, question *AgentQuestion) ACPRunObject` — translate Memory messages to ACP format, include await_request if paused
- [x] 2.6 Implement `ToolCallToTrajectoryMetadata(tc *AgentRunToolCall) TrajectoryMetadata`

## 3. Repository Layer

- [x] 3.1 Add `FindExternalAgentDefinitions(ctx, projectID) ([]*AgentDefinition, error)` — query where visibility = 'external'
- [x] 3.2 Add `FindExternalAgentBySlug(ctx, projectID, slug) (*AgentDefinition, error)` — slug lookup with visibility check
- [x] 3.3 Add `GetAgentStatusMetrics(ctx, agentDefID) (*AgentStatusMetrics, error)` — avg tokens, avg duration, success rate from runs in last 30 days
- [x] 3.4 Add `CreateACPSession(ctx, session *ACPSession) error`
- [x] 3.5 Add `GetACPSession(ctx, projectID, sessionID) (*ACPSession, error)` — project-scoped lookup
- [x] 3.6 Add `GetSessionRunHistory(ctx, sessionID) ([]*AgentRun, error)` — runs linked to session ordered by created_at
- [x] 3.7 Add `InsertACPRunEvent(ctx, event *ACPRunEvent) error`
- [x] 3.8 Add `GetACPRunEvents(ctx, runID) ([]*ACPRunEvent, error)` — ordered by created_at ascending
- [x] 3.9 Add `SetRunCancelling(ctx, runID) error` — update status to `cancelling`

## 4. ACP HTTP Handler & Routes

- [x] 4.1 Create `acp_handler.go` with `ACPHandler` struct (depends on Repository, AgentExecutor, SSE writer, logger)
- [x] 4.2 Implement `Ping` handler — `GET /acp/v1/ping` returning `{}`, no auth
- [x] 4.3 Implement `ListAgents` handler — `GET /acp/v1/agents`, requires `agents:read`, returns manifests with optional status metrics
- [x] 4.4 Implement `GetAgent` handler — `GET /acp/v1/agents/:name`, requires `agents:read`, slug lookup, 404 for non-external
- [x] 4.5 Implement `CreateRun` handler — `POST /acp/v1/agents/:name/runs`, requires `agents:write`, support mode=sync/async/stream, optional session_id validation
- [x] 4.6 Implement async run path in CreateRun — enqueue via executor, return 202 with submitted status
- [x] 4.7 Implement sync run path in CreateRun — block until completion, return 200 with final state
- [x] 4.8 Implement stream run path in CreateRun — SSE headers, background goroutine with StreamCallback publishing to channel, inline streaming on response
- [x] 4.9 Implement event persistence in StreamCallback — write each event to `kb.acp_run_events` alongside channel publication
- [x] 4.10 Implement `GetRun` handler — `GET /acp/v1/agents/:name/runs/:runId`, requires `agents:read`, agent name validation
- [x] 4.11 Implement `CancelRun` handler — `DELETE /acp/v1/agents/:name/runs/:runId`, requires `agents:write`, set cancelling, 409 for terminal states
- [x] 4.12 Implement `ResumeRun` handler — `POST /acp/v1/agents/:name/runs/:runId/resume`, requires `agents:write`, check input-required status, support mode=sync/async/stream
- [x] 4.13 Implement `GetRunEvents` handler — `GET /acp/v1/agents/:name/runs/:runId/events`, requires `agents:read`, return JSON array
- [x] 4.14 Implement `CreateSession` handler — `POST /acp/v1/sessions`, requires `agents:write`, optional agent_name validation
- [x] 4.15 Implement `GetSession` handler — `GET /acp/v1/sessions/:sessionId`, requires `agents:read`, include run history

## 5. Route Registration & Module Wiring

- [x] 5.1 Create `acp_routes.go` — register all ACP routes on Echo at `/acp/v1/` prefix with auth middleware (RequireAuth + RequireAPITokenScopes)
- [x] 5.2 Wire `ACPHandler` into `module.go` fx.Module — add fx.Provide and fx.Invoke for route registration
- [x] 5.3 Verify hot reload picks up the new routes; test `GET /acp/v1/ping` returns 200

## 6. Go SDK Client

- [x] 6.1 Create `pkg/sdk/acp/client.go` with `Client` struct initialized from SDK config (server URL + auth token)
- [x] 6.2 Implement `Ping() error`
- [x] 6.3 Implement `ListAgents() ([]ACPAgentManifest, error)`
- [x] 6.4 Implement `GetAgent(name string) (*ACPAgentManifest, error)`
- [x] 6.5 Implement `CreateRun(agentName string, req CreateRunRequest) (*ACPRunObject, error)` — handle sync/async responses
- [x] 6.6 Implement `CreateRunStream(agentName string, req CreateRunRequest) (*SSEStream, error)` — return streaming reader for SSE events
- [x] 6.7 Implement `GetRun(agentName, runID string) (*ACPRunObject, error)`
- [x] 6.8 Implement `CancelRun(agentName, runID string) (*ACPRunObject, error)`
- [x] 6.9 Implement `ResumeRun(agentName, runID string, req ResumeRunRequest) (*ACPRunObject, error)`
- [x] 6.10 Implement `GetRunEvents(agentName, runID string) ([]ACPSSEEvent, error)`
- [x] 6.11 Implement `CreateSession(req CreateSessionRequest) (*ACPSessionObject, error)`
- [x] 6.12 Implement `GetSession(sessionID string) (*ACPSessionObject, error)`
- [x] 6.13 Add typed error types: `NotFoundError`, `ConflictError`, `ForbiddenError`

## 7. CLI Commands

- [x] 7.1 Create `tools/cli/internal/cmd/acp.go` with `memory acp` cobra command group
- [x] 7.2 Implement `memory acp ping` subcommand
- [x] 7.3 Implement `memory acp agents list` subcommand with table output and `--json` flag
- [x] 7.4 Implement `memory acp agents get <name>` subcommand with formatted output and `--json` flag
- [x] 7.5 Implement `memory acp runs create <agent-name>` with `--message`, `--mode`, `--session` flags
- [x] 7.6 Implement stream mode output in `runs create` — real-time token streaming to stdout
- [x] 7.7 Implement interactive resume prompt when sync run returns `input-required`
- [x] 7.8 Implement `memory acp runs get <agent-name> <run-id>` with `--json` flag
- [x] 7.9 Implement `memory acp runs cancel <agent-name> <run-id>`
- [x] 7.10 Implement `memory acp runs resume <agent-name> <run-id>` with `--message` and `--mode` flags
- [x] 7.11 Implement `memory acp sessions create` with optional `--agent` flag
- [x] 7.12 Implement `memory acp sessions get <session-id>` with `--json` flag
- [x] 7.13 Register `acp` command in CLI root command

## 8. MCP Tools

- [x] 8.1 Add `acp-list-agents` tool definition to `GetAgentToolDefinitions()` with input schema
- [x] 8.2 Implement `ExecuteACPListAgents(ctx, projectID, args)` handler — call internal repo, return ACP manifests
- [x] 8.3 Add `acp-trigger-run` tool definition with input schema (agent_name, message, mode, session_id)
- [x] 8.4 Implement `ExecuteACPTriggerRun(ctx, projectID, args)` handler — validate external visibility, call executor, support sync/async (reject stream)
- [x] 8.5 Add `acp-get-run-status` tool definition with input schema (agent_name, run_id)
- [x] 8.6 Implement `ExecuteACPGetRunStatus(ctx, projectID, args)` handler — fetch run, validate agent name match, return ACP-mapped status

## 9. Testing & Verification

- [x] 9.1 Write unit tests for `ACPSlugFromName` — spaces, special chars, long names, edge cases
- [x] 9.2 Write unit tests for `MapMemoryStatusToACP` — all status values
- [x] 9.3 Write unit tests for message format translation (Memory → ACP)
- [x] 9.4 Write integration tests for ACP discovery endpoints (ping, list, get)
- [x] 9.5 Write integration tests for ACP run lifecycle (create sync/async, get, cancel, resume)
- [x] 9.6 Write integration tests for ACP session endpoints (create, get with history)
- [x] 9.7 Write integration tests for ACP event persistence and retrieval
- [x] 9.8 Manually test streaming mode end-to-end with `memory acp runs create --mode stream` *(noted as manual-only — requires live agent)*
- [x] 9.9 Run `task build` and `task lint` to verify no compilation or lint errors
- [x] 9.10 Run `task test` to verify no regressions in existing tests
