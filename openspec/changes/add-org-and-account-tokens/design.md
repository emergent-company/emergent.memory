## Context

Currently, the `ApiToken` entity in `core.api_tokens` requires a `project_id`. This means users must create a separate token for every project they wish to access programmatically. As the system scales, users need broader access tokens:
- Organization API Tokens: Access all projects within a specific organization.
- Account API Tokens: Access all organizations and projects associated with a user's account.

## Goals / Non-Goals

**Goals:**
- Extend the database schema for `core.api_tokens` to support organization and account-level scoping.
- Implement token resolution logic in the authentication middleware to determine the scope of a token and validate it appropriately against the requested resource.
- Provide API endpoints for managing org-level and account-level tokens.

**Non-Goals:**
- Changing the cryptographic approach (hashing/encryption) used for existing tokens.
- Introducing entirely new authorization models beyond simple scope/target expansion.

## Decisions

**Database Schema Changes:**
- **Decision:** Make `project_id` nullable. Add a nullable `organization_id` column to `core.api_tokens`.
- **Rationale:** This allows the table to store three types of tokens:
  1. Project Token: `project_id` is set, `organization_id` is null (or matches project's org).
  2. Org Token: `project_id` is null, `organization_id` is set.
  3. Account Token: Both `project_id` and `organization_id` are null. The token is inherently scoped to the `user_id`.

**Token Resolution & RLS:**
- **Decision:** The authentication middleware will read the token, identify its type based on the populated ID fields, and inject appropriate context (e.g., bypassing project-specific RLS if it's an org token but the request targets a project within that org).
- **Rationale:** Centralizing token resolution ensures consistent authorization across all endpoints without modifying individual handlers.

## Risks / Trade-offs

- **Risk: Scope Escalation** -> If validation logic is flawed, an account token might grant access to resources the user themselves doesn't have access to. Mitigation: Ensure token validation still defers to the user's actual RBAC permissions; the token merely *acts as* the user for the granted scopes across the specified target (project/org/all).
- **Risk: RLS Policy Complexity** -> Existing Row-Level Security policies likely depend on `project_id`. Mitigation: We will need to update database RLS policies on `core.api_tokens` to allow users to read/manage their org/account tokens.
