## ADDED Requirements

### Requirement: Review blueprint models before install

Before applying any blueprint whose manifest agents pin a model, the gallery
SHALL render a model-review step (one row per pinned agent) instead of applying
immediately. The review applies to every install surface — the available-pack
install action, the draft install action, and the GitHub URL import flow — and
SHALL NOT run (no UI change) for a blueprint that pins no models, which installs
directly as before. Each review row SHALL show the agent name and a neutral
"Blueprint: <model>" badge for the blueprint's pinned reference, plus a select of
the project's configured generative models whose first option ("Project
default") carries an empty value. Submitting the review applies the blueprint
with the chosen per-agent model overrides; a pinned model the project cannot
serve SHALL additionally show an informational/warning note pointing the user at
the provider settings (`/settings/providers`) without blocking the install.

#### Scenario: Pinned blueprint shows the review step

- **WHEN** a user activates install on a blueprint whose manifest pins a model on
  at least one agent
- **THEN** the gallery renders the review step (one row per pinned agent) and does
  not apply yet

#### Scenario: Unpinned blueprint installs directly

- **WHEN** a user activates install on a blueprint whose agents pin no model
- **THEN** the blueprint applies directly with no review step, exactly as before

#### Scenario: Review row identifies the pinned model

- **WHEN** the review step renders for a pinned agent
- **THEN** the row shows the agent name and a neutral "Blueprint: <model>" badge
  with the blueprint's pinned reference

#### Scenario: Select offers configured models and the project default

- **WHEN** the review step renders
- **THEN** each row's select lists the project's configured generative models,
  with a first "Project default" option carrying an empty value

#### Scenario: Non-servable pin links to provider settings

- **WHEN** a pinned agent's model reference cannot be served by the project's
  configured providers (a bare name or an unconfigured provider prefix)
- **THEN** the row shows an informational/warning note and a link to
  `/settings/providers`, and the user can still install

### Requirement: Pre-fill model mappings

The review step SHALL pre-fill each pinned agent's select with the effective
choice, resolved with precedence **saved per-project override > pinned model when
its provider is configured > project default (empty)**. A saved override that is
present wins even when its value is empty (the empty value selects the project
default). A selected value that is not in the configured catalog SHALL render as
a synthetic "(current)" option so it remains visible and selectable.

#### Scenario: Saved override wins

- **WHEN** a saved per-project override exists for a pinned agent
- **THEN** the row pre-fills with the saved override, even when the pinned model
  would otherwise be servable

#### Scenario: Servable pin pre-fills

- **WHEN** a pinned agent has no saved override and its pinned model's provider is
  configured
- **THEN** the row pre-fills with the pinned model

#### Scenario: Unservable pin defaults to project default

- **WHEN** a pinned agent has no saved override and its pinned model's provider is
  not configured
- **THEN** the row pre-fills with the project default (empty)

#### Scenario: Selected value outside the catalog is shown

- **WHEN** a pre-filled (saved-override) value is not among the configured
  generative models
- **THEN** the select renders a synthetic "(current)" option holding that value so
  it stays selected

### Requirement: Apply with model overrides

Submitting the review step SHALL apply the blueprint with the chosen per-agent
model overrides, applying them at apply time (never rewriting the stored
manifest) and persisting them per-project keyed by blueprint name, so the user's
mapping survives a version upgrade and the update check continues to compare the
pristine upstream manifest. An empty override clears the model so the agent falls
back to the project default.

#### Scenario: Confirm applies with overrides

- **WHEN** a user submits the review step with chosen model overrides
- **THEN** the gateway applies the blueprint with those overrides and redirects to
  the installed confirmation

#### Scenario: Empty override selects the project default

- **WHEN** a user leaves a pinned agent on "Project default" and submits
- **THEN** the agent's override is empty and the apply resolves it to the project
  default

#### Scenario: Saved overrides persist across upgrades

- **WHEN** a blueprint is applied with model overrides
- **THEN** the overrides are stored per-project by blueprint name, so installing a
  later version of the same blueprint re-reads the same mapping

## MODIFIED Requirements

### Requirement: Import a blueprint from a GitHub URL in the gallery

The gallery install surface SHALL provide a GitHub URL import form with a URL
field, an optional ref field, and an optional token field. Submitting the form
posts to the import flow; on success the imported blueprint is installed
(applied) and the UI shows success, and on failure the UI shows the error. The
registry-pack install path and its ids/`data-testid`/htmx hooks SHALL be
preserved. The bundled-pack install path is retired.

Before installing, the flow SHALL let the user review the imported manifest's
pinned agent models against the project's configured providers and map them to
valid `provider/model` references (previewing the manifest without installing
anything). The install SHALL submit those mappings as apply-time `modelOverrides`
— never rewriting the stored manifest — so a failed install leaves no partial
blueprint behind and can be retried cleanly, and the mapping survives a version
upgrade.

#### Scenario: Successful GitHub URL import

- **WHEN** a user submits a valid GitHub repository URL (with optional ref and
  token) in the gallery import form
- **THEN** the blueprint is imported, installed (applied), and the UI shows success

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

- **WHEN** a user installs from the registry as before
- **THEN** the registry install action behaves exactly as it did before this change,
  with its existing ids, `data-testid`, and htmx hooks intact (the bundled path is
  retired)
