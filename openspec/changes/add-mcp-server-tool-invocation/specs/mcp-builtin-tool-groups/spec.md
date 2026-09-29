## ADDED Requirements

### Requirement: Builtin tool capability groups are exposed for the registry UI

The memory API SHALL expose a project-scoped endpoint `GET /api/admin/builtin-tool-groups` returning
memory's builtin tools organised by the server-owned capability taxonomy in its frozen group order.
Each group SHALL carry a stable `id`, a display `label`, an optional `description`, and its member
tool names; groups with no member tools SHALL be omitted. The endpoint SHALL require project
membership (the same middleware trio as the rest of the builtin-tools surface).

The endpoint SHALL derive membership from the project tool catalog using the same grouping code path
as the agent tool picker, so the two views cannot drift. It is a read-only catalog view: groups report
`enabled: true` with no approval policy.

#### Scenario: Groups are returned in contract order

- **WHEN** a project member requests the builtin tool groups
- **THEN** the response lists the capability groups non-empty for this project, in the taxonomy's
  frozen order, each with its label, description, and member tool names

#### Scenario: Empty groups are omitted

- **WHEN** a capability group has no member tools in the project catalog
- **THEN** that group is absent from the response

#### Scenario: Grouping shares the agent picker's code path

- **WHEN** the catalog-only grouping changes (new tool, new scope mapping)
- **THEN** both the builtin tool groups and the agent definition's `toolGroups` reflect it without
  the gateway deriving anything locally

#### Scenario: Catalog unavailable degrades gracefully

- **WHEN** the tool catalog cannot be read
- **THEN** the endpoint responds with an empty group list and the registry page falls back to the
  flat cached-tool list
