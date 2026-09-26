## Purpose

A project may configure more than one LLM provider endpoint, including more than one endpoint that speaks the same dialect (e.g. an Azure OpenAI deployment and a local LiteLLM proxy). Provider instances give each config row a stable, project-scoped identity so credentials, model selection, pricing, and usage can address it unambiguously.

## ADDED Requirements

### Requirement: Provider instance identity

Each provider config SHALL have a project-scoped slug that is unique within the project, and a dialect that selects the wire protocol and authentication behavior. The dialect SHALL be one of the supported provider dialects.

#### Scenario: Instance created with a user-supplied slug

- **WHEN** a caller creates a provider config with a dialect and an explicit slug
- **THEN** the config is stored with that slug and dialect, unique within the project

#### Scenario: Instance created without a slug

- **WHEN** a caller saves a provider config with a dialect and no slug
- **THEN** the slug defaults to the dialect name
- **AND** if an instance with that slug already exists it is updated in place, not duplicated

#### Scenario: Duplicate slug updates in place

- **WHEN** a caller saves a provider config with a slug that already exists in the project
- **THEN** the existing instance is updated (credentials/models), and no second row is created

#### Scenario: Second instance requires an explicit slug

- **WHEN** a caller wants a second instance of a dialect that already has its default instance
- **THEN** the caller must supply a distinct, explicit slug

#### Scenario: Slug must not shadow a different dialect

- **WHEN** a caller creates an instance whose slug equals a dialect other than its own dialect
- **THEN** the request is rejected with a message explaining the reserved name

### Requirement: Multiple instances per dialect

A project SHALL be permitted to hold multiple provider configs that share the same dialect, distinguished by slug.

#### Scenario: Two OpenAI-compatible endpoints

- **WHEN** a project configures two configs with dialect `openai` and distinct slugs and distinct base URLs
- **THEN** both configs are stored and both are listed for the project

#### Scenario: Distinct credentials

- **WHEN** a model reference names one of the two instances
- **THEN** the request uses that instance's credentials and base URL, not the other instance's

### Requirement: Deterministic default-instance selection

When no instance is named, the system SHALL select a default instance deterministically: iterate dialects in the configured preference order and, within the chosen dialect, prefer the instance whose slug equals the dialect, then the lexicographically smallest slug. An unrelated slug SHALL NOT pre-empt the dialect-named default.

#### Scenario: Dialect-named default wins

- **WHEN** a project has instances `a-local` (dialect `openai`) and `openai` (dialect `openai`) and resolution names no instance
- **THEN** the `openai` instance is selected, not `a-local`

#### Scenario: Single instance unaffected

- **WHEN** a dialect has exactly one instance
- **THEN** that instance is selected

### Requirement: Slug-first resolution with dialect fallback

Resolving a model reference SHALL resolve the provider by instance slug first. If no instance slug matches but the segment names a supported dialect, resolution SHALL fall back to that dialect's default instance.

#### Scenario: Exact instance slug

- **WHEN** a reference names an existing instance slug
- **THEN** that instance's config is used

#### Scenario: Legacy dialect prefix

- **WHEN** a reference names a dialect that has a default instance and no instance slug of that name
- **THEN** the dialect's default instance is used

#### Scenario: Unknown provider segment

- **WHEN** a reference names a segment that is neither an instance slug nor a supported dialect
- **THEN** resolution fails with an error naming the unsupported provider

### Requirement: Address provider instances by slug

The provider config API, SDK, and CLI SHALL address a specific instance by its slug for get, update, delete, and test operations. A dialect value SHALL be accepted as a legacy alias for the dialect's default instance.

#### Scenario: Get by slug

- **WHEN** a caller requests a provider config by slug
- **THEN** the matching instance's public metadata is returned

#### Scenario: Legacy dialect alias

- **WHEN** a caller addresses a provider by a dialect name that has a default instance
- **THEN** the request resolves to that default instance

#### Scenario: Unknown slug

- **WHEN** a caller addresses a slug that does not exist in the project
- **THEN** the request fails with a not-found error

### Requirement: Provider config listing shows instance identity

The provider config listing SHALL return every instance for the project, each carrying its slug and dialect so callers can distinguish instances that share a dialect.

#### Scenario: Listing multiple same-dialect instances

- **WHEN** a project has two configs with the same dialect and distinct slugs
- **THEN** the listing returns both, each with its slug and dialect
