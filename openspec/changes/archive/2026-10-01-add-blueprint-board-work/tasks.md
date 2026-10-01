# Tasks — Blueprint board work

## 1. Server manifest + apply

- [x] 1.1 Extend `apps/server/domain/blueprints/manifest.go`: add `boardEnabled`, `allowedStatuses`, `skipEmbeddings`, `skipExtraction`, `excludeFromSearch` to the object-type manifest; add `workConfig`, `triggerType`, `reactionConfig`, `cronSchedule` to the agent manifest; add `assignee` to the seed-object record. Verify: manifest decode/round-trip unit tests.
- [x] 1.2 Extend `apps/server/domain/blueprints/apply.go` so the new keys are carried into the schema pack / agent definition / seed object create-or-update path without being dropped. Verify: apply unit tests assert the fields survive.
- [x] 1.3 Verify `go build ./...` and `go test ./...` in `apps/server`.

## 2. CLI parity

- [x] 2.1 Add the same keys to `apps/cli/internal/blueprints/types.go` (pack object types, agent file, seed record) so the directory loader does not drop them. Verify: loader unit tests.

## 3. Sample blueprint

- [x] 3.1 Author `blueprints/task-board/` — `project.yaml`, `schemas/task-board.yaml` (board-enabled `Task` + `blocks` relationship), `agents/task-worker.yaml` (reaction-triggered, work contract), `skills/task-workflow/SKILL.md`, `seed/objects/Task.jsonl`, `README.md`. Verify: each YAML parses and the JSONL is one object per line.

## 4. Provenance guard

- [x] 4.1 In `apps/web-ui/gateway/objects.go`, normalize the `/objects` provenance filter (incomplete actor pair or unrecognised value → `any`) before dispatch, and in `objects.templ` render the provenance control only when a complete actor pair is selected. Verify: gateway render/handler tests + `go build ./...` + `go test ./...`.

## 5. Board e2e

- [x] 5.1 Add an e2e test that seeds a board-enabled Task through the memory API, asserts it renders in the `ready` lane, opens the card drawer, and exercises approve (`review` → `done`), retry (`blocked` → `ready`), reassign, and cancel (→ `blocked`). Verify: Playwright e2e run (type-check + `--list` discovery at minimum).

## 6. Verification

- [x] 6.1 `go build ./...` and `go test ./...` in `apps/server`.
- [x] 6.2 `go build ./...` and `go test ./...` in `apps/web-ui/gateway` (after `templ generate`).
- [x] 6.3 `PATH="/root/go/bin:$PATH" task lint`.
- [x] 6.4 `openspec validate add-blueprint-board-work --strict`.
