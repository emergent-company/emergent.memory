## Purpose

Defines the redesigned project/org picker: a single-column vertically-scrolling list grouped by organization, with clickable org headers and cogwheels, a conditional recent-projects section, a sticky action footer, and last-used-project auto-selection on return.

## ADDED Requirements

### Requirement: Single-column vertical-scroll list

The picker SHALL render all accessible projects in one column that scrolls vertically and never horizontally.

#### Scenario: Many projects scroll vertically
- **WHEN** the user has more projects than fit in the picker's height
- **THEN** the picker scrolls vertically and the page does not scroll horizontally

#### Scenario: Long project names truncate
- **WHEN** a project name is longer than the picker width
- **THEN** the name truncates to a single line without widening the picker

### Requirement: Group by organization

The picker SHALL group projects under their organization, with the organization name as a header above its projects.

#### Scenario: Projects grouped
- **WHEN** the picker opens
- **THEN** each organization's projects appear under that organization's header

#### Scenario: Organization without projects
- **WHEN** the user belongs to an organization that has no projects
- **THEN** the picker shows that organization with no projects under it

### Requirement: Enter organization context

The picker SHALL let the user open an organization as the navigation context by clicking its header or its cogwheel.

#### Scenario: Header opens org context
- **WHEN** the user clicks an organization header
- **THEN** the console activates the organization context

#### Scenario: Cogwheel opens org context
- **WHEN** the user clicks an organization's cogwheel
- **THEN** the console activates the organization context

### Requirement: Select a project

The picker SHALL activate a project when the user selects its row.

#### Scenario: Project selected
- **WHEN** the user clicks a project row
- **THEN** the session's active project changes to that project and the picker closes

### Requirement: Recent projects section

The picker SHALL show a "Recent" section only when the user has more than ten projects.

#### Scenario: Recent shown over threshold
- **WHEN** the user has more than ten projects
- **THEN** the picker shows a Recent section above the full grouped list

#### Scenario: Recent hidden under threshold
- **WHEN** the user has ten or fewer projects
- **THEN** the picker shows only the grouped list with no Recent section

### Requirement: Sticky action footer

The picker SHALL keep its "Add New Project" and "New Organization" actions visible at the bottom while the list scrolls.

#### Scenario: Footer stays visible
- **WHEN** the project list scrolls
- **THEN** the Add New Project and New Organization actions remain visible at the bottom

### Requirement: Last-used project auto-selection

The gateway SHALL remember the last-used project per account in a durable signed cookie and automatically select it when the user returns with no active project.

#### Scenario: Auto-select on return
- **WHEN** a returning user has a remembered last-used project and no active project in the session
- **THEN** the console activates that project automatically

#### Scenario: No remembered project
- **WHEN** a user has no remembered last-used project
- **THEN** the console falls back to the org context or wizard without auto-selecting a project

### Requirement: Mobile full-width picker

The picker SHALL render full-width on mobile viewports.

#### Scenario: Full width on mobile
- **WHEN** the picker opens on a mobile viewport
- **THEN** the picker spans the available width
