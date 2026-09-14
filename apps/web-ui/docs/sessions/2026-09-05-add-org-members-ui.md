# 2026-09-05 — add-org-members-ui (org/member/invite/profile management)

## Goal
Implement the `add-org-members-ui` OpenSpec change: add organization, project-member,
invitation, and user-profile management to the Alfred web console, backed by Emergent
Memory's org/member/invite/profile endpoints.

## Outcome
**Done.** Three layers landed and committed:

- Memory client methods + DTOs (`a57d22f`)
- JSON API handlers + routes (`ca39b09`)
- templ UI — orgs list + separate create page, merged members+invitations view,
  read-only member details page, profile with pending-invites section (`8a358ac`)
- Spec/tasks updated (`8461277`, `900a97c`)

Automated verification green (build / vet / test / templ / lint). Task 4.3 (manual
browser walkthrough) deferred to the user — blocked in this environment by the
production Memory backend (`memory.emergent-company.ai`) + no test OIDC session.

## Decisions
- **Drop `POST /invites/with-user`** — the live Memory Go server has no such route
  (OpenAPI yaml is stale); every invite is email-based via `POST /api/invites`, and a
  not-yet-registered email provisions itself via the invite link.
- **Merge Members + Invitations into one `/members` view** — pending invites render as
  "Not responded" members (user direction).
- **Separate create-organization page (`/orgs/new`) + read-only member details page
  (`/members/:userId`)** — user direction; role editing is future work.
- **Incoming pending invites (accept/decline) live on the Profile page** — they're the
  signed-in user's personal inbox, not project membership.
- **Surgical git staging in shared master** — user chose this over a worktree despite
  heavy parallel-session activity on `main.go`/`backend.go`/`handlers_test.go`.

## Changes
- `gateway/memory.go` — org/member/invite/profile client methods + DTOs
  (`OrgWithProjectsDto`, `ProjectMemberDto`, `SentInviteDto`, `CreateInviteDto`,
  `Invite`, `PendingInviteDto`, `UserSearchResultDto`, `UserProfileDto`, …).
- `gateway/backend.go` — `MemoryBackend` interface additions.
- `gateway/handlers_test.go` — `fakeMemory` capture fields + error injection
  (`createOrgErr`, `declineInviteErr`).
- `gateway/memory_test.go` — client unit tests (httptest pattern).
- `gateway/org_members_handlers.go` — 13 JSON handlers (createOrg, listMembers,
  removeMember, listInvites, createInvite, listPendingInvites, acceptInvite,
  declineInvite, cancelInvite, searchUsers, getProfile, updateProfile).
- `gateway/main.go` — `/api/*` routes + UI PRG routes (`/orgs`, `/orgs/new`,
  `/members`, `/members/new`, `/members/:userId`, `/profile`, `/invites/:id/*`).
- `gateway/org_members_ui.go` + `org_members_ui.templ` — the screens + PRG handlers.
- `gateway/ui.go` — sidebar nav (Organizations, Members; Profile under Account).
- `openspec/changes/add-org-members-ui/*` — design D3 (drop with-user), spec
  requirements, tasks updated.

## Verification
- `go build ./...` — pass
- `go vet ./...` — pass
- `go test ./... -count=1` — pass (gateway + webui)
- `templ generate` — no diff
- `golangci-lint run` — 1 pre-existing issue (`auth_ui_test.go` QF1001 De Morgan);
  later fixed by a parallel session. `task lint` wrapper needs `lefthook` (not installed).

## Open questions / follow-ups
- **Member role editing** — details page is read-only by design; Memory has no
  member-role PATCH, so a change is remove + re-invite.
- **Org-level invite** (`org_admin` role) — deferred; the invite form is project-scoped.
- **`listOrgs` JSON handler** returns `null` (not `[]`) on empty — pre-existing minor
  inconsistency vs `listProjects`.
- **Task 4.3 manual browser test** — needs user's DevTools browser + a test Memory
  instance + real OIDC session.

## Tasks
- [verify-org-members-browser](../tasks/verify-org-members-browser.md) — manual browser walkthrough (task 4.3)
- [archive-add-org-members-ui](../tasks/archive-add-org-members-ui.md) — archive the OpenSpec change after 4.3
- [member-details-role-editing](../tasks/member-details-role-editing.md) — role editing on the member details page
- [org-level-invite](../tasks/org-level-invite.md) — org-level (org_admin) invitations
