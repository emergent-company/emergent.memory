## ADDED Requirements

### Requirement: Actor provenance filters on object list

The `memory graph objects list` command SHALL accept actor provenance filters: `--actor-type` (`user`|`agent`|`system`), `--actor-id` (a UUID), and `--provenance` (`created`|`updated`|`any`). The filters SHALL be applied as a `(actor_type, actor_id)` pair with the selected provenance mode.

#### Scenario: Agent-scoped listing

- **WHEN** the user runs `memory graph objects list --actor-type agent --actor-id <agent-uuid>`
- **THEN** the command lists objects created by or updated by that agent according to the selected `--provenance` mode (defaulting to `any`)

#### Scenario: Creator-scoped listing

- **WHEN** the user runs `memory graph objects list --actor-type agent --actor-id <agent-uuid> --provenance created`
- **THEN** the command lists only objects whose earliest surviving version was authored by that agent

### Requirement: Reject invalid provenance filter values

The command SHALL reject an invalid `--provenance` value and an invalid `--actor-type` value with a clear error rather than silently ignoring them.

#### Scenario: Invalid provenance mode rejected

- **WHEN** the user runs `memory graph objects list --provenance bogus`
- **THEN** the command returns an error indicating the accepted values (`created`, `updated`, `any`)

#### Scenario: Invalid actor type rejected

- **WHEN** the user runs `memory graph objects list --actor-type bogus`
- **THEN** the command returns an error indicating the accepted values (`user`, `agent`, `system`)
