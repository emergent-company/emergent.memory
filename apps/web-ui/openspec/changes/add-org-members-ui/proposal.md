## Why

The auth/project frame (`add-auth-project-frame`) lets users sign in and switch projects, but there is no way to manage the organizations and members those projects live under. Emergent Memory already exposes orgs, members, and invites; Alfred is missing the UI to use them.

## What Changes

- Add organization list and creation.
- Add member listing, invitation, and role management for an organization/project.
- Add invite-accept flow and a user profile view.
- Surface the org/member context needed when creating projects.

## Capabilities

### New Capabilities
- `org-membership`: manage organizations, members, invitations, and roles.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods (`ListOrgs`, `CreateOrg`, members/invites), handlers, routes.
- `gateway/ui.go` + templ: org switcher, member/invite/role screens.
- Builds on the session + project frame from `add-auth-project-frame`.
