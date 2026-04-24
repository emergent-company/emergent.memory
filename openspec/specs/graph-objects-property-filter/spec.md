# Spec: Graph Objects Property Filter

## Purpose

Enable server-side JSONB property filtering on the `memory graph objects list` command via `--filter` and `--filter-op` flags, so users can narrow object listings by property values without post-processing.

## Requirements

### Requirement: Property equality filter flag
The `memory graph objects list` command SHALL accept a repeatable `--filter <key>=<value>` flag that applies server-side JSONB property filtering. Multiple `--filter` flags SHALL be combined with AND logic. If `=` is absent, the command SHALL return an error: `--filter: expected key=value format`.

#### Scenario: Single equality filter
- **WHEN** user runs `memory graph objects list --type Assumption --filter status=invalidated`
- **THEN** the API is called with `property_filters=[{"path":"status","op":"eq","value":"invalidated"}]`
- **THEN** only objects whose `properties.status` equals `"invalidated"` are returned

#### Scenario: Multiple filters (AND)
- **WHEN** user runs `memory graph objects list --type Feature --filter status=active --filter inertia_tier=1`
- **THEN** the API is called with two property filter entries, both with `op: "eq"`
- **THEN** only objects matching both conditions are returned

#### Scenario: Invalid filter format
- **WHEN** user runs `memory graph objects list --filter invalidformat`
- **THEN** the command exits with a non-zero code and prints `--filter: expected key=value format`

### Requirement: Property filter operator flag
The `memory graph objects list` command SHALL accept an optional `--filter-op <op>` flag that sets the operator for all `--filter` values in the same invocation. Supported operators SHALL be: `eq` (default), `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `in`, `exists`. An unrecognised operator SHALL cause the command to exit with a non-zero code.

#### Scenario: Non-equality operator
- **WHEN** user runs `memory graph objects list --filter inertia_tier=3 --filter-op gte`
- **THEN** the API is called with `property_filters=[{"path":"inertia_tier","op":"gte","value":"3"}]`
- **THEN** objects with `inertia_tier >= 3` are returned

#### Scenario: Contains operator
- **WHEN** user runs `memory graph objects list --filter name=auth --filter-op contains`
- **THEN** the API is called with `property_filters=[{"path":"name","op":"contains","value":"auth"}]`

#### Scenario: Exists operator (value ignored)
- **WHEN** user runs `memory graph objects list --filter deprecated=any --filter-op exists`
- **THEN** the API is called with `property_filters=[{"path":"deprecated","op":"exists"}]`
- **THEN** only objects that have the `deprecated` property set are returned

#### Scenario: Invalid operator
- **WHEN** user runs `memory graph objects list --filter status=x --filter-op fuzzy`
- **THEN** the command exits with a non-zero code and prints `--filter-op: unsupported operator "fuzzy"`

#### Scenario: In operator with comma-separated values
- **WHEN** user runs `memory graph objects list --filter status=active,draft --filter-op in`
- **THEN** the API is called with `property_filters=[{"path":"status","op":"in","value":["active","draft"]}]`

### Requirement: Filter flags composable with existing flags
The `--filter` and `--filter-op` flags SHALL compose with all existing `memory graph objects list` flags (`--type`, `--limit`, `--label`, `--output`, `--json`).

#### Scenario: Combined with type and limit
- **WHEN** user runs `memory graph objects list --type Feature --filter track=product --limit 10`
- **THEN** the API request includes both `type=Feature` and `property_filters=[{"path":"track","op":"eq","value":"product"}]` with `limit=10`

#### Scenario: JSON output still works
- **WHEN** user runs `memory graph objects list --filter status=active --json`
- **THEN** the response is printed as JSON and the filter is applied
