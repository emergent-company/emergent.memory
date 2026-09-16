## ADDED Requirements

### Requirement: Authoring affordances for blueprint drafts and versions

The gallery SHALL expose edit and release actions for each project blueprint
draft, and an install action for each version in a blueprint's version list,
linking into the `blueprint-authoring` flows.

#### Scenario: Draft row actions

- **WHEN** a user views the drafts list and a project blueprint draft exists
- **THEN** the draft row offers edit, release, and install actions

#### Scenario: Version list install

- **WHEN** a user opens a blueprint's detail view that has multiple versions
- **THEN** each version is listed with an install action

#### Scenario: Released version row

- **WHEN** a blueprint version is released
- **THEN** the version row shows it as released and offers install, and does not offer in-place edit
