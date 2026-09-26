## Purpose

Extends the existing settings rail from a list of the six project-setting sections into the navigator
for the whole Settings area, so every settings destination shows the rail and its entries agree with the
sidebar's Settings group.

## MODIFIED Requirements

### Requirement: Show the settings sub-navigation

The Settings area SHALL show a vertical sub-navigation rail listing the Settings-area destinations — the
members of the sidebar's Settings group — and each of the six project-setting sections (General,
Assistant, Overrides, Providers, Voice, and Devices) SHALL remain reachable from that rail as a nested
entry under the Project entry.

#### Scenario: Sub-navigation present

- **WHEN** a Settings page loads
- **THEN** the sub-navigation shows the Settings-area destinations and, nested under the Project entry, the General, Assistant, Overrides, Providers, Voice, and Devices links

#### Scenario: Active section highlighted

- **WHEN** a Settings page loads
- **THEN** the sub-navigation highlights the link for the current destination, and additionally highlights the current project-setting section when the page is one of the six project-setting sub-pages

### Requirement: Keep section save behavior intact

Each settings sub-page SHALL preserve its existing save and error behavior; moving a section to its own page SHALL NOT change how its settings are saved or how errors are reported. Error feedback SHALL be presented in the context of what failed — at the failing control where one exists, and at page level only when the failure has no control to attach to — so a failure is never reported solely as an unlabelled page-level notice.

#### Scenario: Save on a sub-page

- **WHEN** the user saves a valid change on any settings sub-page
- **THEN** the change persists and the page shows a success message or toast, as before the split

#### Scenario: Error on a sub-page

- **WHEN** a backend request for a settings sub-page fails
- **THEN** that sub-page shows a clear error in the context of the failed control (or at page level when no control exists), without breaking the sub-navigation

## ADDED Requirements

### Requirement: Every Settings-area destination renders the settings sub-navigation

A destination that is a member of the sidebar's Settings group SHALL render the settings sub-navigation
rail.

#### Scenario: Rail presence is not page-dependent

- **WHEN** the user opens any member of the Settings group — project settings, API tokens, approvals, MCP servers, MCP sharing, blueprints, or skills
- **THEN** each renders the settings sub-navigation rail, so navigation inside the Settings area is continuous

#### Scenario: A destination outside the group renders no rail

- **WHEN** a user opens a page that is not a member of the Settings group, including a page whose path sits under `/settings/`
- **THEN** it does not render the settings sub-navigation rail

### Requirement: The rail entries agree with the sidebar Settings group

The rail's destination entries SHALL be the sidebar Settings group's members, so the rail and the sidebar
cannot present two different sets. Approvals is currently a top-level ungrouped nav item and MCP sharing
is a nested page; this change settles their placement so the two sets are identical.

#### Scenario: Rail entries match the group

- **WHEN** the rail entries are compared with the sidebar Settings group's destinations
- **THEN** they are the same set, in the same order

#### Scenario: Current destination appears in the rail

- **WHEN** a user is on any Settings-group destination
- **THEN** that destination is listed in the rail and marked as active
