## Context

See `proposal.md` — Why for motivation. Current state that shapes the approach:

- **Gateway** (`/root/alfred/gateway`, Go + Echo + templ) is a stateless reverse proxy over the Memory REST API. Identity lives in a signed session cookie (`sessionClaims`, `session.go:17`) carrying `Name`, `Email`, `Picture`, `Sub`.
- **`Picture` today comes from the ID token** (`oidc.go:251`, decoded at `oidc.go:357`). This is broken in practice: Zitadel omits `profile` claims from the ID token whenever an access token is issued (default off for `idTokenUserinfoAssertion`). The avatar has likely never populated from Zitadel.
- **Account menu** (`account_menu.templ`) renders `currentUser.Picture` (`ui.go:253`) → `<img>` or initials. **Profile page** (`org_members_ui.templ`) renders initials only (`profileAvatar`, line 622) — no avatar in scope.
- **Memory backend** (`emergent.memory`, cloned in `.slim/clonedeps`) already has `core.user_profiles.avatar_object_key` (entity `userprofile/entity.go:19`) surfaced as `UserProfileDto.AvatarObjectKey`, but **no write path** — no upload endpoint, `UpdateProfileRequest` omits it, and there is no avatar URL resolution.
- **Object storage service exists** (`internal/storage/storage.go`) with upload + presigned-URL support; the documents domain already uses it for uploads.

## Goals / Non-Goals

**Goals:**
- Give every signed-in account a resolved avatar with deterministic precedence: manual override > Zitadel `picture` > initials.
- Let the user upload an override (validated) and remove it to revert.
- Fix the Zitadel `picture` read so social/console avatars actually populate.

**Non-Goals:**
- **IdP → Zitadel avatar propagation (Zitadel Actions).** Deferred to a follow-up change; this change only consumes whatever `picture` Zitadel already emits.
- Server-side re-encode/crop normalization (only sniff + size/dimension caps now).
- Org/project avatars (this is the signed-in account's personal avatar only).
- iOS: consumes the same resolved URL via existing member/profile DTOs; no native avatar UI in this change.

## Decisions

### D1 — Resolution precedence and resolution point: the gateway

```
resolvedAvatar = AvatarOverrideURL ?? ZitadelPicture ?? initials
```

Resolution happens in the **gateway**, not the backend. The gateway already holds both inputs (session `Picture` = Zitadel, profile `AvatarObjectKey` = override) and renders the topbar; the backend stays a dumb store. *Alternative:* resolve in the backend and return one `avatarUrl` — rejected because it would hide the override/social distinction the profile page needs to show "Remove photo" vs "not overridden".

### D2 — Override storage: Memory object storage + `avatar_object_key`

Override is stored as an object in Memory's object storage, key recorded in `core.user_profiles.avatar_object_key` (the existing, unused column). *Alternative:* Zitadel Assets API (`POST /assets/v1/users/me/avatar`) — rejected (confirmed in research): single-slot model has no clean "remove → revert to social", and couples the override to Zitadel's storage.

### D3 — Serving: a dedicated, cacheable avatar endpoint (not presigned URLs)

`GET /api/user/avatar` streams the current avatar from object storage with auth-gating and `Cache-Control` + a version query param. *Alternative:* return a 15-minute presigned URL in the profile DTO — rejected because the avatar is shown in the topbar on every page; a short-TTL URL produces broken `<img>` tags after expiry.

The gateway surface is `UserProfileDto.AvatarUrl` (a stable, versioned URL the gateway renders directly). The backend generates it from `avatar_object_key`.

### D4 — Fix the Zitadel picture source: userinfo, not ID token

At login, fetch the **OIDC userinfo endpoint** with the access token and read `picture` (and name/email) from it, replacing the ID-token decode as the identity source. *Alternative:* enable Zitadel's `idTokenUserinfoAssertion` app setting and keep decoding the ID token — rejected: requires Zitadel app config and is still fragile across token types.

### D5 — Social sync: deferred

No IdP-picture propagation in this change. The gateway reads whatever Zitadel `picture` exists (console-set avatar, or a future Actions-configured picture). IdP → Zitadel propagation via Zitadel Actions is tracked as a follow-up.

### D6 — Session propagation: two explicit fields, override re-issued on change

`sessionClaims` keeps `Picture` (= Zitadel picture) and gains `AvatarOverrideURL`. At login both are set (override from `GetProfile`, social from userinfo). Upload/remove re-issues the session cookie with the new `AvatarOverrideURL`, so the topbar updates on the next navigation without re-login and without a per-render `GetProfile` round-trip. *Alternative:* resolve override via `GetProfile` on every render — rejected for latency (extra REST call on every page).

## Risks / Trade-offs

- **[Stale social picture]** Zitadel picture is frozen in the cookie until re-login → accepted; the userinfo read fixes the *initial* population, and live social refresh is the deferred follow-up.
- **[Upload validation bypass]** trusting the client extension → mitigation: sniff content-type by magic bytes, `image/` allowlist, reject SVG, 512 KiB cap (mirrors Zitadel), pixel-dimension cap.
- **[Object cleanup]** removing/overwriting an avatar leaks the old object → mitigation: version keys and delete the superseded object on replace/remove (storage service already supports delete).
- **[`avatar_object_key` migration]** the column exists but is empty for all rows → no schema migration needed; only additive DTO/endpoint changes.
- **[Cross-session cache]** versioned `?v=` URLs prevent stale cached avatars after override.

## Migration Plan

1. Backend: add upload/remove/stream endpoints + `AvatarUrl` on the profile DTO (additive; existing rows unaffected).
2. Gateway: userinfo read + `AvatarOverrideURL` in session claims + precedence in `ui.go` + account-menu/profile rendering + upload/remove form.
3. No DB migration, no rollback beyond redeploy (all additive). Session cookies are backward-compatible (new field is `omitempty`).

## Open Questions

- Whether to add server-side re-encode/crop (deferred; sniff + caps suffice for v1).
- Exact cache TTL/`?v=` format for the avatar endpoint (minor; decide at implementation).
