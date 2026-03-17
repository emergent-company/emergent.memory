## Why

Projects currently support only two roles (`project_admin`, `project_user`) with no way to invite collaborators by email or manage team membership from the CLI. Teams need to share projects with read-only stakeholders (reviewers, clients, auditors) and manage access without leaving the terminal.

## What Changes

- New `project_viewer` role with read-only access enforced at the API level
- Invitation flow: generate a signed token, send an email with install instructions and an accept link
- Invitation acceptance creates a user account (if needed) and adds project membership
- New `memory projects team` subcommands: `list`, `invite`, `remove`
- Usage statistics per member (token last-used, document count touched) surfaced in `team list --stats`
- New database table `kb.project_invitations` for pending invitations
- New email template: `project-invitation`

## Capabilities

### New Capabilities

- `project-viewer-role`: New `project_viewer` role for projects; enforces read-only scopes (`data:read`, `schema:read`, `agents:read`, `projects:read`) at the API middleware level; existing `project_admin` / `project_user` roles are unchanged
- `project-invitations`: Backend flow to create, deliver, and accept project invitations — invitation record, signed token, email dispatch, accept endpoint that provisions membership
- `project-team-management`: CLI subcommands under `memory projects team` for listing members with roles and usage stats, inviting by email with a role flag, and removing members by email

### Modified Capabilities

- `cli-orgs-crud`: No requirement changes — team management lives under `projects`, not `orgs`

## Impact

- **Database**: New migration adding `kb.project_invitations` table and `project_viewer` as an allowed role value
- **API**: New endpoints — `POST /v1/projects/:id/invitations`, `GET /v1/invitations/:token/accept`, `GET /v1/projects/:id/members`, `DELETE /v1/projects/:id/members/:userId`
- **Auth middleware**: Viewer role must be recognized; read-only scope set enforced when token was issued with viewer scopes
- **Email**: New `project-invitation` Handlebars template with install instructions, CLI snippet, and accept link
- **CLI**: New `team` subcommand group added to `projectsCmd`
- **SDK**: New SDK methods for invitations and members endpoints
