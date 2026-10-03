## Purpose

A single, data-driven registry describes every supported LLM/embedding vendor — its wire protocol, authentication style, default endpoints, supported model types, vendor-specific configuration fields, and protocol compatibility quirks. All provider dispatch reads this registry, so adding a vendor is a data change rather than edits to switch statements scattered across the codebase.

## ADDED Requirements

### Requirement: Vendor definitions are data

The system SHALL define each supported vendor as a declarative registry entry carrying at minimum: a stable identifier, a display name, a wire protocol, an authentication style, default base URL(s) per model type, the model types served, and a UI ordering value. The registry SHALL be the single source of truth for the set of supported vendors.

#### Scenario: Registry exposes all configured vendors

- **WHEN** the provider registry is initialized
- **THEN** it contains an entry for each supported vendor, including the previously hardcoded `openai`, `deepseek`, `google` (as `gemini`) and `google-vertex`

#### Scenario: Adding a vendor requires no dispatch edits

- **WHEN** a new OpenAI-compatible vendor is added as a registry entry with a base URL and bearer auth
- **THEN** credentials can be stored, models listed, and generation performed for it without modifying provider dispatch code

### Requirement: Protocol and authentication are orthogonal

The registry SHALL separate a vendor's wire protocol (`openai-chat`, `google-genai`, `anthropic-messages`) from its authentication style (`bearer`, `api-key`, `x-api-key`, `x-goog-api-key`, `none`, `signed`). A vendor MAY override its authentication style for a specific protocol.

#### Scenario: Vendor shares protocol with a different auth style

- **WHEN** a vendor speaks the OpenAI Chat Completions protocol but authenticates with an `api-key` header (Azure OpenAI)
- **THEN** the same protocol adapter is used and only the credential-injection strategy differs

#### Scenario: Vendor exposes two protocols

- **WHEN** a vendor serves both a native protocol and an OpenAI-compatible facade on a sub-path
- **THEN** the registry entry selects the default protocol and MAY override the auth style for the other

### Requirement: Vendor-specific configuration fields

The registry SHALL describe vendor-specific configuration inputs (for example an Azure `api-version`, a LKEAP `secret_key`, a Volcengine region) as metadata that the API and UI render dynamically, rather than hardcoding a per-vendor form.

#### Scenario: Dynamic configuration rendered

- **WHEN** a vendor definition declares an extra configuration field
- **THEN** the provider configuration response exposes that field's key, type, required flag, and secret flag, and the UI surfaces the field's metadata read-only without a per-vendor code branch (submitted values are not yet persisted)

### Requirement: Catalog fetch strategy is per vendor

The system SHALL resolve a vendor's model catalog using a strategy declared on its definition: live OpenAI-compatible `/models`, native Google genai listing, or a static list.

#### Scenario: OpenAI-compatible vendor lists models live

- **WHEN** a vendor declares the OpenAI-compatible catalog strategy
- **THEN** the system fetches `GET {base_url}/models` and caches the results

#### Scenario: Static-only vendor

- **WHEN** a vendor declares the static catalog strategy
- **THEN** the system returns its embedded model list without a network call

### Requirement: Existing provider identifiers remain valid

Previously stored provider identifiers SHALL continue to resolve after the registry is introduced. The legacy identifier `google` SHALL alias to the `gemini` vendor; `google-vertex`, `openai`, and `deepseek` SHALL retain their meaning.

#### Scenario: Legacy google row resolves

- **WHEN** a stored provider config uses the identifier `google`
- **THEN** resolution maps it to the `gemini` vendor definition and credentials still decrypt and dispatch

### Requirement: Anthropic Messages protocol

The system SHALL support the Anthropic Messages wire protocol for chat generation, including tool use, authenticated with the `x-api-key` header, independent of the OpenAI-compatible adapter.

#### Scenario: Anthropic vendor generation

- **WHEN** a request is routed to an Anthropic-vendor model
- **THEN** the system calls the Messages API with the `x-api-key` header and a mandatory `max_tokens`, and maps the response content blocks to the internal content representation

#### Scenario: Anthropic tool round-trip

- **WHEN** the model returns a `tool_use` block and the tool result is supplied on the next turn
- **THEN** the result is sent as a `tool_result` block with the matching tool-use id
