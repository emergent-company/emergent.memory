## ADDED Requirements

### Requirement: Gateway bundled-blueprint manifests carry agent work and reaction config

The web-ui gateway SHALL build a bundled blueprint's manifest with the same
agent work/reaction keys the server's `AgentManifest` accepts — `workConfig`,
`triggerType`, `reactionConfig`, `cronSchedule`, `dispatchMode`, `defaultQueue`,
and `maxSteps` — so installing a bundled pack through `POST /api/blueprints`
does not drop a worker agent's object-driven-work wiring.

#### Scenario: Worker agent work and reaction config survives

- **WHEN** a bundled blueprint agent declares `workConfig`, `triggerType`,
  `reactionConfig`, `cronSchedule`, `dispatchMode`, `defaultQueue`, and
  `maxSteps`
- **THEN** `buildBlueprintManifest` emits all those keys in the agent manifest
  JSON, matching the server's `AgentManifest` shape, and `POST /api/blueprints`
  accepts them without dropping any

### Requirement: Gateway bundled-blueprint manifests carry skills and seed

The web-ui gateway SHALL carry a bundled blueprint's `skills/` and `seed/`
contents into its manifest — skills plus seed objects and relationships — so a
bundled pack's workflow skill and keyed seed objects install out of the box
rather than being omitted.

#### Scenario: Workflow skill carried

- **WHEN** a bundled blueprint has a `skills/<name>/SKILL.md` whose frontmatter
  declares `name` and `description`
- **THEN** the gateway manifest lists that skill with its name, description,
  and Markdown body as `content`

#### Scenario: Seed object with assignee carried

- **WHEN** a bundled blueprint has a `seed/objects/<Type>.jsonl` holding a
  keyed, board-enabled object with an `assignee`
- **THEN** the gateway manifest lists that seed object with its `assignee`
  intact, so it routes to the matching listener on apply

### Requirement: Gallery lists the task-board sample

The web-ui gateway SHALL embed the `task-board` sample blueprint, so the
available-blueprints list surfaces it alongside the other bundled packs and
installs a board-enabled `Task` type plus the reaction-triggered
`task-worker` agent.

#### Scenario: task-board surfaced and installs correctly

- **WHEN** the gateway lists embedded blueprints
- **THEN** `task-board` appears with its `Task` object type and `task-worker`
  agent, and installing it produces a board-enabled `Task` type and a
  `task-worker` agent carrying its `workConfig` and `reactionConfig`
