# Account switch-row email backfill

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-account-identity-display](../sessions/2026-09-08-account-identity-display.md)

## Resolution (2026-09-09)

Backfill at the registry-put transitions instead of per-render fetches. The
registry is in-memory (lost on restart) and rows are only ever created when an
account rotates active → cached — `registerAccount` (OIDC select_account) and
`authSwitch`. Both sites now backfill the email from the account's **own**
Memory profile (`backfillAccountEmail` in `gateway/account.go`) before caching:
the profile is fetched with the account's own access token because the ambient
session token can belong to a different account on the OIDC callback. Failures
swallow silently (row keeps no email line). Covers every row within a process
lifetime. Unit-tested (`gateway/account_backfill_test.go`).

## What

Backfill the email for cached "switch account" rows in the avatar dropdown. Today the active account's email falls back to the Memory profile when the IdP token omits it, but each cached switch-account row (`accounts` in `gateway/ui.go`, from the registry) shows email only when the registry record already carries it.

## Why

Consistency: the email gap that prompted the fallback fix applies equally to secondary cached accounts; their rows render avatar + name with a silently missing second line.

## Depends on

none

## Notes

- Each cached account has its own token, so backfilling needs a per-account Memory `GET /api/user/profile` (or registry storing identity claims at registration) — more than a trivial plumbing change, hence deferred.
- Active account fallback already landed in `09062ef` (`gateway/ui.go`, `gateway/org_members_ui.go`, `gateway/memory.go`).
