## Why

The web-UI blueprint gallery lists packs embedded into the gateway binary, but
the `task-board` sample (added by `add-blueprint-board-work`, #1351/#1353) was
authored only at the repo root and never copied into the gateway's embedded
`blueprints/` directory — so it does not appear in the gallery, and its README
advertises installing it from there. Worse, the gateway's manifest path is
incomplete: `apps/server/domain/blueprints/manifest.go` and
`apps/cli/internal/blueprints/types.go` gained the board/work keys, but
`apps/web-ui/gateway/blueprint_manifest.go` did not. Its `blueprintAgent` drops
`workConfig`, `triggerType`, `reactionConfig`, and `cronSchedule` (and the
sample's `dispatchMode`/`defaultQueue`/`maxSteps`), so even if embedded,
installing `task-board` through the gateway would silently drop the worker's
work/reaction configuration. The loader also omits `skills/` and `seed/`, so the
sample's promised seed `Task` and workflow skill never install.

## What Changes

- **Gateway manifest parity.** `blueprintAgent` + `buildBlueprintManifest`
  carry `workConfig`, `triggerType`, `reactionConfig`, `cronSchedule`,
  `dispatchMode`, `defaultQueue`, and `maxSteps` through to the manifest JSON,
  matching the server's `AgentManifest` shape.
- **Gateway skills/seed support.** The bundled loader reads `skills/*/SKILL.md`
  (frontmatter + Markdown) and `seed/objects/*.jsonl` +
  `seed/relationships/*.jsonl`, and the manifest carries `skills` and `seed`
  so they install through `POST /api/blueprints` instead of being dropped.
- **Embed the sample.** Copy `blueprints/task-board/` into the gateway's
  embedded `blueprints/` directory so the gallery lists it and installs a
  board-enabled `Task` type plus the reaction-triggered `task-worker` agent.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `blueprint-api`: the gateway's bundled-blueprint manifest must carry agent
  work/reaction keys and skills/seed, and the gallery must list the embedded
  `task-board` sample.

## Impact

- **Gateway** (`apps/web-ui/gateway/blueprint_manifest.go`,
  `blueprints.go`): new agent keys, skills/seed loading and manifest members,
  embedded `blueprints/task-board/`.
- **Sample** (`blueprints/task-board/`): copied into the gateway embedded dir
  (verbatim; the loader supplies name/version from the schema file).
- **Tests** (`blueprints_test.go`, `blueprint_manifest_test.go`): manifest
  round-trip for the new agent keys, bundled-list assertion for `task-board`,
  skills/seed carry-through.

## Out of Scope

- Changing the server manifest or apply path — those already carry the keys
  (#1351/#1353).
- The remaining `AgentManifest` fields the gateway never emits
  (`workspaceConfig`, `toolPolicies`, `isDefault`, `defaultTimeout`) — no
  bundled pack declares them, so nothing is silently dropped from the shipped
  sample.
