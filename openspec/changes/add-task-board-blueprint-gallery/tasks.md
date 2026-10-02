# Tasks — Add task-board to the blueprint gallery

## 1. Gateway manifest parity

- [x] 1.1 Extend `BundledAgent` (`apps/web-ui/gateway/blueprints.go`) with `workConfig`, `triggerType`, `reactionConfig`, `cronSchedule`, `dispatchMode`, `defaultQueue`, and `maxSteps` (plus a `BundledReactionConfig` type). Verify: YAML loader unit test decodes them.
- [x] 1.2 Extend `blueprintAgent` + `buildBlueprintManifest` (`blueprint_manifest.go`) to emit those keys matching the server's `AgentManifest` shape, and map them back in `bundledAgentsFromManifest`. Verify: manifest round-trip unit test.

## 2. Gateway skills/seed support

- [x] 2.1 Add `BundledSkill`, `BundledSeedObject`, `BundledSeedRelationship` types and load `skills/*/SKILL.md`, `seed/objects/*.jsonl`, `seed/relationships/*.jsonl` in `loadBundledFromFS`. Verify: loader unit tests.
- [x] 2.2 Add `skills` + `seed` members to `blueprintManifest` and emit them in `buildBlueprintManifest`. Verify: manifest carry-through unit test.

## 3. Embed the sample

- [x] 3.1 Copy `blueprints/task-board/` (schema, agent, skill, seed, project.yaml, README) into `apps/web-ui/gateway/blueprints/task-board/`. Verify: each YAML parses, JSONL is one object per line.

## 4. Tests

- [x] 4.1 Gateway: bundled-list test asserts `task-board` is listed with its `Task` type and `task-worker` agent.
- [x] 4.2 Gateway: manifest round-trip asserts the new agent keys and skills/seed survive `buildBlueprintManifest`.

## 5. Verification

- [x] 5.1 `go build ./...` and `go test ./...` in `apps/web-ui/gateway`.
- [x] 5.2 `task lint` (gateway) and `openspec validate add-task-board-blueprint-gallery --strict`.
- [x] 5.3 `go build ./...` and `go vet ./...` in `apps/server` (no server change expected; confirm).
