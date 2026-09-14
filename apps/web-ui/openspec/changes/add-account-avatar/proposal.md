## Why

Signed-in users have no way to control their account avatar. Today an avatar only appears if Zitadel happens to emit a `picture` claim — which it does not for IdP (Google/GitHub) logins by default, and which the gateway reads from the wrong place (the ID token, not userinfo). Users need a way to set an avatar, have it automatically sync from their social login when Zitadel is configured for it, and override it manually.

## What Changes

- Add a first-class account avatar to the signed-in user's account, surfaced in the topbar account menu and the `/profile` page (replacing the initials-only placeholder).
- Resolve the avatar from the correct OIDC source (userinfo endpoint `picture` claim, not the ID token) so IdP-provided avatars actually populate.
- Automatically sync the avatar from the user's social IdP login when Zitadel is configured to provide one (via Zitadel's `picture` claim).
- Allow the user to upload a manual avatar that **overrides** the social avatar.
- Allow the user to remove the override, reverting to the social avatar (or to initials when neither exists).
- Fall back to initials when no avatar is available.

## Capabilities

### New Capabilities

- `account-avatar`: the signed-in account's avatar — resolution precedence (override > social > initials), automatic sync from the IdP via Zitadel, manual upload override, and remove-to-revert behavior.

### Modified Capabilities

<!-- None: no existing spec covers account/profile/avatar behavior. -->

## Impact

- **Gateway** (`/root/alfred/gateway`): session claims carry the avatar; OIDC identity read moves from ID-token `picture` to userinfo; account menu (`account_menu.templ`) and profile page (`org_members_ui.templ`) render the avatar and expose upload/remove controls.
- **Memory backend** (`emergent.memory`, cloned in `.slim/clonedeps`): write path for the existing-but-unused `core.user_profiles.avatar_object_key` — an upload endpoint (object storage) and profile DTO/request wiring.
- **Zitadel**: may require config/`Actions` for IdP picture propagation, and/or the app-level `idTokenUserinfoAssertion` setting; the gateway remains IdP-agnostic and consumes only the `picture` claim.
- **Tests**: TDD — unit tests for avatar resolution/precedence, upload validation, and session propagation are the minimum bar.
