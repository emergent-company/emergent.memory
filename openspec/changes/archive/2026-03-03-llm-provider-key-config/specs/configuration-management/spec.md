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

### Requirement: Missing Provider Graceful Failure & Instructions
The system SHALL fail requests gracefully and provide clear CLI/API error messages if an LLM action is attempted but no provider is configured at any level (Project, Organization, or Environment).

#### Scenario: User triggers an LLM action without any credentials
- **WHEN** a user triggers an agent or embedding operation
- **AND** the resolved credential hierarchy returns no valid credentials for the requested provider
- **THEN** the API SHALL return a `400 Bad Request` or `424 Failed Dependency`
- **AND** the error message MUST explicitly instruct the user on how to fix it (e.g., `"No LLM provider configured. Run 'emergent provider set-key <api-key>' to configure Google AI for your organization."`)

### Requirement: Post-Installation Provider Prompts
The CLI SHALL proactively prompt or instruct the user to configure an LLM provider immediately after key lifecycle events where a provider will be needed.

#### Scenario: Prompting after standalone installation
- **WHEN** a user successfully finishes running `emergent install`
- **AND** they did not provide a `--google-api-key` flag during installation
- **THEN** the CLI success output SHALL include a prominent step indicating they must configure a provider (e.g., `"Next step: Configure your LLM provider by running 'emergent provider set-key <your-key>'`)

#### Scenario: Prompting after project creation
- **WHEN** a user creates a new project via `emergent projects create`
- **AND** the organization currently has no LLM provider configured
- **THEN** the CLI SHALL output a warning reminding the user that features will not work until a provider is configured for the organization or the project.

### Requirement: Credential Encryption Configuration
The system SHALL require an encryption key to be defined in the server environment configuration to safely secure organization and project-level credentials at rest.

#### Scenario: Server validating encryption key on startup
- **WHEN** the server application starts up
- **THEN** the configuration module SHALL validate the presence of the `LLM_ENCRYPTION_KEY` environment variable
- **AND** if missing, it SHALL either fail startup or disable the credential storage feature, logging a clear warning