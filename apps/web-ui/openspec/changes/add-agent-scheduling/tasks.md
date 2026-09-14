## 1. Gateway — scheduled-agent model + memory client

- [x] 1.1 Add Go DTOs in `gateway/memory.go` mirroring memory: `ScheduledAgent` (`id`, `name`, `prompt`, `cronSchedule`, `enabled`, `triggerType`, `agentDefinitionId`, `lastRunAt`, `lastRunStatus`, `consecutiveFailures`) and `ScheduledAgentRun` (`id`, `status`, `startedAt`, `completedAt`, `summary`, `triggerSource`, `errorMessage`). Verify with `go build ./...`.
- [x] 1.2 Add `MemoryClient` methods: `ListAgents`, `GetAgent`, `CreateAgent`, `UpdateAgent`, `EnableAgent`, `DeleteAgent`, `TriggerAgent`, `ListAgentRuns` hitting `/api/projects/:id/agents[/...]`. Verify with `go build ./...`.
- [x] 1.3 Unit test (TDD): DTO JSON round-trip and each client method calls the correct endpoint and parses the response envelope. Verify with `go test ./...` in `gateway/`.

## 2. Gateway — proxy handlers + routes

- [x] 2.1 Add handlers `listScheduledAgents`, `getScheduledAgent`, `createScheduledAgent`, `updateScheduledAgent`, `enableScheduledAgent`, `deleteScheduledAgent`, `triggerScheduledAgent`, `listScheduledAgentRuns` in `gateway/handlers.go`, behind existing API auth, returning `502` on memory errors and `404` on not-found. Verify with `go build ./...`.
- [x] 2.2 Wire `/api/schedules` (list/create) and `/api/schedules/:id[/enable|/trigger|/runs]` in `gateway/main.go`. Verify with `go build ./...`.
- [x] 2.3 Handler unit test (fake `MemoryBackend`): list/create/update/delete/enable/trigger proxy correctly; missing agent → `404`; memory error → `502`. Verify with `go test ./...`.

## 3. Gateway — session origin + scheduled filter

- [x] 3.1 Add an `origin` field to `sessionSummary` in `gateway/client_api.go` and populate it: `manual` for conversations, `scheduled` for runs merged from scheduled agents (via `ListAgentRuns` for agents with `triggerType=="schedule"`). Verify with `go build ./...`.
- [x] 3.2 Add an `?origin=` query param to `listSessions` that filters the merged session list. Verify with `go build ./...`.
- [x] 3.3 Unit test (TDD): `origin` is populated per session; `?origin=scheduled` returns only scheduled sessions; unknown origin returns empty, not an error. Verify with `go test ./...`.

## 4. UI — Schedules view + session filter

- [x] 4.1 Add a Schedules list page (`schedules.templ`) showing each scheduled agent's name, prompt, cron schedule, enabled state, and last-run status, with create/edit/delete actions. Verify with `templ generate` + browser smoke test.
- [x] 4.2 Add a create/edit form (name, prompt, cron expression, enabled toggle, optional agent-definition selector) following the agent-settings panel pattern. Verify with `templ generate` + browser smoke test.
- [x] 4.3 Add manual trigger action and render the last-run status/error. Verify with browser smoke test.
- [x] 4.4 Add an origin/type filter (with a "Scheduled" option) to the session browser, mirroring the chat-rail agent filter. Verify with browser smoke test.
- [x] 4.5 UI test: schedules list renders agents and empty state; the session filter shows only scheduled sessions when selected. Verify with `go test ./...`.

## 5. Verify

- [x] 5.1 `templ generate` + `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/`. Verify all pass.
- [ ] 5.2 Smoke test against live memory: create a scheduled agent, trigger it manually, confirm the run appears as a `scheduled` session and the filter isolates it.
