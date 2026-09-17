# Tool groups cross-lane fixture

`tool-groups.golden.json` is the serialized `toolGroups` payload produced by the
server's agent-definition read DTO (`AgentDefinition.toolGroups`, computed via
`AgentDefinition.ToolGroupsWithCatalog`). The gateway parses this exact file in a
test to prove the two lanes agree on the group shape and membership semantics.

## How it is produced

The file is **generated from code, not hand-written**. The server test

```
go test ./domain/agents/ -run TestToolGroupsGoldenFixture -updateGolden
```

regenerates it from a representative definition (`goldenDefinition`) and tool
catalog (`goldenCatalog`) in
`apps/server/domain/agents/tool_group_dto_test.go`. The same test (run without
`-updateGolden`) re-marshals the DTO and asserts byte-for-byte equality with the
file, so it cannot silently drift.

## Representative definition encoded

The fixture is the `toolGroups` array for an agent definition that exercises the
full contract:

- **several members per group** — `graph-read` and `graph-write` each carry
  multiple catalog tools (full membership, including tools the agent has not
  enabled, e.g. `entity-query`, `entity-update`).
- **one group fully disabled and banned** — `schema-write`
  (`schema-create`, `schema-delete`) has `enabled: false` yet still appears so it
  can be re-enabled.
- **one group with a stored `ask` policy** — `documents` has `policy: "ask"`
  (stored as `"@group:documents": {"confirm": true}`).
- **one member with an explicit per-tool override** — `entity-create` carries a
  per-tool entry in the agent's `toolPolicies` (not visible in `toolGroups`,
  which only reports the group-level `policy`).
- **one relay-style name in `def.Tools`** — `relay1_reminders_list` appears in
  the `other` group (external/relay tools are deferred and grouped by name).

## Shape contract

Each group entry carries the frozen D6 fields in this order:

```
{ "id", "label", "description", "policy", "enabled", "tools" }
```

`policy` is always present — `""` means "inherit the default"; `"allow"`, `"ask"`,
or `"deny"` otherwise. `enabled` is `true` when at least one member tool is in
the agent's `Tools` and not in `BannedTools`. Groups with zero members are
omitted entirely.
