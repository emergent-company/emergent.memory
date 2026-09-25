## Context

Standalone mode (`domain/standalone`) seeds one user (`zitadel_user_id='standalone'`), one org, and one project, and `checkStandaloneAPIKey` (`pkg/auth/middleware.go`) maps a single `STANDALONE_API_KEY` to that user. `Service.Accept`/`Service.Decline` verify the invitee email ∈ the caller's `core.user_emails` and return 403 otherwise, so happy-path accept/decline are unreachable with a single identity. Note: the primary standalone user currently has **no** `core.user_emails` row (only `core.user_profiles.display_name`); the accept gate depends on that table.

## Goals / Non-Goals

**Goals:**
- Enable full invite-lifecycle e2e (accept + decline happy paths) with a second identity.
- Keep the change scoped to standalone test mode; do not alter production auth.

**Non-Goals:**
- Fixing the primary standalone user's missing `core.user_emails` row (out of scope).
- Production/auth changes beyond standalone mode.

## Decisions

- Add `STANDALONE_USER_EMAIL_2` + `STANDALONE_API_KEY_2`; bootstrap creates `zitadel_user_id='standalone-2'` with a `core.user_emails` row (the accept gate requires it). The second user needs no org/project — accept creates the memberships.
- `checkStandaloneAPIKey` matches `APIKey` → `standalone` or `APIKey2` → `standalone-2`, looking up the actual UUID from `core.user_profiles` (as it already does for the primary user).
- Tests assert membership via `GET /api/projects/{projectId}/invites` (`status` field), which reflects accept/decline/revoke transitions without needing a members endpoint.

## Risks / Trade-offs

- The second identity is created only during the fresh-DB bootstrap path; the e2e stack uses an ephemeral Postgres, so this holds. A reused/persisted standalone DB would not re-run bootstrap.
- Adding a second static API key slightly widens standalone auth surface, but it is test-only and gated behind `STANDALONE_MODE`.
