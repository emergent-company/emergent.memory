# Verify dev Zitadel native callback

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
Confirm the **dev** Zitadel application (`Memory Connector Dev`, client
`390138928478289930`, issuer `https://zitadel.dev.emergent-company.ai`) allows
the connector's redirect scheme `com.emergent.memory.connector://callback`
(plus a matching post-logout URI, Development mode, refresh enabled). Verify a
full dev sign-in on a clean profile.

## Why
Dev sign-in succeeded once, but the dev app provisioning was inferred; a missing
redirect URI or Development mode would break new installs (e.g. the `tool` Mac)
with a confusing error.

## Depends on
- Infra/Zitadel console access (see `INFRASTRUCTURE.md` for the values).

## Notes
- Prod app: client `390138006318678019`, issuer `https://auth.emergent-company.ai`.
- Record the outcome in `INFRASTRUCTURE.md` if anything changed.
