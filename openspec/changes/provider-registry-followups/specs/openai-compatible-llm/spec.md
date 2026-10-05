## MODIFIED Requirements

### Requirement: Tools combined with a graded reasoning effort

The Responses adapter SHALL support a graded reasoning effort, sent as `{"reasoning":{"effort":...}}`. It SHALL send `tools` and the reasoning effort in the same request, because the Responses protocol permits that combination while Chat Completions rejects it for the newest OpenAI reasoning models. The effort SHALL be reachable end-to-end: a per-run effort (from the agentcompat `reasoning_effort` request field) is applied to the resolved model through the `ReasoningEffortConfigurator` interface, and the usage-tracking wrapper returned in production SHALL delegate that interface to the wrapped adapter.

#### Scenario: Tools and reasoning effort sent together

- **WHEN** a Responses model with a configured reasoning effort receives a request that declares tools
- **THEN** the request body SHALL include both the `tools` array and `reasoning.effort`

#### Scenario: Reasoning effort omitted when unset

- **WHEN** no reasoning effort is configured
- **THEN** the request body SHALL omit the `reasoning` field

#### Scenario: Per-run effort reaches the adapter through the tracking wrapper

- **WHEN** an agent run carries a non-empty `reasoning_effort` and resolves to a Responses model
- **THEN** the effort SHALL be applied to the adapter even though the model is wrapped by the usage-tracking model

#### Scenario: Providers without effort support ignore the value

- **WHEN** an agent run carries a `reasoning_effort` but resolves to a model that does not implement `ReasoningEffortConfigurator`
- **THEN** the run SHALL proceed unchanged and the effort SHALL NOT be sent

## ADDED Requirements

### Requirement: Responses reasoning items are replayed across tool loops

The Responses adapter SHALL preserve the opaque reasoning item returned by a reasoning model and replay it with the conversation history. It SHALL capture both the reasoning item id (e.g. `rs_...`) and, when present, the opaque `encrypted_content` blob, and SHALL request `reasoning.encrypted_content` via the request `include` array. When a replayed `function_call` was produced by a reasoning item, the `input` array SHALL contain that reasoning item immediately preceding the `function_call`, because OpenAI requires a reasoning item and its following model-emitted item to stay contiguous and rejects a reasoning item without its following item. The adapter SHALL replay `encrypted_content` in preference to the id and SHALL NOT replay an id when `encrypted_content` is available, because under Zero Data Retention (`store:false`) reasoning items are not persisted and an id-only replay is rejected (HTTP 404, "Items are not persisted when `store` is set to false").

#### Scenario: Reasoning item replayed before its function call

- **WHEN** a previous Responses turn returned a reasoning item followed by a `function_call`, and that turn is replayed with the tool output
- **THEN** `input` SHALL include the reasoning item immediately before the `function_call`, carrying its id when no `encrypted_content` was returned

#### Scenario: Reasoning id survives ADK session persistence

- **WHEN** the assistant turn is persisted to and reloaded from the ADK session
- **THEN** the reasoning item id and any `encrypted_content` SHALL still be available to replay with the `function_call`

#### Scenario: Stateless replay under Zero Data Retention

- **WHEN** a reasoning item is returned with `encrypted_content` (as under `store:false`/ZDR) and its turn is replayed
- **THEN** the replayed reasoning item SHALL carry `encrypted_content` and SHALL NOT carry the id, so the API does not reject it as an unresolvable stored item

#### Scenario: Encrypted reasoning requested

- **WHEN** the adapter issues a Responses request
- **THEN** the request SHALL include `reasoning.encrypted_content` so stateless replay yields the blob

#### Scenario: Text-only reasoning is not replayed as a dangling item

- **WHEN** a reasoning item is not followed by a `function_call`
- **THEN** no standalone reasoning item SHALL be emitted that would lack its required following item
