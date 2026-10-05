## Why

The merged auth change (#1508, task 1.6) maps API-token profile-lookup failures during `validateAPIToken` to the correct HTTP class: a missing user row → `401 invalid_token`, and any other (database/pool) failure → `503 service_unavailable`. The archived capability spec `openspec/specs/auth-error-semantics/spec.md` documents the token-row lookup classification but **omits this follow-up profile-lookup rule**, so the spec under-describes shipped behaviour (surfaced during the #1511 archive review, issue #1532).

## What Changes

- **Document the 401 side.** Extend "Authoritative token rejections return 401" so the authoritative-rejection list includes "an API token's owning user profile no longer exists", with a scenario.
- **Document the 503 side.** Extend "Infrastructure token-validation failures return 503" so the database/pool clause explicitly covers a failure of an API token's follow-up user-profile lookup, with a scenario.
- **No behaviour change.** This is a spec-only correction describing code already shipped in #1508 (`classifyProfileLookupError` in `apps/server/pkg/auth/middleware.go`).

## Decisions

1. **Modify the existing requirements, not add a parallel one.** The profile lookup is a stage of the same API-token validation path, so it belongs under the existing 401/503 requirements rather than a new capability. OpenSpec `MODIFIED Requirements` restate the full requirement with the added scenario.
2. **Describe the shipped mapping verbatim.** `classifyProfileLookupError` returns `ErrInvalidToken` on `sql.ErrNoRows` and `apperror.NewServiceUnavailable` otherwise; the spec pins exactly that, nothing broader.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `auth-error-semantics`: adds the profile-lookup (`validateAPIToken`) 401/503 classification — missing user → `401 invalid_token`, database/pool failure → `503 service_unavailable`.

## Impact

- `openspec/specs/auth-error-semantics/spec.md` (via the delta) — documentation only.
- No code, API, route, or behaviour change.

## Back-compat

The delta only narrows the spec text to match already-shipped behaviour. No consumer-visible change.
