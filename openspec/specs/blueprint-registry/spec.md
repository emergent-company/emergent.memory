# blueprint-registry Specification

## Purpose
TBD - created by archiving change registry-blueprints. Update Purpose after archive.

## Requirements

### Requirement: Blueprint provenance is explicit

A blueprint SHALL carry an `origin` of `builtin`, `user`, or `import`. Newly created blueprints SHALL
default to `user`; GitHub-imported blueprints SHALL be `import`; platform-delivered blueprints SHALL
be `builtin`. The origin SHALL be returned by the blueprint read endpoints alongside the existing
blueprint fields.

#### Scenario: Created blueprint is user-origin

- **WHEN** a client creates a blueprint draft
- **THEN** the stored blueprint has `origin="user"` and the read endpoints return it

#### Scenario: Imported blueprint is import-origin

- **WHEN** a blueprint is imported from a GitHub URL
- **THEN** the stored blueprint has `origin="import"`

### Requirement: Built-in blueprints are seeded into the global registry at startup

The server SHALL seed the platform's built-in packs (currently `agent-notes`, `dreaming-agent`,
`operator`, `task-board`) as global, published blueprints (`project_id` NULL, `origin="builtin"`) at
startup, idempotently by `(name, version)`. Re-running the seeder on a later boot SHALL update the
existing rows rather than create duplicates.

#### Scenario: Built-ins exist after startup

- **WHEN** the server starts
- **THEN** a global published blueprint with `origin="builtin"` exists for each built-in pack

#### Scenario: Seeding is idempotent

- **WHEN** the server starts a second time with the same pack definitions
- **THEN** no duplicate built-in blueprint rows are created

### Requirement: Built-in schema packs are global and shared; user packs are project-scoped

Applying a built-in blueprint SHALL materialize a single global schema pack (`project_id` NULL,
`source="builtin"`) shared by every project that installs it. Applying a `user` or `import` blueprint
SHALL materialize a project-scoped pack, and SHALL NOT reuse another project's pack.

#### Scenario: Built-in pack is shared

- **WHEN** two projects install the same built-in blueprint
- **THEN** both projects are assigned the same global `source="builtin"` pack

#### Scenario: User pack is not shared across projects

- **WHEN** project A applies a user or imported blueprint and project B applies a blueprint of the
  same name and version
- **THEN** project B gets its own project-scoped pack and does not reuse project A's pack

### Requirement: Remove a user or imported blueprint from the registry

The registry SHALL allow deleting a project-private (`user`/`import`) blueprint by id. A global
built-in blueprint SHALL NOT be deletable. A blueprint with an active application for the caller's
project SHALL NOT be deletable, and the attempt SHALL fail with a conflict that tells the caller to
uninstall it first. A successful removal SHALL clear the blueprint's pack claims and delete
project-owned schema packs that are no longer referenced by any claim or assignment.

#### Scenario: Unapplied user blueprint is removed

- **WHEN** a client deletes an unapplied project-private blueprint
- **THEN** the blueprint row and its now-orphaned owned schema packs are removed

#### Scenario: Applied blueprint is not removed

- **WHEN** a client deletes a project-private blueprint that is currently applied
- **THEN** the request fails with a conflict and nothing is deleted

#### Scenario: Built-in is immutable

- **WHEN** a client deletes a global built-in blueprint
- **THEN** the request fails and the blueprint is unchanged

### Requirement: GitHub import is idempotent for an existing name and version

When an import targets a `(name, version)` that already exists in the caller's scope, the server
SHALL adopt the existing blueprint — refreshing it when it is a draft, reusing it when published —
and then publish and apply as requested, instead of failing with a conflict.

#### Scenario: Re-import adopts the existing row

- **WHEN** a client imports a blueprint whose `(name, version)` already exists in its scope
- **THEN** the request succeeds against the existing blueprint rather than returning 409
