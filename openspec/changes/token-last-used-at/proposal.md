## Why

`core.api_tokens.last_used_at` exists (`apps/server/domain/apitoken/entity.go`) but is never written when a token is actually used. `validateAPIToken` (`apps/server/pkg/auth/middleware.go`) validates a token without touching the column, so `emt_*` token audit is mint-time and revoke-time only — there is no way to answer "was this token used, and when?" nor to find a dormant credential for revocation (issue #845).

PR #857 added a best-effort `last_used_at` touch for the **device-credential path only** (a per-request fire-and-forget goroutine). This change generalizes that mechanism to **every** `emt_*` token and, in doing so, removes the unbounded per-request write by adding a per-token throttle.

## What Changes

- **Generalize the existing touch.** The device-only `touchDeviceTokenLastUsed` is replaced by a `tokenUsageTracker` that records `last_used_at` for every successfully validated `emt_*` token — device credentials, project/account tokens, agent-share keys, and ephemeral sandbox tokens alike. The device path is a strict subset; its ceiling/surface guards and `last_used_at` touch are unchanged in effect.
- **Throttle the write.** The tracker coalesces writes with a per-token throttle (default 1 minute): at most one `UPDATE core.api_tokens SET last_used_at = NOW()` per token per minute. Writes remain fire-and-forget on a short-lived goroutine with a bounded timeout and never fail the request. This is a write-frequency *improvement* over the #857 device path (per-request) while extending coverage to all tokens.
- **Document the semantics.** A new capability `api-token-audit` defines what "last used" means, the resolution, and the write-frequency bound (see delta spec).

## Decisions

1. **Reuse, do not re-implement.** The #857 device mechanism (detached goroutine, `last_used_at = NOW()`, never surfaced to the caller) is the established shape. This change generalizes it rather than adding a second parallel write path, and confirms the device path is not regressed.
2. **Per-token throttle over a synchronous UPDATE.** A synchronous `UPDATE` per request on the hot auth path would be a performance regression; a per-token in-memory throttle (last-flush timestamp per token) bounds write frequency to once per token per minute. This is the simplest mechanism that satisfies "best-effort, non-blocking, bounded".
3. **Touch on successful validation, before profile resolution.** The touch happens after the token row is found valid (not revoked, not expired, within any ceiling) but before the user-profile lookup. An orphaned token whose owner row is missing still counts as "used" — the credential itself was valid; the profile failure is a separate data-integrity concern and does not suppress the audit write.
4. **Direct and gateway-proxied use share the same path.** The gateway forwards the credential verbatim as a Bearer/X-API-Key, so the server's `validateAPIToken` runs identically for both — no separate touch is needed.
5. **No weakening of existing boundaries.** The #857 ceiling/surface checks and the #791 `session_required` boundary are untouched; this change is additive audit metadata only.

## Capabilities

### New Capabilities

- `api-token-audit`: the semantics and write-frequency bound of `core.api_tokens.last_used_at` for `emt_*` token use, and the best-effort, non-blocking, throttled recording of it on successful validation.

### Modified Capabilities

None. The device-specific `last_used_at` wording in `web-device-credential` is a special case of the general guarantee and is left in place; `api-token-audit` supersedes the general gap it referenced.

## Impact

- `apps/server/pkg/auth/`: new `tokenUsageTracker` (per-token throttle + fire-and-forget flush); `validateAPIToken` calls it for every `emt_*` token; the device-only `touchDeviceTokenLastUsed` is removed.
- `apps/server/pkg/auth/` tests: DB-backed fail-first (last_used_at set after validation), non-fatal touch, throttle bound; device path unaffected.
- No route-auth, scope, ceiling, or surface changes.

## Back-compat

`last_used_at` is nullable and previously (except device credentials) always NULL; the change only starts populating it. Consumers that read it (dashboards, unused-token sweeps) see strictly more data. No schema or API change.
