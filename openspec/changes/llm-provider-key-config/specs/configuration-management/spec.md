## ADDED Requirements

### Requirement: Environment Variable Credentials Fallback
The system SHALL treat existing environment variable credentials (e.g., `GOOGLE_API_KEY`, `VERTEX_PROJECT`, `VERTEX_LOCATION`) as the lowest-priority fallback in the LLM provider credential resolution hierarchy, rather than as required global variables.

#### Scenario: Server starts without environment variable credentials
- **WHEN** the server application starts up
- **AND** `GOOGLE_API_KEY` and Vertex AI environment variables are not set
- **THEN** the configuration module SHALL NOT block startup
- **AND** SHALL treat these variables as optional, assuming organizations will provide their own credentials

#### Scenario: Using environment variables as fallback
- **WHEN** a request requires an LLM client
- **AND** the project policy is `none`
- **AND** the organization has no stored credentials for the provider
- **THEN** the system SHALL attempt to use the environment variable credentials (e.g., `GOOGLE_API_KEY`)
- **AND** if not set, the request SHALL fail gracefully indicating missing credentials

### Requirement: Credential Encryption Configuration
The system SHALL require an encryption key to be defined in the server environment configuration to safely secure organization and project-level credentials at rest.

#### Scenario: Server validating encryption key on startup
- **WHEN** the server application starts up
- **THEN** the configuration module SHALL validate the presence of the `LLM_ENCRYPTION_KEY` environment variable
- **AND** if missing, it SHALL either fail startup or disable the credential storage feature, logging a clear warning