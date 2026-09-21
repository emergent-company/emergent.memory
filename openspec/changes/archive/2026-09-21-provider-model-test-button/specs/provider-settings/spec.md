## ADDED Requirements

### Requirement: Test a default model from the settings panel

The **Default models** panel SHALL offer a Test action beside each dropdown (default generative model and embedding model). The action SHALL run a live generate or embed call for the model currently selected in that dropdown, report the outcome to the user, and SHALL NOT modify the stored default models or interfere with the panel's save-on-change behaviour.

#### Scenario: Test the selected generative model

- **WHEN** the user selects a generative model and activates its Test action
- **THEN** a live generate call is made against that exact model using the project's configured provider credentials
- **AND** the user sees a success message naming the tested model, its reply, and the round-trip latency

#### Scenario: Test the selected embedding model

- **WHEN** the user selects an embedding model and activates its Test action
- **THEN** a live embed call is made against that exact model using the project's configured provider credentials
- **AND** the user sees a success message naming the model and the round-trip latency

#### Scenario: Reject an empty selection

- **WHEN** the user activates Test with no model selected
- **THEN** no provider call is made and the user sees an error message telling them to select a model

#### Scenario: Reject an unprefixed model

- **WHEN** the selected value is not in `provider/model` form
- **THEN** no provider call is made and the user sees an error message describing the expected form

#### Scenario: Surface a provider failure

- **WHEN** the provider call fails (bad credentials, unknown model, upstream error)
- **THEN** the user sees an error message carrying the provider's failure detail

#### Scenario: Test does not persist

- **WHEN** a model test succeeds or fails
- **THEN** the stored default generative and embedding models are unchanged

#### Scenario: Prevent duplicate in-flight tests

- **WHEN** a test request is in flight
- **THEN** the Test action is disabled and shows a busy indicator until the call returns

### Requirement: Test an explicit model through the provider test API

`POST /api/v1/projects/:projectId/providers/:provider/test` SHALL accept an optional JSON body `{"model": "<name>", "modelType": "generative"|"embedding"}`. When `model` is present, the endpoint SHALL test exactly that model using the project's configured provider credentials. When the body is absent or `model` is empty, the endpoint SHALL preserve its existing behaviour of testing the credential's configured generative model and then its embedding model.

#### Scenario: Explicit generative model

- **WHEN** the body carries a `model` and `modelType` is `generative` or omitted
- **THEN** a generate call is made for exactly that model and the response reports the model, reply, and latency

#### Scenario: Explicit embedding model

- **WHEN** the body carries a `model` and `modelType` is `embedding`
- **THEN** an embed call is made for exactly that model and the response reports the verified embedding model

#### Scenario: No body preserves existing behaviour

- **WHEN** the request carries no body
- **THEN** the endpoint tests the credential's configured generative model and then its embedding model, exactly as before

#### Scenario: Reject an unknown model type

- **WHEN** the body carries a `modelType` other than `generative` or `embedding`
- **THEN** the endpoint responds 400 with a message naming the accepted values

#### Scenario: Upstream failure

- **WHEN** the requested model cannot be tested (bad credentials, unknown model, upstream error)
- **THEN** the endpoint responds 400 with the provider's failure detail
