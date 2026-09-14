# Zitadel social-avatar sync (IdP picture propagation)

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-account-avatar](../sessions/2026-09-08-account-avatar.md)

## What
Make social logins (Google/GitHub/…) auto-populate the account avatar by propagating the
IdP `picture` into Zitadel's `picture` claim, so the gateway's userinfo read surfaces it.

## Why
The avatar resolution already prefers override > Zitadel `picture` > initials, and the
gateway now reads `picture` from the OIDC userinfo endpoint correctly. But Zitadel does
**not** copy an external IdP's picture into the user's avatar slot on its own — nothing in
the codebase consumes `GetAvatarURL()`, and there is no config to enable it. So a social
login today yields initials (no override, no Zitadel avatar) until the user uploads one.
This closes the "auto-sync from social logins" half of the original request.

## Depends on
- none (avatar plumbing is deployed; this is additive Zitadel config).

## Notes
- Zitadel's documented approach is **Actions**: a post-auth Action writes the IdP picture
  into user metadata, and a complement-token Action copies it into the `picture` claim
  (Zitadel emits `picture` in the ID token only when `idTokenUserinfoAssertion` is enabled;
  the gateway reads it from userinfo, which always carries it when the avatar is set).
- The gateway stays IdP-agnostic — it only consumes `picture`. No gateway change expected.
- Decide whether "revert to social" should re-apply on next IdP login (the Action runs at
  login, so removing an override leaves initials until the next social sign-in).
