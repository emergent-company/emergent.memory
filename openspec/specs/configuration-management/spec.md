# configuration-management Specification

## Purpose
TBD - created by archiving change integrate-infisical-secrets-management. Update Purpose after archive.
## Requirements
### Requirement: Infisical SDK Integration

The system SHALL integrate with Infisical secrets management platform using the Node.js SDK (@infisical/sdk) to fetch configuration and secrets at application startup.

#### Scenario: Server application fetches secrets on startup

- **GIVEN** the server application is starting
- **AND** Infisical bootstrap credentials are configured (INFISICAL_CLIENT_ID, INFISICAL_CLIENT_SECRET, INFISICAL_PROJECT_ID)
- **WHEN** the configuration module initializes
- **THEN** the system SHALL authenticate with Infisical using Universal Auth
- **AND** SHALL fetch all secrets from the configured project and environment
- **AND** SHALL inject fetched secrets into process.env for backward compatibility
- **AND** SHALL cache secrets in memory with 5-minute TTL

#### Scenario: Admin application fetches secrets on startup

- **GIVEN** the admin frontend is initializing
- **AND** Infisical bootstrap credentials are configured (VITE_INFISICAL_CLIENT_ID, VITE_INFISICAL_CLIENT_SECRET, VITE_INFISICAL_PROJECT_ID)
- **WHEN** the configuration service initializes
- **THEN** the system SHALL authenticate with Infisical using Universal Auth
- **AND** SHALL fetch all secrets from the 'admin' environment
- **AND** SHALL return secrets as configuration object for React context

#### Scenario: Workspace CLI fetches secrets before starting services

- **GIVEN** the workspace CLI is starting applications
- **AND** Infisical bootstrap credentials are configured
- **WHEN** the start command executes
- **THEN** the system SHALL fetch workspace configuration from Infisical
- **AND** SHALL inject NAMESPACE, ports, and shared configuration into environment
- **AND** SHALL pass environment variables to PM2 process manager

### Requirement: Graceful Fallback to Environment Files

The system SHALL fallback to .env file loading when Infisical is unavailable in local development environments, with production environments requiring Infisical availability.

#### Scenario: Local development with Infisical unavailable

- **GIVEN** NODE_ENV is set to 'development'
- **AND** Infisical SDK fetch fails (network error, authentication failure, or timeout)
- **WHEN** the configuration module initializes
- **THEN** the system SHALL log a warning about Infisical unavailability
- **AND** SHALL fallback to loading .env and .env.local files using dotenv
- **AND** SHALL continue application startup with .env configuration

#### Scenario: Production deployment with Infisical unavailable

- **GIVEN** NODE_ENV is set to 'production'
- **AND** Infisical SDK fetch fails
- **WHEN** the configuration module initializes
- **THEN** the system SHALL throw an error blocking application startup
- **AND** SHALL log critical error with Infisical connection details
- **AND** SHALL NOT fallback to .env files (enforce Infisical requirement)

#### Scenario: Infisical temporarily unavailable with valid cache

- **GIVEN** Infisical SDK fetch fails
- **AND** cached secrets exist with expiry timestamp in the future
- **WHEN** the configuration module fetches secrets
- **THEN** the system SHALL serve secrets from in-memory cache
- **AND** SHALL log warning about serving from cache
- **AND** SHALL continue application operation without interruption

### Requirement: Secret Caching with Automatic Refresh

The system SHALL cache fetched secrets in memory with automatic background refresh to minimize API calls and ensure availability during transient Infisical outages.

#### Scenario: Initial secret fetch with empty cache

- **GIVEN** the application is starting
- **AND** no cached secrets exist
- **WHEN** the configuration module fetches secrets
- **THEN** the system SHALL fetch fresh secrets from Infisical
- **AND** SHALL store secrets in memory cache with 5-minute expiry timestamp
- **AND** SHALL persist cache to file system (encrypted)
- **AND** SHALL schedule background refresh for 4 minutes from now

#### Scenario: Background refresh before cache expiry

- **GIVEN** cached secrets exist with 1 minute until expiry
- **AND** background refresh task executes
- **WHEN** the refresh task runs
- **THEN** the system SHALL fetch fresh secrets from Infisical
- **AND** SHALL update in-memory cache with new expiry timestamp
- **AND** SHALL update file system cache
- **AND** SHALL schedule next refresh for 4 minutes from now

#### Scenario: Refresh failure with valid cache

- **GIVEN** cached secrets exist with expiry timestamp in the future
- **AND** background refresh task fails (Infisical unreachable)
- **WHEN** the refresh task executes
- **THEN** the system SHALL log warning about refresh failure
- **AND** SHALL serve existing cached secrets (stale cache is acceptable)
- **AND** SHALL retry refresh in 1 minute instead of 4 minutes
- **AND** SHALL NOT interrupt application operation

#### Scenario: Cold start with file system cache available

- **GIVEN** the application is starting after restart
- **AND** file system cache exists from previous run
- **AND** Infisical SDK fetch fails on startup
- **WHEN** the configuration module initializes
- **THEN** the system SHALL load secrets from file system cache
- **AND** SHALL inject cached secrets into process.env
- **AND** SHALL log warning about using file system cache
- **AND** SHALL schedule background refresh to attempt Infisical fetch

### Requirement: Infisical Project Structure

The system SHALL organize secrets in Infisical using separate projects per deployment environment (dev/staging/production) with environments per application component (workspace/server/admin/docker).

#### Scenario: Development environment secret organization

- **GIVEN** the deployment environment is 'development'
- **WHEN** organizing secrets in Infisical
- **THEN** the system SHALL use project named 'spec-server-dev'
- **AND** SHALL create environments: 'workspace', 'server', 'admin', 'docker'
- **AND** SHALL store workspace config in 'workspace' environment (NAMESPACE, ports)
- **AND** SHALL store server config in 'server' environment (database, API keys)
- **AND** SHALL store admin config in 'admin' environment (VITE_* variables)
- **AND** SHALL store docker config in 'docker' environment (PostgreSQL, Zitadel)

#### Scenario: Production environment secret organization

- **GIVEN** the deployment environment is 'production'
- **WHEN** organizing secrets in Infisical
- **THEN** the system SHALL use project named 'spec-server-production'
- **AND** SHALL create environments: 'workspace', 'server', 'admin', 'docker'
- **AND** SHALL use separate machine identity credentials from dev/staging
- **AND** SHALL enforce stricter access control policies

### Requirement: Machine Identity Authentication

The system SHALL authenticate with Infisical using Universal Auth (machine identity) with unique credentials per application and environment.

#### Scenario: Server authentication with machine identity

- **GIVEN** the server application is configured with Infisical credentials
- **WHEN** authenticating with Infisical
- **THEN** the system SHALL use INFISICAL_CLIENT_ID from environment
- **AND** SHALL use INFISICAL_CLIENT_SECRET from environment (stored in .env.local)
- **AND** SHALL call client.auth().universalAuth.login() with credentials
- **AND** SHALL receive access token valid for fetching secrets
- **AND** SHALL use INFISICAL_ENVIRONMENT to determine which environment to fetch from

#### Scenario: Authentication failure handling

- **GIVEN** machine identity credentials are invalid or expired
- **WHEN** authenticating with Infisical
- **THEN** the system SHALL throw authentication error
- **AND** SHALL log error with client ID (NOT secret) for debugging
- **AND** SHALL trigger fallback to .env in development
- **AND** SHALL block startup in production with clear error message

### Requirement: Configuration Migration from .env Files

The system SHALL provide tooling to migrate existing .env files to Infisical with validation and rollback capabilities.

#### Scenario: Automated migration of secrets to Infisical

- **GIVEN** a migration script is executed
- **AND** .env and .env.local files exist with current configuration
- **WHEN** the migration script runs
- **THEN** the system SHALL parse all .env.local files (secrets)
- **AND** SHALL upload each key-value pair to appropriate Infisical project/environment
- **AND** SHALL preserve comments as secret descriptions in Infisical
- **AND** SHALL create backup of original .env files before migration
- **AND** SHALL log summary of migrated secrets (count per environment)

#### Scenario: Migration validation

- **GIVEN** secrets have been migrated to Infisical
- **WHEN** validation script runs
- **THEN** the system SHALL fetch all secrets from Infisical
- **AND** SHALL compare with original .env files
- **AND** SHALL report any missing or mismatched values
- **AND** SHALL verify application can start with Infisical secrets
- **AND** SHALL confirm all required variables are present

#### Scenario: Migration rollback

- **GIVEN** migration to Infisical has issues
- **AND** backup .env files exist
- **WHEN** rollback script executes
- **THEN** the system SHALL restore .env files from backup
- **AND** SHALL revert code changes to remove Infisical integration
- **AND** SHALL verify application starts with restored .env files
- **AND** SHALL log rollback completion status

### Requirement: Secret Access Logging and Audit Trail

The system SHALL log all secret fetch operations and leverage Infisical's built-in audit trail for compliance and security monitoring.

#### Scenario: Logging successful secret fetch

- **GIVEN** the configuration module fetches secrets from Infisical
- **WHEN** the fetch operation succeeds
- **THEN** the system SHALL log info-level message with:
  - Project ID
  - Environment name
  - Number of secrets fetched
  - Fetch latency in milliseconds
  - Whether served from cache

#### Scenario: Logging failed secret fetch

- **GIVEN** the configuration module attempts to fetch secrets
- **AND** the fetch operation fails
- **WHEN** the error occurs
- **THEN** the system SHALL log error-level message with:
  - Error type (network, authentication, timeout)
  - Project ID and environment
  - Whether fallback was triggered
  - Suggested remediation steps

#### Scenario: Infisical audit trail for secret access

- **GIVEN** application fetches secrets from Infisical
- **WHEN** the SDK authenticates and fetches secrets
- **THEN** Infisical SHALL record audit log entry with:
  - Machine identity that accessed secrets
  - Timestamp of access
  - Project and environment accessed
  - IP address of requesting application
- **AND** audit logs SHALL be viewable in Infisical UI
- **AND** audit logs SHALL be retained per Infisical retention policy

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

### Requirement: Bootstrap Credentials Management

The system SHALL require minimal bootstrap credentials stored in .env.local files to authenticate with Infisical, with all other configuration fetched from Infisical.

#### Scenario: Server bootstrap credentials

- **GIVEN** the server application needs to fetch secrets from Infisical
- **WHEN** checking bootstrap requirements
- **THEN** the system SHALL require these variables in .env or .env.local:
  - INFISICAL_SITE_URL (URL of self-hosted Infisical instance)
  - INFISICAL_PROJECT_ID (project identifier)
  - INFISICAL_CLIENT_ID (machine identity client ID)
  - INFISICAL_CLIENT_SECRET (machine identity client secret, .env.local only)
  - INFISICAL_ENVIRONMENT (environment name: 'server', 'admin', etc.)
- **AND** all other configuration SHALL be fetched from Infisical

#### Scenario: Validation of bootstrap credentials on startup

- **GIVEN** the application is starting
- **WHEN** the configuration module initializes
- **THEN** the system SHALL validate all required bootstrap variables are present
- **AND** SHALL throw error if any bootstrap variable is missing
- **AND** SHALL log error message listing missing variables
- **AND** SHALL NOT attempt to fetch from Infisical with incomplete credentials

