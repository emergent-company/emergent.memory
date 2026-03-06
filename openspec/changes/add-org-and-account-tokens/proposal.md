## Why

Currently, API tokens are scoped exclusively to individual projects. Users need the ability to create API tokens scoped to an entire organization, as well as account-level tokens that grant access across all of a user's organizations. This enables broader programmatic access and integrations without the overhead of managing numerous project-specific tokens.

## What Changes

- Add support for generating Organization API Tokens scoped to a specific organization.
- Add support for generating Account API Tokens scoped to the user account (accessing all user orgs).
- Introduce new endpoints for managing Organization API Tokens (`/api/orgs/:orgId/tokens`).
- Introduce new endpoints for managing Account API Tokens (`/api/user/tokens`).
- Update the token validation middleware to correctly authenticate and enforce scopes for organization and account-level tokens.

## Capabilities

### New Capabilities

- `org-api-tokens`: Management and validation of API tokens scoped to an organization.
- `account-api-tokens`: Management and validation of API tokens scoped to a user account.

### Modified Capabilities

- `mcp-settings-guide`: Expand UI to support generating and managing organization and account tokens alongside project tokens.

## Impact

- **Database**: `api_tokens` table needs to support `organization_id` and `user_id` as targets, distinguishing between project, org, and account scopes.
- **Backend API**: New REST endpoints for token management at the org and user levels.
- **Authentication**: `AuthService` token validation must handle different scopes and set appropriate request context.
- **Frontend**: Settings pages must include sections for managing organization and account tokens.