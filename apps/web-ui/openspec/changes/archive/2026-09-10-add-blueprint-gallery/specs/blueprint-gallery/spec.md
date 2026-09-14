## Purpose

Provide a web UI page that lets users browse, install, toggle, and remove blueprint
packs to extend the graph database schema.

## ADDED Requirements

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
