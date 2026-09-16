## Purpose

Provides web UI to create, list, edit the scopes of, regenerate, and revoke scoped Emergent Memory API tokens — at both the project level and the account level — showing each token's scopes and the plaintext value at creation and on regenerate.

## ADDED Requirements

### Requirement: Navigate to project API tokens

The gateway sidebar SHALL include an API Tokens entry in the Settings group that opens the project API tokens page.

#### Scenario: Sidebar entry present

- **WHEN** the sidebar loads
- **THEN** an API Tokens link is shown in the Settings group and opens the project API tokens page

### Requirement: Navigate to account API tokens

The profile area SHALL expose account-level API tokens on a dedicated `/profile/tokens` page reached from the profile sub-navigation.

#### Scenario: Profile sub-navigation entry

- **WHEN** the signed-in user opens any profile page
- **THEN** the profile sub-navigation shows an API Tokens entry that opens the account API tokens page

### Requirement: List API tokens

The API tokens UI SHALL list the relevant scope's tokens — the active project's tokens for the project page, the signed-in user's tokens for the account page — in a table with their metadata (name, token prefix, scopes, creation time, last-used time, revoked state) and MUST NOT display any plaintext token value.

#### Scenario: Project tokens listed

- **WHEN** the project API tokens page loads and tokens exist for the active project
- **THEN** each token is listed with its name, token prefix, scopes, creation time, last-used time, and revoked state

#### Scenario: Account tokens listed

- **WHEN** the account API tokens page loads and account tokens exist for the user
- **THEN** each account token is listed with the same metadata

#### Scenario: No tokens

- **WHEN** the list loads and no tokens exist for the given scope
- **THEN** a clear "no tokens" state is shown

### Requirement: Create an API token

The API tokens UI SHALL allow the user to create a scoped token on a dedicated create page by providing a name and at least one scope, at both the project and account level.

#### Scenario: Project token created

- **WHEN** the user submits a name and at least one valid scope on the project create page
- **THEN** a new project-scoped token is created and its plaintext value is shown

#### Scenario: Account token created

- **WHEN** the user submits a name and at least one valid scope on the account create page
- **THEN** a new account-level token is created and its plaintext value is shown

#### Scenario: Missing name or scopes

- **WHEN** the user submits a create form with an empty name or no scopes
- **THEN** the page shows an error and does not create a token

### Requirement: Show token scopes

The API tokens UI SHALL display the scopes granted to each token, both in the list and on the create form as selectable options, at both levels.

#### Scenario: Scopes displayed

- **WHEN** a token is listed or the create form is shown
- **THEN** the token's scopes are shown, and the create form offers the valid scope choices

### Requirement: Show a token only at creation and regenerate

The API tokens UI SHALL show the full plaintext token exactly once at creation and on regenerate, with a copy affordance, and MUST NOT show it again in any list or detail view.

#### Scenario: Token shown once

- **WHEN** a token is created or regenerated
- **THEN** the full `emt_*` token value is displayed with a copy control and is not shown on any later list or detail view

### Requirement: Regenerate a token

The API tokens UI SHALL allow the user to regenerate a token, atomically revoking it and issuing a replacement with the same name and scopes, showing the new plaintext once.

#### Scenario: Token regenerated

- **WHEN** the user confirms a regenerate action
- **THEN** the old token is revoked, a replacement is created with the same name and scopes, and the new plaintext is shown once

#### Scenario: Regenerate rejected

- **WHEN** the Memory service rejects a regenerate request (e.g. already revoked)
- **THEN** the page shows an error and no replacement is created

### Requirement: Edit token scopes

The API tokens UI SHALL allow the user to update the scopes of a non-revoked token on a dedicated edit page at both levels.

#### Scenario: Scopes updated

- **WHEN** the user submits new scopes for a non-revoked token
- **THEN** the token's scopes are updated and reflected in the list

#### Scenario: Update rejected

- **WHEN** the Memory service rejects a scope update
- **THEN** the page shows an error and the token's scopes remain unchanged

### Requirement: Revoke a token

The API tokens UI SHALL allow the user to revoke a token, invalidating it immediately, at both levels.

#### Scenario: Token revoked

- **WHEN** the user revokes a token and confirms
- **THEN** the token is invalidated immediately and is shown as revoked in the list

#### Scenario: Revoke rejected

- **WHEN** the Memory service rejects a revoke request
- **THEN** the page shows an error and the token remains unchanged

### Requirement: Enforce valid scopes

The API tokens UI SHALL only submit scopes from the supported reference set and MUST require at least one scope on create and on scope edit.

#### Scenario: Unsupported scope rejected

- **WHEN** a create or edit request would carry a scope outside the supported reference set
- **THEN** the token is not created or updated and an error is surfaced

### Requirement: Surface server-side errors

The API tokens UI SHALL surface Memory's error responses as readable messages — including duplicate-name (409) and permission (403) rejections.

#### Scenario: Duplicate name

- **WHEN** the Memory service rejects a create with a duplicate-name error
- **THEN** the page shows the duplicate-name message and does not create a token

#### Scenario: Permission denied

- **WHEN** the Memory service rejects a create, edit, or regenerate with a permission error (viewer write scope, or `admin:all` grant)
- **THEN** the page shows the permission message and does not change the token

### Requirement: Authenticated, scope-appropriate access

The API tokens UI SHALL require an authenticated session; the project page SHALL operate on the session's active project only, and the account tokens page SHALL operate on the signed-in user's account tokens only, leaving the existing shared `X-API-Key` programmatic path unchanged.

#### Scenario: Unauthenticated request

- **WHEN** an unauthenticated caller requests any API tokens page or action
- **THEN** the request is redirected to sign-in and no token data is exposed

#### Scenario: Existing API-key path unaffected

- **WHEN** an `X-API-Key` client continues to call Memory directly
- **THEN** the existing programmatic access path keeps working as before

### Requirement: Handle an unreachable Memory service

The API tokens UI SHALL show an error state when the Memory service cannot be reached, without crashing the rest of the UI.

#### Scenario: Memory unreachable

- **WHEN** the API tokens UI loads and the Memory service is unreachable
- **THEN** an error message is shown and partial data is not presented as authoritative
