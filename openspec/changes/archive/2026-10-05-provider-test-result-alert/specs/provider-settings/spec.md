## ADDED Requirements

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
