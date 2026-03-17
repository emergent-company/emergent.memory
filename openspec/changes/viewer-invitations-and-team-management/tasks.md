## 1. Database Migrations

- [ ] 1.1 Add migration to allow `project_viewer` as a valid role in `kb.project_memberships` (add CHECK constraint or document valid values)
- [ ] 1.2 Add migration to create `kb.project_invitations` table with columns: `id`, `project_id`, `invited_email`, `invited_role`, `token_hash`, `invited_by_user_id`, `status`, `expires_at`, `created_at`, `accepted_at`
- [ ] 1.3 Add index on `kb.project_invitations(token_hash)` for fast lookup on accept

## 2. Domain: Viewer Role Enforcement

- [ ] 2.1 Add `project_viewer` constant to project membership role definitions in `apps/server/domain/projects/entity.go`
- [ ] 2.2 Add validation in API token creation (`apitoken` service/handler) to reject write scopes when caller's membership role is `project_viewer`
- [ ] 2.3 Write unit test: viewer cannot create a token with `data:write` scope
- [ ] 2.4 Write unit test: viewer can create a token with `data:read` scope

## 3. Domain: Invitations

- [ ] 3.1 Create `apps/server/domain/invitations/entity.go` with `ProjectInvitation` model and status constants
- [ ] 3.2 Create `apps/server/domain/invitations/service.go` with `Create`, `Accept`, `Revoke`, `ExpireStale` methods
- [ ] 3.3 Implement invitation token generation (32 random bytes → hex string; store SHA-256 hash)
- [ ] 3.4 Implement `Create`: validate role, check existing membership (409), insert record, enqueue email job
- [ ] 3.5 Implement `Accept`: validate token hash + expiry, provision user if needed, insert membership, mark accepted, return bootstrap token
- [ ] 3.6 Implement `Revoke`: check status is pending (409 otherwise), set status to revoked
- [ ] 3.7 Add `project-invitation` Handlebars email template (`apps/server/templates/email/project-invitation.hbs`) with inviter name, project name, role, CLI install steps, and accept URL
- [ ] 3.8 Write integration tests for invitation lifecycle (create → accept, create → expire, create → revoke)

## 4. API Endpoints: Invitations

- [ ] 4.1 Add `POST /v1/projects/:projectId/invitations` handler — admin-only, calls invitation service `Create`
- [ ] 4.2 Add `DELETE /v1/projects/:projectId/invitations/:invitationId` handler — admin-only, calls `Revoke`
- [ ] 4.3 Add `GET /v1/invitations/:token/accept` handler — unauthenticated, calls `Accept`, returns bootstrap token
- [ ] 4.4 Register all three routes in the router
- [ ] 4.5 Write HTTP-level tests for each endpoint covering happy path and error cases

## 5. Domain: Team/Member Management

- [ ] 5.1 Add `GetMembers(projectID, includeStats bool)` to projects service returning `[]ProjectMemberDTO` (extend existing DTO with `LastActiveAt`, `RequestCount30d`)
- [ ] 5.2 Implement stats lookup: `lastActiveAt` = max `last_used_at` from `core.api_tokens` for that user+project; `requestCount30d` = null if per-request counts unavailable
- [ ] 5.3 Add `RemoveMember(projectID, targetUserID, callerUserID)` to projects service — checks caller is admin, checks sole-admin guard (409), deletes membership, revokes project tokens for target user
- [ ] 5.4 Write unit tests for sole-admin guard and token revocation on removal

## 6. API Endpoints: Members

- [ ] 6.1 Add `GET /v1/projects/:projectId/members` handler — any member, optional `?stats=true`
- [ ] 6.2 Add `DELETE /v1/projects/:projectId/members/:userId` handler — admin-only
- [ ] 6.3 Register both routes and add to SDK
- [ ] 6.4 Write HTTP-level tests for list and remove endpoints

## 7. SDK

- [ ] 7.1 Add `Invitations` client to Go SDK (`apps/server/pkg/sdk/invitations/`) with `Create`, `Revoke` methods
- [ ] 7.2 Add `Members` client to Go SDK (`apps/server/pkg/sdk/members/`) with `List`, `Remove` methods
- [ ] 7.3 Wire both clients into the main SDK struct

## 8. CLI: Team Subcommands

- [ ] 8.1 Create `tools/cli/internal/cmd/team.go` with `teamCmd` (subcommand of `projectsCmd`)
- [ ] 8.2 Implement `memory projects team list [project]` — calls members SDK, formats table output with name/email/role/joined; `--stats` adds last-active column; `--json` outputs raw JSON
- [ ] 8.3 Implement `memory projects team invite <email> [project]` — `--role` flag (default `project_viewer`), calls invitations SDK, prints confirmation
- [ ] 8.4 Implement `memory projects team remove <email> [project]` — resolves email to user ID via members list, confirms with prompt (skippable with `--yes`), calls remove SDK, prints confirmation
- [ ] 8.5 Register `teamCmd` and its subcommands in `root.go` or `projects.go`
- [ ] 8.6 Add shell completion for `team list`, `team invite`, `team remove` (project name arg)
- [ ] 8.7 Write CLI integration tests for `team list`, `team invite`, `team remove`

## 9. Background: Expiry Job

- [ ] 9.1 Add a periodic job (or on-read check) that marks `project_invitations` as `expired` where `expires_at < now()` and `status = 'pending'`
- [ ] 9.2 Wire the expiry job into the server startup scheduler
