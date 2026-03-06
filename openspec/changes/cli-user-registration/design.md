## Context

The Emergent CLI already has a full `emergent login` command that uses the OAuth 2.0 Device Authorization Grant flow against Zitadel. Under the hood, server-side account creation is implicit: on any authenticated request the `EnsureProfile()` middleware automatically inserts a row in `core.user_profiles` if none exists. There is no explicit registration step or endpoint.

The gap is UX: a brand-new user has no discoverable entry point for "create an account." They land at `emergent login`, complete the Zitadel device flow, and their account is silently created — but there is no feedback that an account was created versus that they are returning to an existing one, and no `register` command to signal "first time? start here."

The `GET /api/auth/me` endpoint already returns user identity (`user_id`, `email`, `type`) after any valid token is accepted, which is sufficient for confirming server-side account provisioning.

## Goals / Non-Goals

**Goals:**
- Add `emergent register` as a first-class CLI command that guides new users through account creation
- Reuse all existing auth infrastructure (Device Authorization Grant, `PollForToken`, `GetUserInfo`, `~/.emergent/credentials.json`)
- After token acquisition, call `GET /api/auth/me` to confirm server-side profile creation and display user ID + email to the user
- Write an E2E test that exercises the post-token confirmation step (`/api/auth/me`) using the existing `e2e-test-user` static token
- Update CLI status command output to hint at `register` for unauthenticated users

**Non-Goals:**
- No new Zitadel configuration — device flow scopes and client ID remain unchanged (`emergent-cli`, `openid profile email`)
- No new server-side endpoints — `EnsureProfile` + `GET /api/auth/me` are sufficient
- No "invite code" or gated registration logic
- No changes to the `emergent login` flow itself — `register` is additive
- No UI changes in the React admin app

## Decisions

### Decision: `register` reuses `login`'s Device Authorization Grant flow

**Rationale:** Zitadel's device flow is the only auth mechanism the CLI supports for interactive sessions. Using the same flow for registration means zero new auth primitives. The only difference is messaging ("Creating your account" vs "Signing in") and the post-flow confirmation call to `/api/auth/me`.

**Alternative considered:** Redirect to a Zitadel registration URL directly (skipping device flow). Rejected because it requires knowing the Zitadel issuer URL and constructing a registration redirect URI, adds coupling to Zitadel internals, and doesn't work in environments where the browser can't be opened. The device flow already handles the "open browser" case and falls back to manual URL input.

### Decision: Account confirmation via `GET /api/auth/me`, not a new endpoint

**Rationale:** `EnsureProfile` guarantees the profile exists by the time any authenticated response is returned. Calling `/api/auth/me` with the newly obtained token is therefore a reliable confirmation signal — if it returns 200, the account exists. Adding a new endpoint would be dead weight.

**Alternative considered:** New `POST /api/auth/register` endpoint. Rejected — adds server-side surface area for something that already happens automatically.

### Decision: `register` is a sibling of `login`, not a subcommand

**Rationale:** Both commands are top-level entry points for unauthenticated users. Making `register` a subcommand (e.g., `emergent auth register`) would bury it. Consistent with `emergent login`, `emergent status`, `emergent logout`.

### Decision: E2E test uses static `e2e-test-user` token, not a live device flow

**Rationale:** The device flow requires a real browser and Zitadel interaction, which cannot run in CI. The test scope is the account-confirmation step: given a valid token, does `/api/auth/me` return the expected user data and does the CLI print the right success message? The static test token satisfies this.

## Risks / Trade-offs

- **Zitadel "new account" vs "existing account"** — The device flow itself does not distinguish between a returning user and a first-time user completing registration. `emergent register` cannot guarantee it's creating a new account. The UX copy must set expectations ("If you already have an account, this will log you in instead") to avoid confusion. This is a deliberate trade-off to avoid adding server-side state.
- **No standalone-mode support** — In standalone mode there is no Zitadel and the device flow is unavailable. `emergent register` should detect standalone mode (server health endpoint returns `mode: standalone`) and print a clear "registration not available in standalone mode" message rather than failing cryptically.
- **`/api/auth/me` requires no project context** — The endpoint works without `X-Project-Id`, which is correct: new users have no project yet. The E2E test must not set a project header.

## Migration Plan

1. Implement `runRegister()` in `tools/emergent-cli/internal/cmd/auth.go` — call existing device flow, then call `/api/auth/me`, print result.
2. Register `registerCmd` in the `init()` block alongside `loginCmd`.
3. Update `runStatus()` unauthenticated branch to mention `emergent register` as an alternative to `emergent login`.
4. Add E2E test in `apps/server-go/tests/e2e/` targeting `GET /api/auth/me` with `e2e-test-user` token, asserting 200 + non-empty `user_id`.
5. No migrations. No config changes. Hot reload picks up CLI changes on rebuild (`task cli:install`).

## Open Questions

- Should `emergent register` print a link to documentation or onboarding steps after confirming account creation? (Currently out of scope — can be added as a follow-up.)
- Should the confirmation call also attempt to fetch available organizations/projects to print as "next steps"? (Out of scope — `emergent status` already covers this.)
