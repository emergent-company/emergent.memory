# blueprint-gallery Specification

## Purpose
Provide a web UI page that lets users browse, install, toggle, and remove blueprint
packs to extend the graph database schema.

## Requirements

### Requirement: Blueprints navigation entry
The web shell SHALL expose a "Blueprints" entry in the sidebar navigation linking to `/blueprints`.

#### Scenario: Navigate to blueprints
- **WHEN** a user clicks the "Blueprints" sidebar item
- **THEN** the browser navigates to `/blueprints` and renders the blueprints page

### Requirement: Installed packs view
The blueprints page SHALL list all installed packs with their name, version, active state,
and an action to inspect their types.

#### Scenario: Installed list rendered
- **WHEN** a user opens the blueprints page and packs are installed
- **THEN** each installed pack is shown with its name, version, and active/inactive state

### Requirement: Available packs view
The blueprints page SHALL list packs that are not yet installed, with an install action for each.

#### Scenario: Available list rendered
- **WHEN** a user opens the blueprints page and uninstalled packs exist
- **THEN** each available pack is shown with its name, version, source, and an install action

### Requirement: Install a pack from the UI
The UI SHALL allow installing an available pack and reflect the result, including any conflicts.

#### Scenario: Successful install
- **WHEN** a user activates the install action on an available pack
- **THEN** the pack moves to the installed list and the UI reports the install result

#### Scenario: Install with conflicts
- **WHEN** an install produces conflicts
- **THEN** the UI displays the conflicts rather than silently proceeding

### Requirement: Toggle a pack active from the UI
The UI SHALL allow enabling or disabling an installed pack.

#### Scenario: Disable a pack
- **WHEN** a user activates the toggle on an installed active pack
- **THEN** the pack is shown as inactive

### Requirement: Remove a pack from the UI
The UI SHALL allow soft-removing an installed pack.

#### Scenario: Remove a pack
- **WHEN** a user activates the remove action on an installed pack
- **THEN** the pack is removed from the installed list and its data remains recoverable

### Requirement: Inspect compiled types from the UI
The UI SHALL allow a user to view the object and relationship types a pack contributes.

#### Scenario: Type inspection
- **WHEN** a user opens type inspection for an installed pack
- **THEN** the UI lists the pack's object and relationship types with their labels and descriptions


### Requirement: Create a blueprint from the current schema

The UI SHALL let a user create a blueprint draft from the project's current object types, either as a new blueprint or as a new version of an existing blueprint, and show the resulting draft in the drafts list.

#### Scenario: Save current schema as a new blueprint

- **WHEN** a user invokes "Save as blueprint" from the Schema area or blueprints gallery, provides a name, and confirms
- **THEN** a draft blueprint is created from the current object types and appears in the blueprints drafts list

#### Scenario: Save current schema as a new version

- **WHEN** a user invokes "Save as new version" on an existing blueprint and provides a version label
- **THEN** a new draft version is created from the current object types and appears in that blueprint's version list

#### Scenario: Draft is installable

- **WHEN** a user installs a blueprint draft derived from the current schema
- **THEN** the install succeeds through the existing install path and reports the applied types

#### Scenario: Derivation fails validation

- **WHEN** the derivation request is rejected (for example, duplicate name and version)
- **THEN** the UI shows the error and does not add a draft to the list

### Requirement: Version history display

The blueprint detail view SHALL list a blueprint record's versions newest-first, with each
version linking to that exact version's detail view, and SHALL present the version inline
with the blueprint title and in the breadcrumb trail.

#### Scenario: Versions sorted newest-first

- **WHEN** a user opens a blueprint record with multiple versions
- **THEN** the versions list orders them newest-first by semantic version (not lexicographically)

#### Scenario: Navigate to a specific version

- **WHEN** a user selects a version from the versions list
- **THEN** the browser navigates to that exact version's detail view

#### Scenario: Version shown in the header and breadcrumb

- **WHEN** a user opens a blueprint version
- **THEN** the version badge is shown directly after the blueprint title and the version
  appears as the final breadcrumb step

### Requirement: Blueprint lifecycle and install indication

The UI SHALL indicate a blueprint's lifecycle and install state as two independent
dimensions: a Release state (Draft or Published) and an Install state (Installed when the
exact version is applied to the project). The version currently being viewed SHALL be
identified separately from these state badges.

#### Scenario: Release and install badges shown per version

- **WHEN** a user views a blueprint record's version list
- **THEN** each version shows a Release badge (Draft or Published) and, when that exact
  version is applied to the project, an Installed badge

#### Scenario: Current version identified

- **WHEN** a user views a blueprint version
- **THEN** that version's row is highlighted as the one currently being viewed

### Requirement: Suggested next version

The "Save as new version" flow SHALL suggest the next semantic version by bumping the patch
of the highest existing version of the blueprint.

#### Scenario: Next version suggested

- **WHEN** a user opens "Save as new version" for a blueprint whose highest version is 1.4.2
- **THEN** the version field is prefilled with 1.4.3

### Requirement: Compare draft versions

The detail view of a draft version SHALL allow the user to choose another version to compare
against, defaulting to the next-lower version, and SHALL highlight field-level differences:
additions in green, removals in red, and modifications in orange.

#### Scenario: Compare a draft against another version

- **WHEN** a user views a draft version and selects a comparison version
- **THEN** the UI shows the changed object types, relationship types, and agents with
  field-level additions, removals, and modifications highlighted by color and text labels

#### Scenario: Default comparison base

- **WHEN** a user opens a draft version without selecting a comparison version
- **THEN** the comparison defaults to the highest version strictly lower than the draft

### Requirement: Show blueprint provenance in the gallery

The gallery and blueprint detail views SHALL identify blueprint-derived object types and link them to the owning blueprint.

#### Scenario: Detail lists derived types

- **WHEN** a user opens a blueprint's detail view
- **THEN** its object types are listed with the owning blueprint identified

#### Scenario: Navigate from type to blueprint

- **WHEN** a user follows the owning-blueprint reference shown for a derived type
- **THEN** the browser navigates to that blueprint's detail view
