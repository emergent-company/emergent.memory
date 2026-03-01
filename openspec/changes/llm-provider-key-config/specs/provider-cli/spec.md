## ADDED Requirements

### Requirement: Provider Management CLI Commands
The system SHALL provide a suite of CLI commands under the `emergent provider` group to manage organization-level LLM provider credentials, models, and usage summaries.

#### Scenario: Setting Google AI credentials via CLI
- **WHEN** an administrator runs `emergent provider set-key <api-key>`
- **THEN** the CLI SHALL submit the API key to the backend via the Go SDK
- **AND** the backend SHALL securely save the credentials for the Google AI provider

#### Scenario: Setting Vertex AI credentials via CLI
- **WHEN** an administrator runs `emergent provider set-vertex --project <project-id> --location <location> --key-file <path-to-json>`
- **THEN** the CLI SHALL submit the Service Account JSON and metadata to the backend via the Go SDK
- **AND** the backend SHALL securely save the credentials for the Vertex AI provider

#### Scenario: Listing available models via CLI
- **WHEN** an administrator runs `emergent provider models --provider <provider>`
- **THEN** the CLI SHALL fetch from the Go SDK and display the list of supported embedding and generative models for that provider

#### Scenario: Viewing usage and cost summaries via CLI
- **WHEN** an administrator runs `emergent provider usage`
- **THEN** the CLI SHALL output an aggregated summary of tokens used and estimated costs per project, provider, and model

### Requirement: Project Policy Management via CLI
The system SHALL extend the existing `projects` CLI subgroup with commands to configure per-project, per-provider policies and override credentials and model selections.

#### Scenario: Setting project policy via CLI
- **WHEN** an administrator runs `emergent projects set-provider --project <project-id> --provider <provider> --policy <policy>`
- **THEN** the CLI SHALL submit the policy (`none`, `organization`, or `project`) to the backend via the Go SDK
- **AND** the system SHALL enforce the specified policy for the project