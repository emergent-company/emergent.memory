## Purpose

Defines how organization and project context are addressed in the web console's
URLs, resolved on the server, and propagated to client-side requests, so that
every tab is independently scoped and every screen is linkable.

## ADDED Requirements

### Requirement: Scoped URL paths

The gateway SHALL serve every organization-scoped page under `/o/{orgRef}` and
every project-scoped page under `/o/{orgRef}/p/{projectRef}`, and SHALL keep
account, authentication, API, share, static-asset, and webhook routes at their
unscoped top-level paths.

#### Scenario: Project page is scoped

- **WHEN** a browser requests a project page
- **THEN** the URL contains `/o/{orgRef}/p/{projectRef}/` followed by the page path

#### Scenario: Org page is scoped

- **WHEN** a browser requests an organization page
- **THEN** the URL contains `/o/{orgRef}/` followed by the page path

#### Scenario: Global routes are unscoped

- **WHEN** a browser requests `/profile`, `/orgs`, `/auth/*`, `/api/*`, `/share/*`,
  `/static/*`, `/assets/*`, or `/webhooks/*`
- **THEN** the request is served without an org/project path segment

### Requirement: Reference resolution

The `{orgRef}` and `{projectRef}` path segments SHALL accept either an opaque
identifier or a slug, and the gateway SHALL resolve them to the concrete
organization and project before serving the page.

#### Scenario: Identifier ref resolves

- **WHEN** a scoped request carries a valid UUID ref for an org/project the caller can access
- **THEN** the gateway resolves the org/project and serves the page for that context

#### Scenario: Slug ref is accepted for forward compatibility

- **WHEN** a scoped request carries a ref that is not a UUID and no matching slug exists
- **THEN** the gateway treats the request as not found

### Requirement: Fail-closed scope authorization

The gateway SHALL resolve scope against the caller's organization→project access
tree and MUST NOT serve a scoped page — nor send `X-Project-ID`/`X-Org-ID` for
it — when the ref is unknown, the project does not belong to the org, or the
caller lacks access.

#### Scenario: Unknown ref

- **WHEN** a scoped request carries an org or project ref that does not resolve within the caller's access
- **THEN** the gateway responds with 404 and makes no Memory call for that scope

#### Scenario: Project not in the stated org

- **WHEN** the project ref resolves but belongs to a different org than `{orgRef}`
- **THEN** the gateway responds with 404

### Requirement: Path context is authoritative per request

For a scoped request, the resolved org and project SHALL be the request's Memory
scope; the session cookie SHALL NOT override the path.

#### Scenario: Two tabs, two projects

- **WHEN** one tab requests a page under one project's scoped path and another tab requests a page under a different project's scoped path in the same session
- **THEN** each response renders with its own tab's project context

#### Scenario: Cookie does not override the path

- **WHEN** the session cookie carries a different active project than the scoped path
- **THEN** the scoped path's project is used for the request

### Requirement: Scoped landing redirect

The root path SHALL resolve a default context to a scoped URL: the last-used
project, else the caller's organization, else the onboarding destination.

#### Scenario: Last-used project

- **WHEN** a signed-in caller with a remembered project requests `/`
- **THEN** the gateway redirects to that project's scoped URL

#### Scenario: No project

- **WHEN** a signed-in caller with an org but no project requests `/`
- **THEN** the gateway redirects to that org's scoped landing

### Requirement: Internal links and actions carry scope

All internal app links and form actions rendered for a scoped page SHALL include
the page's scope base so navigation and mutations stay in the current tab's
context.

#### Scenario: Sidebar navigation keeps scope

- **WHEN** a project page renders its sidebar
- **THEN** every sidebar link is prefixed with `/o/{orgRef}/p/{projectRef}`

#### Scenario: Form action keeps scope

- **WHEN** a scoped page renders a mutation form
- **THEN** the form's action targets the same scoped base

#### Scenario: Sub-resource URLs carry scope

- **WHEN** a scoped page renders a sub-resource URL (an embed or image `src`
  pointing at the gateway's document file proxy)
- **THEN** the `src` is prefixed with `/o/{orgRef}/p/{projectRef}` so the
  browser fetches it within the page's scope

### Requirement: Client-side scope propagation

Client-side requests made by a scoped page (htmx, `fetch`, and event streams)
SHALL carry the page's scope, and the gateway SHALL accept a client-supplied
scope only after validating it against the caller's access tree.

#### Scenario: htmx request carries scope

- **WHEN** a scoped page issues an htmx request to an unscoped `/api/*` endpoint
- **THEN** the request carries the page's `X-Org-ID`/`X-Project-ID`, or the endpoint's URL carries the scope

#### Scenario: Unauthorized client scope is rejected

- **WHEN** a client supplies an `X-Project-ID`/`X-Org-ID` outside the caller's access tree
- **THEN** the gateway does not scope the request to it
