## 1. DTO & Entity Definitions

- [x] 1.1 Create `ADKSessionDTO` and `ADKEventDTO` structs in `apps/server-go/domain/agents/dto.go`.

## 2. Repository Layer

- [x] 2.1 Add `ListADKSessions(ctx, projectID)` to `domain/agents/repository.go` joining `kb.adk_sessions` with `kb.agent_runs` to ensure tenant isolation.
- [x] 2.2 Add `GetADKSessionWithEvents(ctx, projectID, sessionID)` to `domain/agents/repository.go` returning both session and ordered events.

## 3. API Handler & Routing

- [x] 3.1 Implement `GetADKSessions` in `domain/agents/handler.go` mapping to `GET /api/projects/:projectId/adk-sessions`.
- [x] 3.2 Implement `GetADKSessionByID` in `domain/agents/handler.go` mapping to `GET /api/projects/:projectId/adk-sessions/:sessionId`.
- [x] 3.3 Register routes in `domain/agents/routes.go` ensuring proper auth scopes.
- [x] 3.4 Regenerate OpenAPI swagger spec (`make swagger` or similar).

## 4. Go SDK

- [x] 4.1 Update `apps/server-go/pkg/sdk/agents/client.go` to include the new data types (`ADKSession`, `ADKEvent`).
- [x] 4.2 Implement `ListADKSessions(ctx, projectID)` in the SDK.
- [x] 4.3 Implement `GetADKSession(ctx, projectID, sessionID)` in the SDK.

## 5. CLI Implementation

- [x] 5.1 Create `tools/emergent-cli/internal/cmd/adksessions.go` and register the `adk-sessions` parent command.
- [x] 5.2 Implement `emergent-cli adk-sessions list --project-id <id>` using the SDK.
- [x] 5.3 Implement `emergent-cli adk-sessions get <id> --project-id <id>` using the SDK, formatting events elegantly.

## 6. Testing

- [x] 6.1 Add E2E tests for the new API endpoints in `apps/server-go/tests/e2e/`.
- [x] 6.2 Add unit tests for the Go SDK methods.
- [x] 6.3 Add mock tests for the CLI commands.
