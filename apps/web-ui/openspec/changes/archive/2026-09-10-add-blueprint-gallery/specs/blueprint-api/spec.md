## Purpose

Expose Emergent Memory's blueprint/schema management as a gateway HTTP API so the
web UI (and future clients) can list, install, toggle, and remove schema packs that
extend the graph database.

## ADDED Requirements

### Requirement: List installed blueprint packs
The API SHALL return the blueprint packs currently installed in the project, including
their assignment id, active state, and install timestamp.

#### Scenario: Installed packs returned
- **WHEN** a client sends `GET /api/blueprints/installed`
- **THEN** the API returns HTTP 200 with a JSON array, each element containing `assignmentId`, `schemaId`, `name`, `version`, `description`, `active`, and `installedAt`

#### Scenario: No packs installed
- **WHEN** a client sends `GET /api/blueprints/installed` and no packs are installed
- **THEN** the API returns HTTP 200 with an empty JSON array

#### Scenario: Memory unavailable
- **WHEN** a client sends `GET /api/blueprints/installed` and memory is unreachable
- **THEN** the API returns HTTP 502 with a JSON body containing an `error` field

### Requirement: List available blueprint packs
The API SHALL return packs that are not yet installed, drawn from both the memory schema
registry and the bundled `blueprints/` directory, and SHALL identify each pack's source.

#### Scenario: Available packs returned
- **WHEN** a client sends `GET /api/blueprints/available`
- **THEN** the API returns HTTP 200 with a JSON array, each element containing `id`, `name`, `version`, `description`, `author`, and `source` (either `registry` or `bundled`)

#### Scenario: Bundled packs surfaced
- **WHEN** a pack exists in the local `blueprints/` directory but not in the memory registry
- **THEN** the API includes it in the available list with `source` equal to `bundled`

### Requirement: View compiled schema types
The API SHALL return the project's merged object and relationship types across all active
installed packs.

#### Scenario: Compiled types returned
- **WHEN** a client sends `GET /api/blueprints/compiled-types`
- **THEN** the API returns HTTP 200 with a JSON object containing `objectTypes` and `relationshipTypes` arrays, where each type carries its type name and the pack it originates from

### Requirement: Install a blueprint pack
The API SHALL install a pack into the project by assigning an existing registry schema, or
by registering a bundled pack and then assigning it in a single call.

#### Scenario: Install a registry pack
- **WHEN** a client sends `POST /api/blueprints/install` with a body containing `schemaId` referencing an existing registry schema
- **THEN** the API assigns that schema to the project and returns HTTP 201 with a JSON body containing `assignmentId`, `installedTypes`, `skippedTypes`, and `conflicts`

#### Scenario: Install a bundled pack
- **WHEN** a client sends `POST /api/blueprints/install` with a body containing `source` equal to `bundled` and `name` matching a local pack directory
- **THEN** the API registers the pack in memory and assigns it, returning HTTP 201 with the assignment result

#### Scenario: Install reports conflicts
- **WHEN** an install would overwrite an existing type name
- **THEN** the API returns the assignment result with a non-empty `conflicts` array and does not silently overwrite data

### Requirement: Toggle a pack active
The API SHALL allow enabling or disabling an installed pack without removing its data.

#### Scenario: Disable a pack
- **WHEN** a client sends `PATCH /api/blueprints/assignments/:id` with body `{"active": false}`
- **THEN** the API returns HTTP 200 and the pack no longer contributes its types to the compiled schema

### Requirement: Remove a blueprint pack
The API SHALL soft-remove an installed pack so its schema types stop applying while its data remains recoverable.

#### Scenario: Soft remove
- **WHEN** a client sends `DELETE /api/blueprints/assignments/:id`
- **THEN** the API returns HTTP 204 and the assignment is marked inactive with a removal timestamp
