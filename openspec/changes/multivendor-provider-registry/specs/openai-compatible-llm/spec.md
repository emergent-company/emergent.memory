## Purpose

The OpenAI Chat Completions runtime becomes one selectable wire protocol among several. A model's vendor determines which protocol adapter and credential-injection strategy are used, instead of the runtime deciding from a fixed `openai`/`deepseek` provider string.

## ADDED Requirements

### Requirement: Protocol selection by vendor definition

The system SHALL select the LLM client by the resolved vendor's declared protocol, not by a hardcoded provider-string comparison. Vendors declaring the OpenAI-compatible protocol SHALL use the Chat Completions adapter; vendors declaring the Anthropic Messages protocol SHALL use the Anthropic adapter; Google vendors SHALL use the genai adapter.

#### Scenario: OpenAI-compatible vendor routes to the chat adapter

- **WHEN** a resolved credential's vendor declares the OpenAI-compatible protocol
- **THEN** `CreateModelWithName` constructs the Chat Completions adapter with the vendor's base URL

#### Scenario: Anthropic vendor routes to the Messages adapter

- **WHEN** a resolved credential's vendor declares the Anthropic Messages protocol
- **THEN** `CreateModelWithName` constructs the Anthropic Messages adapter

### Requirement: Credential injection by auth style

The system SHALL inject credentials according to the vendor's declared authentication style: `Authorization: Bearer`, `api-key`, `x-api-key`, `x-goog-api-key`, no authentication, or a registered signer hook.

#### Scenario: Bearer vendor

- **WHEN** a vendor declares bearer authentication and an API key is configured
- **THEN** requests carry `Authorization: Bearer <key>`

#### Scenario: Header-key vendor

- **WHEN** a vendor declares the `api-key` or `x-api-key` style
- **THEN** requests carry that header with the configured key and no bearer header

#### Scenario: Keyless local vendor

- **WHEN** a vendor declares no authentication and no key is configured
- **THEN** requests are sent without an authorization header and no error is raised for the missing key
