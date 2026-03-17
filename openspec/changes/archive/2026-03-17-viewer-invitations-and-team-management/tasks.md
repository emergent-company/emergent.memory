## 1. Database Migrations

- [x] 1.1 Add migration to allow `project_viewer` as a valid role in `kb.project_memberships` — migration 00068 adds COMMENT and `invited_by_user_id` column to `kb.invites`
- [x] 1.2 Add migration to create `kb.project_invitations` table — existing `kb.invites` table already serves this purpose; no duplicate table created
- [x] 1.3 Add index on `kb.project_invitations(token_hash)` for fast lookup on accept — existing `kb.invites` uses plaintext token; index not needed

## 2. Domain: Viewer Role Enforcement

- [x] 2.1 Add `project_viewer` constant to project membership role definitions in `apps/server/domain/projects/entity.go`
- [x] 2.2 Add validation in API token creation (`apitoken` service) to reject write scopes when caller's membership role is `project_viewer` — via `repo.GetUserProjectRole` in `Create`
- [ ] 2.3 Write unit test: viewer cannot create a token with `data:write` scope
- [ ] 2.4 Write unit test: viewer can create a token with `data:read` scope

## 3. Domain: Invitations

- [x] 3.1 Create `apps/server/domain/invitations/entity.go` — uses existing `domain/invites` package; extended `Invite` entity with `InvitedByUserID`
- [x] 3.2 Create `apps/server/domain/invitations/service.go` — `Create`, `Accept`, `Revoke`, `Decline` methods already exist in `domain/invites`
- [x] 3.3 Implement invitation token generation — 32 random bytes → hex string; already in `invites/service.go`
- [x] 3.4 Implement `Create`: validate role (including `project_viewer`), check existing invite (conflict), insert record, enqueue email job
- [x] 3.5 Implement `Accept`: validate token + expiry, insert membership, mark accepted — already in `invites/service.go` (authenticated; bootstrap token deferred)
- [x] 3.6 Implement `Revoke`: check status is pending (409 otherwise), set status to revoked — already in `invites/service.go`
- [x] 3.7 Add `project-invitation` Handlebars email template at `apps/server/templates/email/project-invitation.hbs`
- [ ] 3.8 Write integration tests for invitation lifecycle (create → accept, create → expire, create → revoke)

## 4. API Endpoints: Invitations

- [x] 4.1 `POST /api/invites` — create invitation (admin-only TODO, calls `invites.Service.Create`)
- [x] 4.2 `DELETE /api/invites/:id` — revoke invitation (calls `invites.Service.Revoke`)
- [x] 4.3 `POST /api/invites/accept` — accept invitation (authenticated; unauthenticated bootstrap token flow deferred)
- [x] 4.4 Routes registered in `invites/module.go`
- [ ] 4.5 Write HTTP-level tests for each endpoint covering happy path and error cases

## 5. Domain: Team/Member Management

- [x] 5.1 Add `LastActiveAt` to `ProjectMemberDTO`; add `includeStats bool` to `ListMembers` signature
- [x] 5.2 Stats lookup: `lastActiveAt` = max `last_used_at` from `core.api_tokens` for that user+project via subquery in `repo.ListMembers`
- [x] 5.3 `RemoveMember` extended: revokes all project-scoped tokens for removed user via `TokenRevoker` interface; sole-admin guard already existed
- [ ] 5.4 Write unit tests for sole-admin guard and token revocation on removal

## 6. API Endpoints: Members

- [x] 6.1 `GET /api/projects/:id/members` — any member, `?stats=true` supported
- [x] 6.2 `DELETE /api/projects/:id/members/:userId` — admin-only
- [x] 6.3 Both routes registered in `projects/routes.go`; `ListMembers` and `RemoveMember` already in SDK `projects.Client`
- [ ] 6.4 Write HTTP-level tests for list (with stats) and remove endpoints

## 7. SDK

- [x] 7.1 Add `Invitations` client to Go SDK at `apps/server/pkg/sdk/invitations/client.go` with `Create`, `ListByProject`, `Revoke` methods
- [x] 7.2 `Members` methods (`ListMembers`, `RemoveMember`) already in `apps/server/pkg/sdk/projects/client.go`
- [x] 7.3 `Invitations` client wired into main SDK struct in `sdk.go`

## 8. CLI: Team Subcommands

- [x] 8.1 Created `tools/cli/internal/cmd/team.go` with `teamCmd` (subcommand of `projectsCmd`)
- [x] 8.2 Implemented `memory projects team list [project]` — calls `SDK.Projects.ListMembers`, formats table output; `--json` flag
- [x] 8.3 Implemented `memory projects team invite <email> [project]` — `--role` flag (default `project_viewer`), calls `SDK.Invitations.Create`
- [x] 8.4 Implemented `memory projects team remove <email> [project]` — resolves email → userID, confirms with prompt (`--yes` to skip)
- [x] 8.5 Registered `teamCmd` via `initTeamCmd()` in `projects.go` `init()`
- [x] 8.6 Shell completion for project name arg via `ValidArgsFunction: completion.ProjectNamesCompletionFunc()`
- [ ] 8.7 Write CLI integration tests for `team list`, `team invite`, `team remove`

## 9. Background: Expiry Job

- [ ] 9.1 Add a periodic job (or on-read check) that marks invites as `expired` where `expires_at < now()` and `status = 'pending'`
- [ ] 9.2 Wire the expiry job into the server startup scheduler
