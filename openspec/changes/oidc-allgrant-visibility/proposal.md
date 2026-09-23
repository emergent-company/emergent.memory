## Why

Issue #736 (follow-up to #667/#730 and #803). The OIDC scope mapping is now fail-closed for the introspection path, but the shipped default `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` combined with no introspection credentials means the legacy all-or-nothing grant is **still active out of the box**: any OIDC user authenticated via the userinfo fallback receives `GetAllScopes()`. That permissive posture is completely silent — nothing tells an operator it is on or how to turn it off, so the hardening work is inert by default with no signal.

This change makes the posture **visible only**. It does not change what scopes are granted, does not flip the default, and does not change gating.

## What Changes

- **Loud startup warning.** When the all-grant is active (`ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` AND introspection is not configured), the server emits a `WARN` at startup naming the effect (userinfo-authenticated users get the full Memory scope catalogue) and both remediations (`ZITADEL_CLIENT_JWT`/`ZITADEL_CLIENT_JWT_PATH`, or set the flag `false`).
- **Health visibility.** The `/health` (and `/api/health`) response gains an `oidc_all_grant` check entry. It reports `warning` when the all-grant is active and `healthy` when it is not, with a message naming both remediations. It is neither a critical nor an optional component, so it never changes the overall status or the HTTP code — it is a configuration warning, not a failing check.
- **Single source of truth.** The predicate moves onto `config.ZitadelConfig` (`IntrospectionConfigured`, `UserinfoAllGrantActive`); the auth middleware delegates to it, so the warning, the health check, and the actual gating all agree by construction.
- **Tests.** The predicate is covered for all four combinations of (flag on/off) × (introspection configured/not) at the config, auth (captured log line), and health (check field) layers.

## NOT in scope

- **No behaviour change.** No scope semantics change, no default flip, no gating change. Whether the shipped default should itself flip to `false` is an operator decision, deliberately not taken here.
- **Issue item 3.** The live-Zitadel e2e (real Zitadel instance + CI credentials) remains outstanding; #736 is referenced with `Refs`, not `Closes`.

## Capabilities

### Modified Capabilities

- `oidc-scope-mapping`: the gated all-grant posture is now observable — a startup warning plus a non-failing health check entry — with the predicate centralised on the config type.

## Impact

- `apps/server/internal/config/config.go`: `IntrospectionConfigured` and `UserinfoAllGrantActive` on `*ZitadelConfig`.
- `apps/server/pkg/auth/middleware.go` / `scope_mapping.go`: startup warning; helpers delegate to config.
- `apps/server/domain/health/handler.go`: `oidc_all_grant` health check entry.
- Tests in `internal/config`, `pkg/auth`, `domain/health`.
- `.env.example`: documents the shipped default and the warning. No schema or API change.
