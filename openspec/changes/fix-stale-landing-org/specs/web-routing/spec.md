## Purpose

Defines how the root path resolves a default organization context to a scoped
URL, including the handling of a remembered org that no longer exists.

## MODIFIED Requirements

### Requirement: Scoped landing redirect

The root path SHALL resolve a default context to a scoped URL: the last-used
project, else the caller's organization, else the onboarding destination. A
remembered organization SHALL be validated against the caller's live org list
before it is used as the landing default.

#### Scenario: Last-used project

- **WHEN** a signed-in caller with a remembered project requests `/`
- **THEN** the gateway redirects to that project's scoped URL

#### Scenario: No project

- **WHEN** a signed-in caller with an org but no project requests `/`
- **THEN** the gateway redirects to that org's scoped landing

#### Scenario: Stale remembered org

- **WHEN** a signed-in caller with a remembered org that no longer exists in the caller's live org list requests `/`
- **THEN** the gateway clears the stale org from the session and redirects to the first live org's scoped landing, or the onboarding destination when no orgs remain
