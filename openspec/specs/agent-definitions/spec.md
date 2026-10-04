# agent-definitions Specification

## Purpose
Defines the write contract for agent definitions (the personas Memory can become),
including the validation of their model reference.

## Requirements

### Requirement: Agent model references are validated as structured references

An agent definition's `model.name` SHALL be accepted only in the structured
`provider/model` string form, parsed by the canonical strict parser (`modelref.Parse`),
which splits on the first `/` and preserves any slashes in the model segment. The provider
segment and the model segment MUST both be non-empty. A stored value SHALL be normalized to
the canonical `provider/model` form. An empty `model` (no override) SHALL remain valid and
means "use the project default".

#### Scenario: Malformed empty-provider reference is rejected

- **WHEN** a create or update request sets `model.name` to `/deepseek-v4-flash`
- **THEN** the request is rejected with a 400 naming the expected `provider/model` form
- **AND** no definition is written

#### Scenario: Reference with no provider segment is rejected

- **WHEN** a create or update request sets `model.name` to `deepseek-v4-flash`
- **THEN** the request is rejected with a 400

#### Scenario: Reference with an empty model segment is rejected

- **WHEN** a create or update request sets `model.name` to `openai/`
- **THEN** the request is rejected with a 400

#### Scenario: Well-formed reference is accepted

- **WHEN** a create or update request sets `model.name` to `openai/deepseek-v4-flash`
- **THEN** the definition is written with that canonical reference

#### Scenario: No override is allowed

- **WHEN** a request omits `model` or sends an empty `model.name`
- **THEN** the definition is written with no model override and inherits the project default
