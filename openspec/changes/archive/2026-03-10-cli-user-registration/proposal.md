## Why

New users of the Emergent CLI have no way to self-register an account from the command line. Currently, accounts are silently auto-created on first use of an authenticated endpoint — but to reach that point the user must already have a valid Zitadel token, which requires going through an external auth flow they may not know about. A dedicated `emergent register` command closes this gap by guiding users through Zitadel's OAuth Device Authorization flow to create and verify their account, ending with credentials saved locally and an account confirmed on the server.

## What Changes

- **New `emergent register` CLI command** — interactive wizard that walks a new user through the registration flow, redirects them to Zitadel for account creation, polls for the result, and on success saves credentials to `~/.emergent/credentials.json`.
- **Reuse existing Device Authorization flow** — registration leverages the same OIDC device-flow infrastructure already used by `emergent login`; the difference is surfacing it as an explicit first-time-user entry point with prompts tailored to registration.
- **Server-side account confirmation endpoint** — a lightweight `GET /api/auth/me` response already exists; `register` calls it after token acquisition to confirm the account was auto-provisioned and print the user's email/ID to the terminal.
- **E2E test** — a CLI-level test that exercises the full `register` flow using a test token to verify the account confirmation step works end-to-end.

## Capabilities

### New Capabilities

- `cli-register`: CLI command `emergent register` that guides a new user through Zitadel Device Authorization flow to create an account, saves credentials, and confirms server-side account provisioning.

### Modified Capabilities

- `cli-tool`: The `authentication-and-credential-management` requirement expands to include a `register` command as a distinct entry point from `login`, with registration-specific UX (prompts, output, error messages).

## Impact

- **`tools/emergent-cli/internal/cmd/auth.go`** — add `runRegister()` command wired into the root CLI.
- **`tools/emergent-cli/internal/auth/`** — reuse `DeviceFlow`, `PollForToken`, `GetUserInfo`; no new auth primitives needed.
- **`apps/server-go/domain/authinfo/`** — `GET /api/auth/me` already returns user context; used as-is for confirmation.
- **`apps/server-go/tests/e2e/`** — new E2E test for register flow using the existing test-token infrastructure.
- No database migrations required — account creation already happens via `EnsureProfile` on first authenticated request.
- No breaking changes.
