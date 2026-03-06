## 1. Database Schema

- [ ] 1.1 Create migration to make `project_id` nullable in `core.api_tokens` and add a nullable `organization_id` column.
- [ ] 1.2 Update RLS policies on `core.api_tokens` to allow users to read/manage their org-scoped and account-scoped tokens.
- [ ] 1.3 Update the `ApiToken` entity model in `apps/server-go/domain/apitoken/entity.go` to support `OrganizationID` and pointers for nullable fields.

## 2. Token Service and Repository

- [ ] 2.1 Update token repository methods to query by `organization_id` or `user_id` (with null `project_id` and `organization_id`).
- [ ] 2.2 Update token service to handle creation and revocation of org and account tokens.

## 3. API Endpoints

- [ ] 3.1 Implement HTTP handlers for Organization Token endpoints (`/api/orgs/:orgId/tokens` Create, List, Revoke).
- [ ] 3.2 Implement HTTP handlers for Account Token endpoints (`/api/user/tokens` Create, List, Revoke).
- [ ] 3.3 Register new routes in the API router.

## 4. Authentication Middleware

- [ ] 4.1 Update token validation middleware to correctly resolve the token type (Project, Organization, Account).
- [ ] 4.2 Ensure the middleware correctly sets the request context and bypassing attributes for broader token scopes.

## 5. Testing & Validation

- [ ] 5.1 Write integration tests for creating and validating organization API tokens.
- [ ] 5.2 Write integration tests for creating and validating account API tokens.
