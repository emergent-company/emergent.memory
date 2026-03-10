## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Organization-Level Credential Storage
**Reason**: Replaced by `provider-config` capability — credentials are now stored in `kb.org_provider_configs` with model selections in the same row. The `organization_provider_credentials` table is dropped.
**Migration**: Use `PUT /api/v1/organizations/:orgId/providers/:provider` with `{ apiKey }` or `{ serviceAccountJson, gcpProject, location }` to store credentials.

### Requirement: Resolving with server environment fallback
**Reason**: The env-var fallback (`GOOGLE_API_KEY`, `LLM_MODEL`) is removed from the request-context resolution path. Silent fallback caused confusing 503 errors on misconfigured projects.
**Migration**: Configure credentials explicitly at the org or project level. Background/test contexts (no `projectID` or `orgID` in context) still receive `nil, nil` and may fall back at the caller's discretion.
