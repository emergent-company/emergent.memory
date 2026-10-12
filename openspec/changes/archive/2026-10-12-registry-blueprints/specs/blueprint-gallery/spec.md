## MODIFIED Requirements

### Requirement: Available packs view

The blueprints page SHALL list blueprints that are published in the caller's read scope (global
built-in blueprints plus the project's own published blueprints), showing the newest version of each
name and excluding those already applied to the project. Each available blueprint SHALL be shown with
its name, version, source, an install action, and — when it is a platform built-in — a Built-in badge.
The bundled-directory source is retired; the gallery no longer reads packs from the web-ui binary.

#### Scenario: Available list rendered

- **WHEN** a user opens the blueprints page and uninstalled published blueprints exist in scope
- **THEN** each is shown with its name, version, source, and an install action

#### Scenario: Built-in blueprint is badged

- **WHEN** an available blueprint is a platform built-in (`origin="builtin"`)
- **THEN** the row shows a Built-in badge

#### Scenario: Applied blueprints are not listed as available

- **WHEN** a blueprint is applied to the project
- **THEN** it is not listed in the available list

#### Scenario: Newest version is surfaced

- **WHEN** a blueprint name has multiple published versions in scope
- **THEN** only its newest version is listed as available

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL provide a GitHub URL import form with a URL field, an optional ref
field, and an optional token field. Submitting the form posts to the import flow; on success the
imported blueprint is installed (applied) and the UI shows success, and on failure the UI shows the
error. The registry-pack install path and its ids/`data-testid`/htmx hooks SHALL be preserved. The
bundled-pack install path is retired.

Before installing, the flow SHALL let the user map the imported manifest's agent models to valid
`provider/model` references (previewing the manifest without installing anything), and the install
SHALL submit those mappings atomically so a failed install leaves no partial blueprint behind and can
be retried cleanly.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and token) in the gallery
  import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows success

#### Scenario: Model mapping step before install

- **WHEN** the imported manifest's agents carry model references the server would reject (for example
  a bare name missing the `provider/` prefix)
- **THEN** the UI lets the user map each such agent to a valid reference before installing, and the
  submitted import applies those mappings

#### Scenario: No partial install on failure

- **WHEN** the install fails after the import (for example the apply is rejected)
- **THEN** the UI shows the error and leaves no partial blueprint installed, so the user can retry

#### Scenario: Import error shown

- **WHEN** the import fails (invalid URL, oversize archive, fetch failure, or authorization failure)
- **THEN** the UI shows the mapped error and installs nothing

#### Scenario: Existing install paths unchanged

- **WHEN** a user installs from the registry as before
- **THEN** the registry install action behaves exactly as it did before this change, with its
  existing ids, `data-testid`, and htmx hooks intact (the bundled path is retired)

## ADDED Requirements

### Requirement: Show built-in provenance in the gallery

The gallery SHALL distinguish platform-delivered blueprints from user-created and imported ones by
rendering a Built-in badge on built-in rows. Only non-built-in blueprints SHALL offer removal.

#### Scenario: Built-in badge shown

- **WHEN** a user views the available list
- **THEN** each blueprint with `origin="builtin"` shows a Built-in badge and no Remove action

### Requirement: Remove a user or imported blueprint from the gallery

The gallery SHALL offer a Remove action, with a confirmation step, on available blueprints that are
not built-in. Activating it SHALL delete the blueprint from the registry and return to the gallery
with a success message. When the blueprint is currently applied, the action SHALL fail and the UI
SHALL tell the user to uninstall it first.

#### Scenario: Remove an unapplied blueprint

- **WHEN** a user confirms Remove on an available non-built-in blueprint
- **THEN** the blueprint is deleted from the registry and the gallery shows success

#### Scenario: Remove is blocked while applied

- **WHEN** a user confirms Remove on a blueprint that is applied to the project
- **THEN** the UI reports that the blueprint must be uninstalled first and nothing is deleted

#### Scenario: Built-in has no Remove action

- **WHEN** a user views a built-in blueprint row
- **THEN** no Remove action is rendered for it
