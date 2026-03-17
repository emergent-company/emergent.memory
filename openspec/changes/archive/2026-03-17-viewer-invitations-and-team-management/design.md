## Context

The system has an existing `kb.project_memberships` table with `project_admin` and `project_user` roles. There is no invitation mechanism — members must be added directly (no current UI or CLI for this). The email infrastructure uses Mailgun with a job queue and Handlebars templates. API tokens already support per-scope enforcement via middleware. The CLI has a `projects` command group but no team/member subcommands.

## Goals / Non-Goals

**Goals:**
- Add `project_viewer` role with enforced read-only access
- Full invitation lifecycle: create → email → accept → membership
- CLI `team` subcommand group: `list`, `invite`, `remove`
- Usage stats in team list (last active, 30-day request count)
- Admins can revoke pending invitations

**Non-Goals:**
- Web UI for invitation management
- Bulk invite (CSV import)
- Transferring admin ownership
- SSO/SAML integration
- Per-resource ACLs (viewer sees all project data or nothing)

## Decisions

### Decision: Signed token vs. UUID-based invitation link
Use a random 32-byte token stored as SHA-256 hash (same pattern as API tokens). The accept URL embeds the raw token; the server hashes it and looks up the invitation.

**Rationale**: Consistent with existing API token pattern. Short UUID links are guessable; JWT would require a signing key rotation story.

**Alternative considered**: JWT with HMAC-SHA256 — rejected because it adds key management complexity without benefit (invitations are already single-use and short-lived).

### Decision: Accept endpoint is unauthenticated
`GET /v1/invitations/:token/accept` requires no auth header. The token itself is the credential.

**Rationale**: The invited user may not have an account yet. The endpoint provisions the account as part of acceptance.

**Alternative considered**: Require login first then accept — rejected because it creates a chicken-and-egg problem for new users.

### Decision: Bootstrap token returned on accept
On acceptance, the server returns a short-lived (15-minute) account-level token that the CLI can exchange for a full session via the normal auth flow.

**Rationale**: Lets the `memory` CLI complete login inline after accepting (user follows the link, CLI polls or displays a code, then gets configured). Avoids needing the user to separately run `memory login`.

**Alternative considered**: Redirect to browser login — rejected because it breaks terminal-only workflows.

### Decision: Usage stats source
`lastActiveAt` = max `last_used_at` across all API tokens for that user in that project. `requestCount30d` = count of `core.api_tokens` usage events in the last 30 days (requires an audit/usage log table or approximate from `last_used_at`).

**Rationale**: Lightweight — no new event stream. For v1, `lastActiveAt` comes from `api_tokens.last_used_at`; `requestCount30d` is approximated or deferred if the table doesn't track per-request counts.

**Open question**: If per-request counts aren't tracked today, v1 may omit `requestCount30d` and show only `lastActiveAt`. Revisit if a usage events table exists.

### Decision: Token revocation on member removal
When a member is removed, all `core.api_tokens` for that user scoped to that project are revoked (set `revoked_at = now()`).

**Rationale**: Prevents dangling tokens from continuing to work after membership is removed. Consistent with security expectations.

### Decision: Viewer role enforcement — middleware vs. service layer
Enforce read-only at the API token scope level, not by inspecting `project_viewer` at every handler. When a viewer creates a token, the system validates that no write scopes are requested. The existing `RequireAPITokenScopes` middleware handles the rest.

**Rationale**: The scope system is already the enforcement layer. Adding a parallel role-check in every handler would be redundant and error-prone.

**Alternative considered**: Check `role == project_viewer` in every write handler — rejected as high surface area.

## Risks / Trade-offs

- **Invitation token exposure in logs** → Mitigation: store only the hash; the raw token appears only in the email and the accept URL. Strip tokens from access logs.
- **Email deliverability** → Mitigation: use existing Mailgun job queue with retries; invitation creation succeeds even if email is temporarily queued.
- **Sole-admin removal** → Mitigation: the remove API checks membership count for `project_admin` role before deletion and returns HTTP 409 if it would leave zero admins.
- **Stale invitations** → Mitigation: a background job (or on-read check) marks invitations `expired` once `expires_at` passes. The accept endpoint always validates expiry.
- **requestCount30d accuracy** → Trade-off: if only `last_used_at` is stored (not per-request events), the count will not be available in v1. Surface as `null` in the API and omit from CLI output.

## Migration Plan

1. Add migration: alter `kb.project_memberships` to add `project_viewer` as a valid role value (or rely on application-level validation if the column is unconstrained varchar).
2. Add migration: create `kb.project_invitations` table.
3. Deploy server with new endpoints and email template (backwards-compatible, no existing behaviour changes).
4. Deploy CLI with `team` subcommands.
5. Rollback: remove the new endpoints and CLI commands; existing memberships are unaffected.

## Open Questions

- Does `core.api_tokens` track per-request counts, or only `last_used_at`? If not, `requestCount30d` in `team list --stats` will be `null` in v1.
- Should invitations be visible to all project members or only admins? (Current design: list endpoint not specified — scope to admin-only for now and revisit.)
- Should there be a `memory projects team invitations list` command to show pending invitations? (Out of scope for v1; can add later.)
