## ADDED Requirements

### Requirement: Fetching Available Models from Provider APIs
The system SHALL dynamically fetch the list of supported embedding and generative models from the provider API (Google AI or Vertex AI) upon credential setup.

#### Scenario: Validating and fetching Google AI models
- **WHEN** a user saves a Google AI API Key
- **THEN** the system SHALL make a request to the Google AI API (`/v1beta/models`)
- **AND** SHALL persist the returned list of supported models in the `provider_supported_models` cache

#### Scenario: Handling provider API timeout or failure
- **WHEN** the system attempts to fetch supported models during credential setup
- **AND** the provider API times out or fails (unrelated to authentication)
- **THEN** the system SHALL fall back to saving a statically defined known list of models to the `provider_supported_models` cache so the user is not blocked

### Requirement: Default Model Selection per Provider
The system SHALL allow users to select default generative and embedding models from the fetched supported model catalog for each provider at both the organization and project level.

#### Scenario: Selecting organization default models
- **WHEN** an organization administrator has successfully configured a provider
- **THEN** they SHALL be able to select a default embedding model and a default generative model from the supported list
- **AND** the system SHALL save these selections in the `organization_provider_model_selections` table

#### Scenario: Overriding default models at the project level
- **WHEN** a project is configured with a `project` provider policy
- **THEN** the project administrator SHALL be able to select project-specific default models
- **AND** the system SHALL save these selections in the `project_provider_policies` table