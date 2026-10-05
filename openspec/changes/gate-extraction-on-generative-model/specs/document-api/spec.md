## Purpose

Gates the document extraction JSON API on the project's resolvable generative model, refusing the trigger when none is configured.

## MODIFIED Requirements

### Requirement: Trigger extraction

The API SHALL let a client trigger an extraction job for a document and return a reference to the resulting job, and SHALL refuse the trigger when the project has no resolvable generative model.

#### Scenario: Extraction triggered

- **WHEN** a client triggers extraction for a known document and the project has a resolvable generative model
- **THEN** the API returns a reference to the created extraction job

#### Scenario: No generative model configured

- **WHEN** a client triggers extraction for a document whose project has no resolvable generative model
- **THEN** the API refuses the request and enqueues no extraction job

#### Scenario: Unknown document

- **WHEN** a client triggers extraction for a document id that does not exist
- **THEN** the API returns a not-found error response
