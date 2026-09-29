## MODIFIED Requirements

### Requirement: Invitation email delivery

The system SHALL send a `project-invitation` email to the invited address. The email SHALL include the inviting user's name, the project name, the role being granted, and a single-use accept URL valid for 7 days.

A missing template, a failed template render, or a `project-invitation` job whose template data lacks a usable accept URL SHALL fail the email job (`failed` or `dead_letter` with `last_error` recorded) and SHALL NOT be sent or marked `sent`. The accept URL SHALL be an absolute `http(s)` URL; a relative or empty accept URL SHALL fail the job. The system SHALL NOT deliver an invitation email without an accept link.

#### Scenario: Invitation email delivered

- **WHEN** an invitation is created
- **THEN** an email job with template `project-invitation` is enqueued and the email contains the accept URL and role label

#### Scenario: Missing template fails instead of sending a link-less invite

- **WHEN** a `project-invitation` job is processed and the template cannot be found
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`

#### Scenario: Render failure fails instead of sending a link-less invite

- **WHEN** a `project-invitation` job is processed and the template render returns an error
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`

#### Scenario: Missing or relative accept URL fails

- **WHEN** a `project-invitation` job is processed and its template data has no accept URL, or an accept URL that is not an absolute `http(s)` URL
- **THEN** the job is marked `failed`/`dead_letter` with `last_error` set, no email is sent, and the job is not marked `sent`
