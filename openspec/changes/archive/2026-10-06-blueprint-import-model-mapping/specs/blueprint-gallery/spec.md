## MODIFIED Requirements

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL gain a GitHub URL import form with a URL
field, an optional ref field, and an optional token field. Submitting the form
posts to the import flow; on success the imported blueprint is installed
(applied) and the UI shows success, and on failure the UI shows the error. The
existing registry-pack and bundled-pack install paths and their
ids/`data-testid`/htmx hooks SHALL be preserved.

Before installing, the flow SHALL let the user map the imported manifest's agent
models to valid `provider/model` references (previewing the manifest without
installing anything), and the install SHALL submit those mappings atomically so
a failed install leaves no partial blueprint behind and can be retried cleanly.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and
  token) in the gallery import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows
  success

#### Scenario: Model mapping step before install

- **WHEN** the imported manifest's agents carry model references the server
  would reject (for example a bare name missing the `provider/` prefix)
- **THEN** the UI lets the user map each such agent to a valid reference before
  installing, and the submitted import applies those mappings

#### Scenario: No partial install on failure

- **WHEN** the install fails after the import (for example the apply is
  rejected)
- **THEN** the UI shows the error and leaves no partial blueprint installed, so
  the user can retry with the model mapped

#### Scenario: Import error shown

- **WHEN** the import fails (invalid URL, oversize archive, fetch failure, or
  authorization failure)
- **THEN** the UI shows the mapped error and installs nothing

#### Scenario: Existing install paths unchanged

- **WHEN** a user installs from the registry or a bundled pack as before
- **THEN** those install actions behave exactly as they did before this change,
  with their existing ids, `data-testid`, and htmx hooks intact
