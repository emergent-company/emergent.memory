## ADDED Requirements

### Requirement: Unified Provider Config Schema
The system SHALL store provider configuration (credentials + model selection) in two tables of identical shape: `kb.org_provider_configs` (FK → `kb.orgs`) and `kb.project_provider_configs` (FK → `kb.projects`). Each table SHALL enforce a unique constraint on `(scope_id, provider)`.

#### Scenario: Org config row structure
- **WHEN** an organization saves credentials for a provider
- **THEN** the system SHALL store `id`, `org_id`, `provider`, `encrypted_credential`, `encryption_nonce`, `gcp_project`, `location`, `generative_model`, `embedding_model`, `created_at`, `updated_at` in a single `org_provider_configs` row
- **AND** SHALL enforce `UNIQUE (org_id, provider)` so only one config per provider per org exists

#### Scenario: Project config row structure
- **WHEN** a project saves credentials for a provider
- **THEN** the system SHALL store the same columns with `project_id` instead of `org_id` in `project_provider_configs`
- **AND** SHALL enforce `UNIQUE (project_id, provider)`

#### Scenario: Cascade delete on org deletion
- **WHEN** an organization is deleted
- **THEN** all `org_provider_configs` rows for that org SHALL be deleted automatically via FK CASCADE

#### Scenario: Cascade delete on project deletion
- **WHEN** a project is deleted
- **THEN** all `project_provider_configs` rows for that project SHALL be deleted automatically via FK CASCADE

### Requirement: Atomic Credential and Model Save
The system SHALL accept credentials and optional model selections in a single `PUT /api/v1/organizations/:orgId/providers/:provider` request and write them atomically. If `generativeModel` or `embeddingModel` are omitted, the system SHALL auto-select the top-ranked model from the synced catalog.

#### Scenario: Save Google AI config with explicit models
- **WHEN** `PUT /api/v1/organizations/:orgId/providers/google-ai` is called with `{ apiKey, generativeModel, embeddingModel }`
- **THEN** the system SHALL encrypt the API key and upsert a single row in `org_provider_configs`
- **AND** SHALL store the provided model names in that same row

#### Scenario: Save with auto-default models
- **WHEN** `PUT /api/v1/organizations/:orgId/providers/google-ai` is called with only `{ apiKey }` (no model names)
- **THEN** after a successful live credential test and catalog sync, the system SHALL select the top-ranked generative and embedding models from the catalog
- **AND** SHALL store those auto-selected names in the config row

#### Scenario: Save Vertex AI config
- **WHEN** `PUT /api/v1/organizations/:orgId/providers/vertex-ai` is called with `{ serviceAccountJson, gcpProject, location }`
- **THEN** the system SHALL encrypt the service account JSON and store `gcp_project` and `location` in the config row

#### Scenario: Upsert on re-configure
- **WHEN** `PUT /api/v1/organizations/:orgId/providers/:provider` is called and a config row already exists for that org+provider
- **THEN** the system SHALL update the existing row (upsert) rather than create a duplicate

### Requirement: Live Credential Test on Save
The system SHALL test the submitted credentials live against the provider API during the save request, before committing to the database.

#### Scenario: Valid credentials accepted
- **WHEN** credentials are submitted and the live test call to the provider API succeeds
- **THEN** the system SHALL proceed to catalog sync and save

#### Scenario: Invalid credentials rejected
- **WHEN** credentials are submitted and the live test call returns an authentication error
- **THEN** the system SHALL return HTTP 422 with a descriptive error and SHALL NOT persist the credentials

#### Scenario: Catalog sync timeout does not block save
- **WHEN** the catalog sync request to the provider API times out or fails (after a successful auth test)
- **THEN** the system SHALL fall back to a static model list and SHALL still save the config row

### Requirement: Project-Level Provider Config Override
The system SHALL allow a project to store its own `project_provider_configs` row via `PUT /api/v1/projects/:projectId/providers/:provider`, giving that project independent credentials and model selections.

#### Scenario: Save project-level config
- **WHEN** `PUT /api/v1/projects/:projectId/providers/:provider` is called with valid credentials
- **THEN** the system SHALL upsert a row in `project_provider_configs` for that project+provider

#### Scenario: Delete project config to revert to org
- **WHEN** `DELETE /api/v1/projects/:projectId/providers/:provider` is called
- **THEN** the system SHALL remove the row from `project_provider_configs`
- **AND** subsequent requests for that project SHALL resolve via the org config

### Requirement: Two-Step Resolution Hierarchy
The system SHALL resolve the effective provider config for a request using the following ordered steps: (1) look up `project_provider_configs` for the project from context; (2) if not found, look up `org_provider_configs` for the org from context; (3) if not found, return a hard error.

#### Scenario: Project config present
- **WHEN** the request context contains a `projectID`
- **AND** a row exists in `project_provider_configs` for `(projectID, provider)`
- **THEN** the system SHALL use that project config

#### Scenario: Fall through to org config
- **WHEN** no row exists in `project_provider_configs` for the project
- **AND** a row exists in `org_provider_configs` for `(orgID, provider)` (where `orgID` is derived from the project)
- **THEN** the system SHALL use the org config

#### Scenario: Hard error when no config found
- **WHEN** no row exists at either level for the requested provider
- **THEN** the system SHALL return a descriptive error: "no provider configured for this project or its organization"
- **AND** SHALL NOT fall back silently to environment variables

#### Scenario: Background context (no project or org in context)
- **WHEN** both `projectID` and `orgID` are absent from the context (background jobs, tests)
- **THEN** `Resolve` SHALL return `nil, nil` to allow callers to fall back gracefully

### Requirement: DB-Stored Generative Model is Authoritative
At generative model call sites, the `generative_model` value stored in the resolved config SHALL take precedence over any hardcoded model name passed by the caller.

#### Scenario: DB model name wins over caller's name
- **WHEN** `CreateModelWithName` is called with a non-empty `modelName` argument
- **AND** the resolved config has a non-empty `generative_model`
- **THEN** the system SHALL use `cred.GenerativeModel` (the DB value), ignoring the caller's argument

#### Scenario: Caller's name used as fallback
- **WHEN** the resolved config has an empty `generative_model`
- **THEN** the system SHALL use the caller's provided `modelName`

#### Scenario: Env-var as last resort
- **WHEN** both the DB value and the caller's argument are empty
- **THEN** the system SHALL fall back to the `LLM_MODEL` environment variable

### Requirement: Get Current Provider Config
The system SHALL provide `GET /api/v1/organizations/:orgId/providers/:provider` to retrieve the current org config (without decrypted credentials) and `GET /api/v1/projects/:projectId/providers/:provider` for project configs.

#### Scenario: Get org provider config
- **WHEN** `GET /api/v1/organizations/:orgId/providers/:provider` is called
- **THEN** the system SHALL return the config row with model names but with credential fields omitted or masked

#### Scenario: Get project provider config
- **WHEN** `GET /api/v1/projects/:projectId/providers/:provider` is called
- **THEN** the system SHALL return the project config row if it exists, or 404 if not
