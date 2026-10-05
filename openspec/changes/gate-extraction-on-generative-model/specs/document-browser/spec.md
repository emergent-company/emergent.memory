## Purpose

Gates the document extraction trigger on the project's resolvable generative model, so extraction is offered only when it can actually run and the server refuses the trigger otherwise.

## MODIFIED Requirements

### Requirement: Trigger extraction

The document view SHALL let the user trigger extraction for a document and show the resulting status, but only when the project has a resolvable generative model. When no generative model is configured, the extraction control SHALL be disabled and the view SHALL show guidance linking to the providers settings, and the server SHALL refuse the trigger.

#### Scenario: Trigger extraction

- **WHEN** the project has a resolvable generative model and the user triggers extraction for a document
- **THEN** extraction starts and the view reflects the updated status

#### Scenario: No generative model configured

- **WHEN** the project has no resolvable generative model
- **THEN** the extraction control is disabled and the view shows a warning with a link to the providers settings

#### Scenario: Trigger refused without a generative model

- **WHEN** the user submits the extraction trigger while the project has no resolvable generative model
- **THEN** the server refuses the request and no extraction job is enqueued

#### Scenario: Config fetch failure fails open

- **WHEN** the effective model config cannot be fetched
- **THEN** extraction is still offered (the failure is captured but does not disable extraction)
