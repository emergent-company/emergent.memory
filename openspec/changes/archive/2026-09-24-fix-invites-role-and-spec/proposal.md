## Why

The `project-invitations` capability spec documents a legacy surface — a `kb.project_invitations` table and `/v1/.../invitations` routes — that no longer matches the shipped implementation (`kb.invites` served under `/api/invites` and `/invites/accept`). A reader implementing from the spec would build against tables and routes that do not exist. Separately, the invite acceptance path hardcoded the invitee's organization membership role to `member`, silently under-granting `org_admin` invitations.

## What Changes

- Fix the invite acceptance path to grant the role the invitation carries: `org_admin` invitations grant `org_admin` organization membership; project-scoped invitations grant `member`. Unexpected stored roles fail closed.
- Reconcile the `project-invitations` capability spec with the shipped implementation: correct table (`kb.invites`), correct route prefix (`/api/invites`, `/invites/accept`, `/api/projects/:projectId/invites`), and the invitation→organization-membership role semantics.

## Capabilities

### Modified Capabilities
- `project-invitations`: rewrite the spec to match the shipped invites implementation and record the role-granting behaviour change.

## Impact

- `apps/server/domain/invites/`: `Accept` now derives the organization membership role from the invitation role and rejects unexpected roles; both-roles and fail-closed tests added.
- `openspec/specs/project-invitations/spec.md`: reconciled with reality via the change delta.
