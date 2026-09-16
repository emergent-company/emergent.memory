## 1. Gateway — run/message model + memory client

- [ ] 1.1 Add Go DTOs in `gateway/memory.go` mirroring memory: `AgentRun` (`id`, `agentId`, `status`, `parentRunId`, `rootRunId`, `traceId`, `startedAt`/`createdAt`, `summary`), `RunMessage` (`role`, `content.text`, `stepNumber`, `createdAt`), and `RunToolCall` (`toolName`, `input`, `output`, `status`, `stepNumber`).
- [ ] 1.2 Add `MemoryClient` methods: `ListAgentRuns(ctx)` → `GET /api/projects/:id/agent-runs`; `GetRunMessages(ctx, runID)` → `.../agent-runs/:id/messages`; `GetRunToolCalls(ctx, runID)` → `.../tool-calls`; `GetRunFull(ctx, runID)` → `.../full`.
- [ ] 1.3 Unit test: DTO JSON round-trip and that each client method calls the correct endpoint + parses the response envelope.

## 2. Gateway — proxy handlers + routes

- [ ] 2.1 Add handlers `listRuns`, `getRun`, `getRunMessages` (and `getRunFull`) in `gateway/handlers.go`, behind the existing API auth, returning `502` on memory errors and `404` when memory returns not-found.
- [ ] 2.2 Wire `/api/agent-runs`, `/api/agent-runs/:id`, `/api/agent-runs/:id/messages`, `/api/agent-runs/:id/full` in `gateway/main.go`.
- [ ] 2.3 Unit test (handler level, fake `MemoryBackend`): list and messages proxy correctly; missing run → `404`; memory error → `502`.

## 3. UI — runs view + transcript

- [ ] 3.1 Add a runs list page (templ) grouping runs by `rootRunId` and indenting parent → children via `parentRunId`, with status + agent + started-at per row.
- [ ] 3.2 Add a run-detail transcript view: messages in order with role, plus tool-call rows; render `spawn_agents`/`trigger_agent` calls as "→ target agent: task".
- [ ] 3.3 Add JS (app.js) to poll `/api/agent-runs/:id/messages` while a run's status is `running`, updating the transcript; stop on terminal status.
- [ ] 3.4 Unit/e2e test: run list groups by root run; transcript renders a delegation call and its child's reply.

## 4. Verify

- [ ] 4.1 `templ generate` + `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/`.
- [ ] 4.2 Smoke test against live memory: list agent runs and read one run's messages (parent + child) end-to-end.
