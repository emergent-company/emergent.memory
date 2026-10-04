# llm-network-disabled Specification

## Purpose
`LLM_NETWORK_DISABLED` is an operator kill switch that guarantees the server
makes no provider-backed LLM network calls. Enforcing it at model creation —
not only at module wiring — makes that guarantee independent of how a caller
reaches `ModelFactory`.

## Requirements

### Requirement: Kill switch hard-gates model creation

When `LLM_NETWORK_DISABLED` is on, `ModelFactory.CreateModel` and
`ModelFactory.CreateModelWithName` SHALL return `ErrNetworkDisabled` without
resolving credentials or constructing any provider-backed model. The gate SHALL
apply on both the environment-variable path and the database-credential
hierarchy, and SHALL take precedence over the configured model so no outbound
provider request is possible.

#### Scenario: Environment-var model blocked

- **WHEN** `LLM_NETWORK_DISABLED` is on and a provider model is configured via
  environment variables
- **THEN** `CreateModel` SHALL return `ErrNetworkDisabled`
- **AND** no provider model SHALL be constructed

#### Scenario: Database-credential model blocked

- **WHEN** `LLM_NETWORK_DISABLED` is on and a model resolves from the project/org
  credential hierarchy
- **THEN** `CreateModelWithName` SHALL return `ErrNetworkDisabled`
- **AND** credential resolution SHALL NOT be performed

#### Scenario: Flag off is unchanged

- **WHEN** `LLM_NETWORK_DISABLED` is off
- **THEN** model creation SHALL proceed exactly as before the gate existed

### Requirement: Deterministic test-LLM is exempt

The deterministic canned test-LLM makes no network calls, so it SHALL take
precedence over the kill switch: when a project resolves to the test-LLM,
`CreateModel` and `CreateModelWithName` SHALL return it even while
`LLM_NETWORK_DISABLED` is on.

#### Scenario: Test-LLM returned while disabled

- **WHEN** `LLM_NETWORK_DISABLED` is on and the project has a test-LLM configured
- **THEN** `CreateModel`/`CreateModelWithName` SHALL return the canned model
- **AND** SHALL NOT return `ErrNetworkDisabled`

### Requirement: Disabled LLM reports not enabled

`LLMConfig.IsEnabled()` SHALL continue to return false while
`LLM_NETWORK_DISABLED` is on, so module wiring remains consistent with the
creation-time gate.

#### Scenario: IsEnabled false while disabled

- **WHEN** `LLM_NETWORK_DISABLED` is on
- **THEN** `LLMConfig.IsEnabled()` SHALL return false
