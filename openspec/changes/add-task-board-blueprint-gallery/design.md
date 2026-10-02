## Context

`add-blueprint-board-work` (#1351/#1353) added board-enabled object types,
object-driven agent work, and seed `assignee` to the server manifest and the
CLI loader, plus a `blueprints/task-board/` sample. The web-ui gateway's
bundled-blueprint path (`apps/web-ui/gateway/blueprints.go`,
`blueprint_manifest.go`) was left at its prior shape: it embeds four packs
(`agent-notes`, `dreaming-agent`, `operator`, `personal-memory`), builds a
manifest from `BundledBlueprint`/`blueprintAgent`, and — per its own comment —
deliberately omits skills and seed. It has no idea about the new board/work
keys.

The gateway installs bundled packs via the blueprint API
(`create → publish → apply`); the manifest it POSTs is the single source of
truth for what gets materialized. So the gateway must carry every field the
sample declares, or installing from the gallery silently drops them.

## Goals / Non-Goals

**Goals:**

- The `task-board` sample appears in the gallery's available list.
- Installing it yields a board-enabled `Task` type + a reaction-triggered
  `task-worker` agent with its `workConfig`/`reactionConfig` intact.
- The sample's workflow skill and seed `Task` install too (they are part of the
  sample's "out of the box" promise), if the gateway can carry them cleanly.

**Non-Goals:**

- Server-side manifest/apply changes (already shipped).
- CLI changes (already shipped).
- Emitting `AgentManifest` fields no bundled pack uses (`workspaceConfig`,
  `toolPolicies`, `isDefault`, `defaultTimeout`).

## Decisions

- **Carry the four required keys plus the sample's extras.** `blueprintAgent`
  gains `workConfig`, `triggerType`, `reactionConfig`, `cronSchedule`,
  `dispatchMode`, `defaultQueue`, `maxSteps`. Rationale: the task mandates the
  four; the sample also declares the last three, and dropping them would
  violate "no config silently dropped". `reactionConfig` is a local
  `BundledReactionConfig` (events as `[]string`) shared by the YAML loader and
  the manifest JSON, mirroring `BundledAgentUI`'s dual-tag pattern.
- **Include skills and seed.** The server's `BlueprintManifest` already
  supports `skills` and `seed`, and the loader work is small and clean: parse
  `skills/*/SKILL.md` frontmatter (agentskills.io convention, same split as the
  CLI loader) and read `seed/objects/*.jsonl` + `seed/relationships/*.jsonl`.
  This removes the "intentionally omitted" comment and lets the sample install
  whole.
- **Copy the sample verbatim.** `blueprints/task-board/` is embedded unchanged;
  the loader already falls back name→version→description from the schema file,
  so the sample's `project:`-wrapped `project.yaml` (which the gateway's
  `bundledProjectFile` does not parse) is harmless — `schemas/task-board.yaml`
  supplies the metadata.

## Risks / Trade-offs

- **Manifest drift detection.** `resolveOrCreateBlueprint` compares checksums
  and logs (not errors) when a bundled manifest differs from a stored published
  blueprint. Changing the gateway manifest shape for the existing four packs is
  a no-op for them (they declare none of the new keys), so no version bump is
  required.
- **Skill content size.** The server validates skill `content` against
  `MaxContentSize`; the sample skill is tiny, so no risk.
- **Seed relationships.** The sample declares none; the relationship loader is
  added for completeness but stays exercised only by tests.
