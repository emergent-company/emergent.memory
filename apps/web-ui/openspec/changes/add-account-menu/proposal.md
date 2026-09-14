## Why

Identity is split awkwardly across the shell: an "Account → Profile" sidebar group and a pinned avatar footer, while the topbar — where account menus conventionally live — has no identity at all. Separately, a user can only be signed into one Memory account at a time; switching means a full logout + re-login.

## What Changes

- Add a topbar account menu (avatar dropdown) showing the active account, any other signed-in accounts, "My Profile", "Add another account", and "Log out".
- Remove the sidebar "Account" group + Profile link and the pinned user-profile footer (identity moves to the topbar).
- Add multi-account session support: sign in to multiple Memory accounts concurrently and switch between them without re-authenticating.
- Extract the Zitadel `sub` claim as the stable account identity.
- Add routes: `/auth/add` (sign in as another account) and `/auth/switch` (switch the active account).

## Capabilities

### New Capabilities
- `account-management`: topbar account menu plus multi-account sign-in, switch, and sign-out.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/auth.go`, `gateway/session.go`, `gateway/oidc.go`: install-id cookie, in-memory account registry, add/switch handlers, `sub` extraction, `prompt=select_account` on the add-account flow.
- `gateway/ui.go`, `gateway/ui.templ`, `gateway/sidebar_user.templ`: account menu component, shell wiring, sidebar "Account" group and footer removal.
- `gateway/main.go`: new `/auth/add` and `/auth/switch` routes.
- Tests: `sidebar_user_test.go`, `org_members_ui_test.go`, plus new account-session and account-menu tests.
