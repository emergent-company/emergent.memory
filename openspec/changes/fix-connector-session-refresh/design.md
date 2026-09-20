# Design

## Context

The macOS connector app does not own an OIDC session. It shells out to the bundled
`memory-connector` CLI and passes `--config ~/.config/memory-connector.yml`; the CLI maps
that to the base dir `~/.config/memory-connector`, where `accounts/<host>.json` holds the
session (0600, identity-independent by design).

Sign-in is PKCE: `auth start` / `auth complete` store the session, refresh token included.
`auth access-token` refreshes when the access token is expired or within 60 s of expiry.

## Goals / Non-Goals

- **Goals**: the refresh token survives sign-in; a transient refresh error does not destroy
  the session; the app never overwrites the session it just created.
- **Non-Goals**: multi-process refresh locking (out of scope — noted as a residual risk);
  changing token lifetimes; changing `auth status` semantics.

## Decisions

### D1 — `auth import` merges a missing refresh token

`auth import` already accepts an optional `refresh_token`. Change it so that when the
payload's refresh token is empty it carries over the currently stored session's refresh
token before `Save`. `Save` replaces the whole file, so the merge must happen in memory
first. A non-empty payload refresh token still replaces the stored one.

This fixes the app bridge generically, without the CLI ever having to print a refresh token
(`auth access-token` deliberately does not expose it).

### D2 — Classify refresh failures; only auth rejections clear the session

The SDK's `OAuthProvider.Refresh` currently returns plain `fmt.Errorf` strings for both a
non-200 token-endpoint response and transport failures, so callers cannot tell them apart.
Introduce a typed `auth.RefreshError{StatusCode int, Code string, Err error}` (returned for
non-200 responses). `account.Manager.Refresh` then clears the session (via `Logout`) only
when the error is a `*RefreshError` whose status is 400/401 or whose `Code` is
`invalid_grant`/`invalid_token`; network, timeout, parse, and 5xx errors leave the stored
session intact and are returned to the caller unchanged.

### D3 — Drop the post-sign-in import from the app

`auth complete` already persisted the session, so the app's unconditional
`importSessionToConnectorCLI` right after sign-in is redundant and destructive. Remove that
call. The self-heal bridge stays for the legacy/absent-session case (and is now
non-destructive thanks to D1).

## Risks / Trade-offs

- Residual: each CLI invocation is a separate process, so `refreshMu` cannot serialize
  refreshes across the app's fan-out. Zitadel rotates refresh tokens single-use, so a lost
  race yields `invalid_grant`. With D2 that no longer deletes the session (the loser
  re-reads the winner's persisted token on the next call), which is a strict improvement,
  but a dedicated single-flight refresh remains future work.
- Adding an exported SDK error type increases the connector's dependency on unreleased SDK
  changes; the connector already depends on unreleased `IsRevoked`/`OwnedByCaller` fields
  from the same line of work, so this is consistent (a workspace build is already required).

## Migration

No data migration. Existing sessions without a refresh token cannot be repaired locally;
they require one more sign-in, after which the token is preserved.
