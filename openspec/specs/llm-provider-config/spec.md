# llm-provider-config Specification

## Purpose
Defines the supported LLM providers (Google AI and Vertex AI), organization-level credential storage with encryption at rest, and the credential resolution hierarchy used to instantiate LLM clients.

## Requirements

### Requirement: Supported LLM Providers Registry
The system SHALL define Google AI and Vertex AI as the two first-class supported LLM providers, with distinct authentication and initialization mechanisms.

#### Scenario: Registering providers in the system
- **WHEN** the backend provider registry initializes
- **THEN** it SHALL register `google-ai` expecting an API Key
- **AND** it SHALL register `vertex-ai` expecting a Service Account JSON, GCP Project ID, and Location

### Requirement: Provider Credential Resolution Hierarchy
The system SHALL resolve the effective LLM credential for a given request using a two-step structural lookup — no policy enum, no env-var fallback for request contexts.

#### Scenario: Resolving with project config
- **WHEN** a request requires an LLM client for a specific project
- **AND** a row exists in `project_provider_configs` for `(projectID, provider)`
- **THEN** the system SHALL use that project's config (credentials + model selections)

#### Scenario: Resolving with org config
- **WHEN** a request requires an LLM client for a specific project
- **AND** no row exists in `project_provider_configs` for the project
- **AND** a row exists in `org_provider_configs` for `(orgID, provider)`
- **THEN** the system SHALL use the organization's config

#### Scenario: Hard error when no config found in request context
- **WHEN** either `projectID` or `orgID` is present in the request context
- **AND** no config row is found at the project or org level
- **THEN** the system SHALL return an error: "no provider configured for this project or its organization"
- **AND** SHALL NOT fall back to environment variables
