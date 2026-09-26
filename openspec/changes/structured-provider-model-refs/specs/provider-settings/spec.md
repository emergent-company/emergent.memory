## Purpose

The Project Settings providers surface — which provider instances a project can use, which models and default models it selects, and how it verifies a model — must speak provider instances (slug) and structured model references. This delta captures the changes to that surface.

## ADDED Requirements

### Requirement: Configure provider instances in settings

The settings surface SHALL list a project's provider instances by slug and dialect, allow adding an instance of a chosen dialect with an optional user-supplied slug, and allow editing and deleting a specific instance.

#### Scenario: Two same-dialect instances listed

- **WHEN** a project has two instances with the same dialect and distinct slugs
- **THEN** both appear in the provider list, each labelled with its slug and dialect

#### Scenario: Add an instance with a custom slug

- **WHEN** the user adds a provider instance and supplies a slug
- **THEN** the instance is created with that slug and appears in the list

#### Scenario: Delete one instance

- **WHEN** the user deletes one of two same-dialect instances
- **THEN** only that instance is removed and the other remains configured

### Requirement: Model dropdowns submit structured references

Default-model and agent-model selection controls SHALL submit a structured reference (provider instance and bare model) rather than a concatenated string.

#### Scenario: Selecting a model from an instance

- **WHEN** the user selects a model offered by an instance
- **THEN** the submitted selection identifies that instance and the bare model

#### Scenario: Two instances offering the same model name

- **WHEN** two instances of the same dialect both offer a model with the same name
- **THEN** the selection distinguishes which instance serves the chosen model

## MODIFIED Requirements

### Requirement: Test a default model from the settings panel

The **Default models** panel SHALL offer a Test action beside each dropdown (default generative model and embedding model). The action SHALL run a live generate or embed call for the model currently selected in that dropdown, report the outcome to the user, and SHALL NOT modify the stored default models or interfere with the panel's save-on-change behaviour. The Test action SHALL resolve the selected model through its structured reference, using the selected provider instance's credentials.

#### Scenario: Test the selected generative model

- **WHEN** the user selects a generative model and activates its Test action
- **THEN** a live generate call is made against that exact model using the selected provider instance's credentials
- **AND** the user sees a success message naming the tested model, its reply, and the round-trip latency

#### Scenario: Test the selected embedding model

- **WHEN** the user selects an embedding model and activates its Test action
- **THEN** a live embed call is made against that exact model using the selected provider instance's credentials
- **AND** the user sees a success message naming the model and the round-trip latency

#### Scenario: Reject an empty selection

- **WHEN** the user activates Test with no model selected
- **THEN** no provider call is made and the user sees an error message telling them to select a model

#### Scenario: Reject an unprefixed model

- **WHEN** the selected value is not in `provider/model` form
- **THEN** no provider call is made and the user sees an error message describing the expected form

#### Scenario: Reject an unparseable selection

- **WHEN** the selected value cannot be resolved to a provider instance and a model
- **THEN** no provider call is made and the user sees an error message describing the expected selection

#### Scenario: Surface a provider failure

- **WHEN** the provider call fails (bad credentials, unknown model, upstream error)
- **THEN** the user sees an error message carrying the provider's failure detail

#### Scenario: Test does not persist

- **WHEN** a model test succeeds or fails
- **THEN** the stored default generative and embedding models are unchanged

#### Scenario: Prevent duplicate in-flight tests

- **WHEN** a test request is in flight
- **THEN** the Test action is disabled and shows a busy indicator until the call returns
