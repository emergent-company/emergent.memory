# provider-cli Specification

## Purpose
Defines the `memory provider` CLI command group for managing organization-level and project-level LLM provider credentials and model selections via a unified `configure` command.

## Requirements

### Requirement: Provider Management CLI Commands
The system SHALL provide CLI commands under `memory provider` to configure organization-level and project-level provider credentials and model selections via a single `configure` command per scope.

#### Scenario: Configure Google AI credentials via CLI
- **WHEN** an administrator runs `memory provider configure google-ai --api-key <key>` from a project directory
- **THEN** the CLI SHALL submit the API key to `PUT /api/v1/organizations/:orgId/providers/google-ai`
- **AND** the system SHALL encrypt, test, catalog-sync, and save credentials + auto-selected models in one operation
- **AND** the CLI SHALL print the effective config (provider, generative model, embedding model) after save

#### Scenario: Configure Vertex AI credentials via CLI
- **WHEN** an administrator runs `memory provider configure vertex-ai --gcp-project <project> --location <loc> --key-file <path>`
- **THEN** the CLI SHALL read the service account JSON from the file and submit to `PUT /api/v1/organizations/:orgId/providers/vertex-ai`

#### Scenario: Configure project-level override via CLI
- **WHEN** an administrator runs `memory provider configure-project <provider> --api-key <key>` from a project directory
- **THEN** the CLI SHALL submit credentials to `PUT /api/v1/projects/:projectId/providers/:provider`
- **AND** the CLI SHALL print the effective project config after save

#### Scenario: Remove project-level override via CLI
- **WHEN** an administrator runs `memory provider configure-project <provider> --remove`
- **THEN** the CLI SHALL call `DELETE /api/v1/projects/:projectId/providers/:provider`
- **AND** the project SHALL revert to using the org-level config

#### Scenario: Listing available models via CLI
- **WHEN** an administrator runs `memory provider models --provider <provider>`
- **THEN** the CLI SHALL fetch and display the list of supported embedding and generative models for that provider

#### Scenario: Viewing usage and cost summaries via CLI
- **WHEN** an administrator runs `memory provider usage`
- **THEN** the CLI SHALL output an aggregated summary of tokens used and estimated costs per project, provider, and model
