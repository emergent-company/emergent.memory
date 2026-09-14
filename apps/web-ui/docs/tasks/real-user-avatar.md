# Real user avatar in sidebar footer

**Status:** done
**Created:** 2026-09-04
**Source:** [2026-09-04-add-auth-project-frame](../sessions/2026-09-04-add-auth-project-frame.md)

**Resolved:** 2026-09-08 — the sidebar footer was superseded by the avatar-only topbar
(D24). Real avatars now work end-to-end: Zitadel `picture` read from the userinfo endpoint,
manual upload/override in Memory object storage, resolution `override > picture > initials`,
and a profile-page photo modal. See [2026-09-08-account-avatar](../sessions/2026-09-08-account-avatar.md).

## What
Replace the initials fallback in `userProfileFooter` with a real avatar image when one is
available.

## Why
The sidebar profile footer currently renders initials from the session `name` because neither
source yields a ready image URL: Zitadel userinfo (as the memory backend consumes it) exposes
no `picture` claim, and Memory stores the avatar as an S3 `avatarObjectKey` (needs a signed
URL). The session already carries an opportunistic `picture` from the id_token if Zitadel
returns one.

## Depends on
- `deploy-zitadel-auth` (to observe the real id_token claims).

## Notes
- Check the real Zitadel id_token/userinfo for a `picture` claim; if present it flows through
  `sessionClaims.Picture` already.
- Otherwise resolve Memory `avatarObjectKey` (`GET /api/user/profile`) to a signed URL — needs
  a gateway-side object-signing endpoint or memory support.
