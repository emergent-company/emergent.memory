# provider-settings Specification

## Purpose

The Project Settings providers surface: which LLM providers a project can use, which models and default models it selects, the rates applied to them, and the ability to verify a selected model works before relying on it.

## Requirements

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

### Requirement: Provider and model test outcomes are presented in an accessible alert dialog

When a provider test, connection test, or default-model test completes — success or failure — the client SHALL present the outcome in the app-wide alert dialog rather than a transient toast. The outcome SHALL be delivered by the `memory-alert` HX-Trigger event carrying a `kind` (`success` or `error`), a `title` naming the action, and a `message` with the result detail. The dialog SHALL be a native modal dialog with an accessible name derived from its (dynamic) title, and SHALL be dismissible via its Close control, the backdrop, and the Escape key. The dialog title and message SHALL be rendered as text, never parsed as markup, because provider replies and upstream errors are untrusted.

#### Scenario: Successful test opens the success dialog

- **WHEN** a provider or model test succeeds
- **THEN** the alert dialog opens with the success tone and shows the titled success message

#### Scenario: Failed test opens the error dialog

- **WHEN** a provider or model test fails validation or the provider call errors
- **THEN** the alert dialog opens with the error tone and shows the provider's failure detail

#### Scenario: Long multi-line provider output stays readable

- **WHEN** the result message spans multiple lines or is long
- **THEN** the dialog preserves the whitespace and wraps the text so it can be read before dismissal

#### Scenario: Untrusted content is never parsed as markup

- **WHEN** the result message or title contains HTML markup
- **THEN** the dialog shows it as literal text and injects no elements and no script

#### Scenario: Dismissal restores focus

- **WHEN** the user closes the dialog via the Close control, the backdrop, or Escape
- **THEN** the dialog closes and focus returns to the element that was focused before it opened

#### Scenario: A successful connection test enables Save

- **WHEN** the connection test that follows this form's own request succeeds
- **THEN** the Save action is enabled, exactly as the previous toast-driven gating did

### Requirement: Provider instance slugs are unique per project

A project's provider instance slug SHALL be unique within that project, enforced by the `(project_id, slug)` database constraint. A slug MAY equal a registered provider dialect name; the system SHALL NOT reject a slug merely because it names a dialect (including another dialect's name). Model reference resolution SHALL be slug-first: a reference whose provider segment matches a project instance slug resolves to that instance, and only when no instance slug matches does the system fall back to a dialect's default instance.

#### Scenario: Dialect-name slug for another dialect succeeds

- **WHEN** a user creates or saves a provider instance whose slug equals another vendor's dialect name (for example an OpenAI-dialect instance named `deepseek`)
- **THEN** the slug is accepted, provided it is non-empty, within the length limit, matches `[a-z0-9][a-z0-9-]*`, and is not already used by another instance in the same project

#### Scenario: Reference matching an instance slug resolves to that instance

- **WHEN** a model reference's provider segment matches a project's instance slug
- **THEN** resolution returns that instance, even when the segment is also a registered dialect name

#### Scenario: Reference naming only a dialect resolves to its default instance

- **WHEN** a model reference's provider segment is a dialect name and no project instance has that slug
- **THEN** resolution falls back to that dialect's default instance

#### Scenario: Duplicate slug within a project is rejected

- **WHEN** a user saves a provider instance whose slug is already used by another instance in the same project
- **THEN** the `(project_id, slug)` constraint rejects the save

### Requirement: Connection-test egress honors an operator allowlist

The gateway's provider connection test (`POST /settings/providers/test`) and the inline
base-url check (`POST /settings/providers/check-url`) SHALL dial only publicly-routable
addresses by default, and SHALL additionally permit any hostname or CIDR listed in the
`PROVIDER_PROBE_ALLOW` environment variable (comma-separated). This lets a self-hosted
deployment test an internal gateway (for example `litellm`) without disabling the SSRF
guard for every destination. Entries that are neither a valid hostname nor a valid CIDR
SHALL be ignored; an unset or empty variable SHALL leave the public-only policy unchanged.

#### Scenario: Allowlisted internal host is testable

- **WHEN** `PROVIDER_PROBE_ALLOW` lists `litellm`
- **AND** the user tests a base_url whose host resolves to a non-public address
- **THEN** the connection test dials it and reports the endpoint's response instead of refusing the non-public address

#### Scenario: Allowlisted CIDR is testable

- **WHEN** `PROVIDER_PROBE_ALLOW` lists `100.64.0.0/10`
- **AND** the target host resolves to `100.113.48.6`
- **THEN** the connection test dials the address

#### Scenario: Unlisted non-public host stays blocked

- **WHEN** `PROVIDER_PROBE_ALLOW` does not list the target host or an address range containing it
- **THEN** the connection test refuses to dial the non-public address

#### Scenario: Cloud-metadata address stays blocked unless explicitly listed

- **WHEN** `PROVIDER_PROBE_ALLOW` lists an unrelated host
- **AND** the target resolves to `169.254.169.254`
- **THEN** the connection test refuses to dial it

#### Scenario: Default policy is unchanged

- **WHEN** `PROVIDER_PROBE_ALLOW` is unset or empty
- **THEN** only publicly-routable addresses are dialable, exactly as before

#### Scenario: Invalid allowlist entries are ignored

- **WHEN** `PROVIDER_PROBE_ALLOW` contains an entry that is neither a valid hostname nor a valid CIDR
- **THEN** that entry is ignored and the remaining valid entries still apply
