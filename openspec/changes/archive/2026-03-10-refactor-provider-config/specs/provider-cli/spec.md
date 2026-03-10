## MODIFIED Requirements

### Requirement: Provider Management CLI Commands
The system SHALL provide CLI commands under `emergent provider` to configure organization-level and project-level provider credentials and model selections via a single `configure` command per scope.

#### Scenario: Configure Google AI credentials via CLI
- **WHEN** an administrator runs `emergent provider configure google-ai --api-key <key>` from a project directory
- **THEN** the CLI SHALL submit the API key to `PUT /api/v1/organizations/:orgId/providers/google-ai`
- **AND** the system SHALL encrypt, test, catalog-sync, and save credentials + auto-selected models in one operation
- **AND** the CLI SHALL print the effective config (provider, generative model, embedding model) after save

#### Scenario: Configure Vertex AI credentials via CLI
- **WHEN** an administrator runs `emergent provider configure vertex-ai --gcp-project <project> --location <loc> --key-file <path>`
- **THEN** the CLI SHALL read the service account JSON from the file and submit to `PUT /api/v1/organizations/:orgId/providers/vertex-ai`

#### Scenario: Configure project-level override via CLI
- **WHEN** an administrator runs `emergent provider configure-project <provider> --api-key <key>` from a project directory
- **THEN** the CLI SHALL submit credentials to `PUT /api/v1/projects/:projectId/providers/:provider`
- **AND** the CLI SHALL print the effective project config after save

#### Scenario: Remove project-level override via CLI
- **WHEN** an administrator runs `emergent provider configure-project <provider> --remove`
- **THEN** the CLI SHALL call `DELETE /api/v1/projects/:projectId/providers/:provider`
- **AND** the project SHALL revert to using the org-level config

#### Scenario: Listing available models via CLI
- **WHEN** an administrator runs `emergent provider models --provider <provider>`
- **THEN** the CLI SHALL fetch and display the list of supported embedding and generative models for that provider

## REMOVED Requirements

### Requirement: Project Policy Management via CLI
**Reason**: The `policy` enum is removed. Project override is now represented by row presence, not by a policy value. `emergent projects set-provider` and the `none`/`organization`/`project` policy flags are removed.
**Migration**: Use `emergent provider configure-project <provider> --api-key ...` to set a project override, or `emergent provider configure-project <provider> --remove` to revert to org config.

### Requirement: set-credentials and set-models as separate commands
**Reason**: These two commands are collapsed into `provider configure`. Separate `set-credentials` and `set-models` subcommands are removed to eliminate the "forgot to call set-models" failure mode.
**Migration**: Use `emergent provider configure <provider>` with optional `--generative-model` and `--embedding-model` flags. Omitting model flags triggers auto-selection.
