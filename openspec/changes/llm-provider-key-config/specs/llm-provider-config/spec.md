## ADDED Requirements

### Requirement: Supported LLM Providers Registry
The system SHALL define Google AI and Vertex AI as the two first-class supported LLM providers, with distinct authentication and initialization mechanisms.

#### Scenario: Registering providers in the system
- **WHEN** the backend provider registry initializes
- **THEN** it SHALL register `google-ai` expecting an API Key
- **AND** it SHALL register `vertex-ai` expecting a Service Account JSON, GCP Project ID, and Location

### Requirement: Organization-Level Credential Storage
The system SHALL allow each organization to securely store one set of credentials per supported LLM provider.

#### Scenario: Saving Google AI credentials
- **WHEN** an organization administrator submits a Google AI API key
- **THEN** the system SHALL encrypt the key at rest using AES-GCM and the `LLM_ENCRYPTION_KEY`
- **AND** SHALL store it in the `organization_provider_credentials` table associated with the organization

#### Scenario: Saving Vertex AI credentials
- **WHEN** an organization administrator submits Vertex AI credentials (JSON, Project ID, Location)
- **THEN** the system SHALL encrypt the Service Account JSON at rest
- **AND** SHALL store it along with the Project ID and Location in `organization_provider_credentials`

### Requirement: Provider Credential Resolution Hierarchy
The system SHALL resolve the effective LLM credential for a given request using a strict hierarchy by evaluating the injected `context.Context`: Project Override -> Organization Credential -> Server Environment Fallback.

#### Scenario: Resolving with project override
- **WHEN** a request requires an LLM client for a specific project
- **AND** the project has a `project` provider policy with overridden credentials
- **THEN** the system SHALL use the project's overridden credentials

#### Scenario: Resolving with organization credential
- **WHEN** a request requires an LLM client
- **AND** the project has an `organization` provider policy (or no project override)
- **AND** the organization has stored credentials for the required provider
- **THEN** the system SHALL use the organization's credentials

#### Scenario: Resolving with server environment fallback
- **WHEN** a request requires an LLM client
- **AND** the project policy is `none` (or falls back)
- **AND** the organization has no stored credentials
- **THEN** the system SHALL fall back to using the server's environment variables (e.g., `GOOGLE_API_KEY`)