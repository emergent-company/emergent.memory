## ADDED Requirements

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL gain a GitHub URL import form with a URL
field, an optional ref field, and an optional token field. Submitting the form
posts to the import flow; on success the imported blueprint is installed
(applied) and the UI shows success, and on failure the UI shows the error. The
existing registry-pack and bundled-pack install paths and their
ids/`data-testid`/htmx hooks SHALL be preserved.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and
  token) in the gallery import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows
  success

#### Scenario: Import error shown

- **WHEN** the import fails (invalid URL, oversize archive, fetch failure, or
  authorization failure)
- **THEN** the UI shows the mapped error and installs nothing

#### Scenario: Existing install paths unchanged

- **WHEN** a user installs from the registry or a bundled pack as before
- **THEN** those install actions behave exactly as they did before this change,
  with their existing ids, `data-testid`, and htmx hooks intact
