## Purpose

A model selection must identify both *which provider endpoint* serves it and *which model* to request. Today this is a free-form string that is sometimes bare and sometimes prefix-qualified, re-parsed inconsistently. A canonical structured model reference removes the ambiguity and confines string parsing to a single boundary.

## ADDED Requirements

### Requirement: Canonical structured model reference

The system SHALL represent a model selection as a structured value carrying a provider instance (slug) and a bare model name. Storage, domain services, API payloads, agent definitions, and run records SHALL use the structured value; implementations SHALL NOT reconstruct provider identity by re-parsing concatenated strings.

#### Scenario: Structured reference stored

- **WHEN** a default model or agent model override is saved
- **THEN** the provider instance and the bare model name are stored as distinct values

#### Scenario: Model name containing slashes

- **WHEN** the bare model name itself contains slashes (e.g. a Vertex resource path)
- **THEN** the model name is preserved intact and is not interpreted as a provider prefix

### Requirement: String form confined to input and display edges

The `"slug/model"` string form SHALL be accepted only at input boundaries (CLI arguments, URL path segments, form values) and produced only for display. A single parse function SHALL convert it into the structured value by splitting on the first `/`, and SHALL reject a string without a provider segment.

#### Scenario: Parse a well-formed reference

- **WHEN** an input string is `openai-main/gpt-4o`
- **THEN** the parsed reference has provider `openai-main` and model `gpt-4o`

#### Scenario: Parse a reference whose model has slashes

- **WHEN** an input string is `google-vertex/publishers/google/models/gemini-2.5-flash`
- **THEN** the parsed reference has provider `google-vertex` and the full multi-segment model name

#### Scenario: Reject a missing provider segment

- **WHEN** an input string has no `/`
- **THEN** parsing fails with an error describing the expected `provider/model` form

#### Scenario: Reject an empty model

- **WHEN** an input string is `openai/` or `openai/   `
- **THEN** parsing fails with an error naming the missing model

### Requirement: Legacy values are normalized, not parsed

Strict edge parsing SHALL reject a string without a provider segment. Backfill of pre-existing stored values SHALL instead use a separate, context-aware normalization operation that resolves a bare or dialect-prefixed value to an instance. The two SHALL be distinct: normalization is used only during migration/inference and is never the runtime edge parser.

#### Scenario: Strict parser rejects bare input

- **WHEN** `ParseModelRef` receives a bare model name with no `/`
- **THEN** it returns an error

#### Scenario: Normalization resolves a bare stored value

- **WHEN** a stored value is a bare model name and the owning project has a determinable default instance for it
- **THEN** the backfill migration yields a structured reference (a provider instance slug and the bare model) with that instance

#### Scenario: Normalization does not corrupt resource paths

- **WHEN** a stored value is an unqualified multi-segment model id (e.g. `publishers/google/models/gemini-2.5-flash`)
- **THEN** normalization matches it against provider configs rather than splitting it at the first `/`

### Requirement: Single parse boundary

The system SHALL parse the string form in exactly one place. Provider-prefix extraction SHALL NOT occur in provider services, the model factory, cost resolution, or adapters.

#### Scenario: No ad-hoc prefix parsing

- **WHEN** any non-test server code handles a provider/model input
- **THEN** it does so through the structured reference or the single parse function, not by splitting the string itself

### Requirement: Structured references in agent configuration and run records

Agent model overrides and run records SHALL carry the provider instance and the bare model as structured values, and the run record SHALL additionally record the dialect actually used.

#### Scenario: Agent override recorded

- **WHEN** an agent runs with a model override
- **THEN** the run record stores the resolved model name, provider instance slug, and dialect

#### Scenario: Override resolution

- **WHEN** an agent definition has a model override referencing an instance slug
- **THEN** the executor builds the structured reference and creates the model against that instance

### Requirement: Backward-compatible legacy references

A legacy reference that prefixes a dialect rather than an instance slug SHALL continue to resolve, mapping to that dialect's default instance, so existing configuration requires no user action after migration.

#### Scenario: Existing prefixed reference

- **WHEN** a stored reference from before the migration prefixes a dialect (e.g. `openai/gpt-4o`)
- **THEN** it resolves to the dialect's default instance

#### Scenario: Existing bare reference

- **WHEN** a stored reference from before the migration has no provider segment
- **THEN** it resolves to the project's default instance for its recorded dialect
