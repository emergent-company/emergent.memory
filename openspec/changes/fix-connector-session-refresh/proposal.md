# Fix connector session refresh

## Why

Users of the macOS connector must sign in again roughly every time the app is rebuilt or
relaunched. Investigation shows the OIDC session is file-based and identity-independent, so
rebuilds do not delete it. The real causes are:

1. **The refresh token is destroyed at sign-in.** The macOS app completes PKCE sign-in
   (`auth complete`), which stores a session *with* a refresh token, and then immediately
   bridges the session into the CLI via `auth import`. That import payload carries only
   `access_token`, `expires_at`, `issuer`, and `email` — no `refresh_token` — and the CLI
   overwrites the whole session file. The refresh token minted by the IdP is gone seconds
   after login, so once the access token expires (Zitadel default 12 h) the session cannot
   be renewed and the app flips to signed-out.
2. **Any refresh failure deletes the session.** `account.Manager.Refresh` calls `Logout` on
   every token-endpoint error, including transient network/5xx failures. A blip during a
   post-rebuild launch forces a fresh sign-in.

## What Changes

- `auth import` merges instead of clobbering: a payload without `refresh_token` keeps the
  stored one.
- Refresh failure handling distinguishes an authentication rejection (refresh token
  genuinely invalid) from a transient failure; only the former clears the session.
- The macOS app stops re-importing the session immediately after a successful PKCE sign-in —
  the CLI already stored it, refresh token included.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `connector-core`: session import preserves a stored refresh token; refresh failures only
  clear the session on an authentication rejection.
- `mac-connector-auth`: the app does not re-import (and thereby overwrite) the session it
  just established via `auth complete`.

## Impact

- `apps/connector.linux/cmd/memory-connector/auth_import.go`
- `apps/connector.linux/internal/account/account.go` (+ refresh error classification)
- `apps/server/pkg/sdk/auth/oauth.go` (typed refresh error so callers can classify)
- `apps/connector.mac/MemoryConnector/Sources/AccountStore.swift` (+ its tests)

## Scope

- **In scope**: keeping a stored refresh token alive across import; not discarding the
  session on transient refresh errors; removing the destructive post-sign-in bridge.
- **Out of scope**: `auth status` reporting `signed_in` for expired-but-unrefreshable
  sessions; cross-process refresh serialization (each CLI call is its own process);
  publishing a new SDK version.
