## MODIFIED Requirements

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL gain a GitHub URL import form with a URL
field, an optional ref field, and an optional token field. Submitting the form
posts to the import flow; on success the imported blueprint is installed
(applied) and the UI shows success, and on failure the UI shows the error. The
existing registry-pack and bundled-pack install paths and their
ids/`data-testid`/htmx hooks SHALL be preserved.

The bundled `personal-memory` pack is no longer compiled into the binary. It is
now installed through this GitHub import path from the public repository
`mkucharz/personal-memory-blueprint`; the installed blueprint keeps the name
`personal-memory`.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and
  token) in the gallery import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows
  success

#### Scenario: Model mapping step before install

- **WHEN** the imported manifest's agents carry model references the server would
  reject (for example a bare name missing the `provider/` prefix)
- **THEN** the UI lets the user review each such agent and map it to a valid
  reference before installing, and the submitted import applies those mappings at
  apply time

#### Scenario: No partial install on failure

- **WHEN** the install fails after the import (for example the apply is rejected)
- **THEN** the UI shows the error and leaves no partial blueprint installed, so the
  user can retry

#### Scenario: Import error shown

- **WHEN** the import fails (invalid URL, oversize archive, fetch failure, or
  authorization failure)
- **THEN** the UI shows the mapped error and installs nothing

#### Scenario: Existing install paths unchanged

- **WHEN** a user installs from the registry or a bundled pack as before
- **THEN** those install actions behave exactly as they did before this change,
  with their existing ids, `data-testid`, and htmx hooks intact

#### Scenario: External personal-memory pack installs via GitHub import

- **WHEN** a fresh tenant installs the `personal-memory` blueprint
- **THEN** it is installed through the GitHub import path from
  `mkucharz/personal-memory-blueprint` (not from the removed embedded bundle),
  and is listed under the name `personal-memory`
