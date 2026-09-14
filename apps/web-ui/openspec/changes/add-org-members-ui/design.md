## Context

See `proposal.md` — Why. Emergent Memory (`/root/emergent.memory`, OpenAPI) already exposes the full org/member/invite surface: `GET/POST /orgs`, `GET/DELETE /orgs/{id}`, `GET /projects/{id}/members`, `DELETE /projects/{id}/members/{userId}`, `GET /projects/{id}/invites`, `POST /invites`, `POST /invites/with-user`, `POST /invites/accept`, `GET /invites/pending`, `POST /invites/{id}/decline`, `DELETE /invites/{id}`, `GET /users/search`, `GET /user/orgs-and-projects`, and `GET/PUT /user/profile`. This change wires Alfred's web console to that surface on top of the session + project frame from `add-auth-project-frame` (signed cookie, per-request user token + active `X-Project-ID`). It does not touch the Memory service, iOS, or the supervisor/bridge workers.

## Goals / Non-Goals

**Goals:**
- Add org list + creation in the web console.
- Add project member listing, removal, and email/role-based invitation (existing-user and new-user paths).
- Add sent-invite management (list/revoke) and the invite accept/decline flow for the signed-in user.
- Add a user profile view/update.
- Surface org context when creating projects (the create-project form from `add-auth-project-frame` gains an org selector fed by the org list).

**Non-Goals:**
- No organization deletion in the UI (cascading destroy is out of scope for this slice).
- No member role-change endpoint — Memory offers none; role changes are remove + re-invite.
- No superadmin user/org administration, billing, or data-source management.
- No changes to how the supervisor/bridge workers authenticate (they remain single-tenant with the shared server token).

## Decisions

### D1 — Org view is the user's own access tree

The console reads organizations via `GET /orgs` (a bare `OrgDto` array: `{id, name}`) and the nested org→project tree with per-level roles via `GET /user/orgs-and-projects` (`OrgWithProjectsDto`). There is no public org-member-list endpoint, so the "organization" screen is the signed-in user's access tree, not a membership roster.

- **Why:** matches exactly what Memory exposes to an interactive user; avoids the superadmin trust boundary.
- **Alternative rejected:** building an org-level member roster from `SuperadminOrgDto`/`ListUsersResponseDto` — those are superadmin endpoints, the wrong trust level for a normal session.

### D2 — Role is chosen at invite time; no role-change endpoint

Invitations carry one role from Memory's invite enum (`org_admin`, `project_admin`, `project_user`); membership resolves to `org_admin`/`org_member` at the org level and `project_admin`/`project_user` at the project level. Because Memory has no member-role PATCH, a role change IS remove + re-invite: the member details page offers a "change role" affordance that removes the member and re-invites their email with the chosen role, gated behind an explicit confirm. The console SHALL NOT fake a mutation the server cannot persist.

- **Why:** the server cannot persist a role change, so the UI spells out the remove-and-re-invite consequence instead of silently no-oping.
- **Alternative rejected:** a client-side role selector that fakes a mutation it cannot perform.

### D3 — Email-based invite with search autofill

The invite flow offers `GET /users/search?email=` (partial match, min 2 chars, up to 10 results) to autofill the email of an already-registered user. Every invitation goes through `POST /invites` (`CreateInviteDto`: `orgId`, optional `projectId`, `email`, `role`). There is no separate "provision new user" path: `POST /invites/with-user` appears in the OpenAPI yaml but is NOT registered by the live Go server, so a not-yet-registered email is invited the same way and completes account setup through the invite email link (`GET /invites/accept?token=`).

- **Why:** the live server exposes a single email-based invite path; search only helps fill the email field.
- **Alternative rejected:** `POST /invites/with-user` — documented in the OpenAPI yaml but unimplemented in the running server (would 404).

### D4 — Invite accept/decline is in-app, session-scoped

Pending invites are read with `GET /invites/pending` (returns the `token` in `PendingInviteDto`); accept posts `AcceptInviteDto {token}` to `/invites/accept`, decline posts to `/invites/{id}/decline`, all carrying the session Bearer token from `add-auth-project-frame`. The email-link deep-accept path is not built in this slice.

- **Why:** reuses the established session trust boundary; the invite token arrives already in the pending list, so no unauthenticated route is required.
- **Alternative rejected:** an unauthenticated deep-link `/invites/accept?token=...` page — a separate trust boundary not needed for the in-app flow.

### D5 — Profile via `/user/profile`

The profile page reads and writes `GET/PUT /user/profile` (`UserProfileDto`/`UpdateUserProfileDto`). `/auth/me` returns the same `UserProfileDto` but is introspection-oriented; the profile feature SHALL use `/user/profile` as its canonical surface.

- **Why:** `/user/profile` supports both read and update, the one thing `/auth/me` cannot do.
- **Alternative rejected:** reading from `/auth/me` and writing to `/user/profile` — two sources for one page invites drift.

### D6 — Project create gains an org selector

The create-project form from `add-auth-project-frame` carries `orgId` (required by `CreateProjectDto`, "no implicit default org"), fed by the org list/access tree. The org selector is mandatory before a project can be created.

- **Why:** Memory requires `orgId` and will reject a project without it; the form must not let the user hit that error blind.
- **Alternative rejected:** auto-selecting the first org — hides the choice and misfiles projects in multi-org users.

### D7 — No organization delete in the UI

`DELETE /orgs/{id}` (projects/documents/chunks/conversations cascade) is NOT exposed in this slice.

- **Why:** the proposal scope is "organization list and creation"; a destructive cascade needs its own confirm/scope work.
- **Alternative rejected:** a one-click delete with a confirm dialog — still too destructive to bundle here.

## Risks / Trade-offs

- **[Last-admin removal]** Memory 403s removing a project's last admin (`last-admin`) → Mitigation: map that error to an explicit "assign another admin first" message and disable the remove affordance when the user is the sole admin.
- **[Duplicate org name / 10-org limit]** `POST /orgs` 409s on both → Mitigation: map the 409 conflict to a clear form error surfaced next to the name field.
- **[Role-enum mismatch]** the invite enum (`org_admin`, `project_admin`, `project_user`) differs from the membership enum (`org_admin`, `org_member`) → Mitigation: the spec pins the exact role strings and unit tests assert the wire payload carries only supported values.
- **[Search-vs-provision race]** a just-provisioned user may not appear in `/users/search` immediately → Mitigation: the with-user fallback remains available regardless of search results.
- **[Destructive org delete]** available in the API but not the UI → Mitigation: not exposed (D7); no handler or route is added for it.

## Migration Plan

1. Add Memory client methods + DTOs in `gateway/` for orgs, members, invites, user search, orgs-and-projects, and profile (mirroring the `MemoryClient` patterns in `memory.go`/`settings.go`), with unit tests against the fake memory backend.
2. Add handlers + routes in `gateway/` behind the session middleware from `add-auth-project-frame`, returning JSON on `/api` and rendering templ pages on `/ui`.
3. Add templ UI (org switcher, members, invites, profile) wired into the `ui.go` shell, with render tests.
4. Extend the create-project form to carry `orgId` from the org selector.
5. Verify: `go build ./...`, `go test ./...`, `templ generate`, `task lint`, and a DevTools browser pass of the full org→member→invite→accept flow.

## Open Questions

- Does the emailed invite link need a gateway-rendered deep-accept page, or is in-app accept (from `/invites/pending`) sufficient for this slice? Leaning in-app only; revisit if Memory's invite email must deep-link into the console.
