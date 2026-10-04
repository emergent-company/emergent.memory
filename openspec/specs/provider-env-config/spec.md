# provider-env-config Specification

## Purpose
A self-host operator who has no database provider configuration must be able to
select any registered LLM vendor using environment variables alone. The four
legacy providers keep their historical variables; every other registered vendor
is configured through one deterministic convention derived from the vendor
registry, so adding a vendor to the registry makes it env-configurable without
touching model-construction code.

## Requirements

### Requirement: Environment configuration is registry-driven

The system SHALL resolve environment configuration for any vendor present in
the vendor registry, without a per-vendor code branch. For a registered vendor
with dialect `P` it SHALL read `PREFIX_API_KEY`, `PREFIX_BASE_URL`, and
`PREFIX_MODEL`, where `PREFIX` is `P` upper-cased with every `-` replaced by `_`.
A vendor that is not registered and is not one of the four legacy providers
SHALL be rejected rather than resolved against an arbitrary default endpoint.

#### Scenario: Non-legacy vendor resolves from env

- **WHEN** no database credential exists and `ANTHROPIC_API_KEY` is set
- **THEN** a model routed to the `anthropic` vendor resolves using the Anthropic
  Messages protocol and the registry's default endpoint, with no code change

#### Scenario: Base URL and model overrides apply

- **WHEN** `OPENROUTER_BASE_URL` or `OPENROUTER_MODEL` is set
- **THEN** the override replaces the registry default endpoint or model
  respectively

#### Scenario: Unknown vendor is rejected

- **WHEN** a model reference names a vendor that is neither registered nor a
  legacy provider
- **THEN** resolution fails with an error instead of selecting a default
  endpoint

### Requirement: Deterministic prefix scheme

The environment prefix for a vendor SHALL be derived deterministically from its
registry dialect by upper-casing and replacing `-` with `_`; the system SHALL
NOT maintain a separate hand-written prefix table that could drift from the
registry.

#### Scenario: Hyphenated dialect

- **WHEN** the vendor dialect is `azure-openai`
- **THEN** the environment prefix is `AZURE_OPENAI`

### Requirement: Legacy providers are preserved

The four legacy providers SHALL keep their historical environment variables,
defaults, and protocol selection exactly as before the generalization:
`DEEPSEEK_API_KEY`/`DEEPSEEK_MODEL`, `OPENAI_API_KEY`/`OPENAI_BASE_URL`/
`OPENAI_MODEL`, `GOOGLE_API_KEY`, and `GCP_PROJECT_ID` + `VERTEX_AI_LOCATION`.

#### Scenario: Legacy DeepSeek default endpoint

- **WHEN** `DEEPSEEK_API_KEY` is set and no base URL is configured
- **THEN** the model is built against `https://api.deepseek.com/v1`

#### Scenario: Legacy OpenAI default endpoint and protocol

- **WHEN** `OPENAI_API_KEY` is set with no `OPENAI_BASE_URL`
- **THEN** the model is built against `https://api.openai.com/v1` using the
  Responses protocol selected by the first-party host rule

### Requirement: Missing credential never enables a vendor

A registered vendor SHALL be considered enabled only when its environment API
key is present. A configured base URL or model SHALL NOT enable a vendor on its
own, so no vendor can be silently activated without a credential.

#### Scenario: Model without key is not enabled

- **WHEN** `ANTHROPIC_MODEL` is set but `ANTHROPIC_API_KEY` is not
- **THEN** the vendor is not enabled and building a model for it returns a
  "`ANTHROPIC_API_KEY` is not set" error

### Requirement: Database configuration takes precedence over environment

When a database provider credential exists for the request context, the system
SHALL use it and SHALL NOT fall back to environment configuration. Environment
configuration SHALL be consulted only when no stored credential is available.

#### Scenario: DB credential wins over env

- **WHEN** a database credential for a vendor exists and the same vendor is also
  configured through environment variables
- **THEN** the model is built from the database credential's endpoint and key
