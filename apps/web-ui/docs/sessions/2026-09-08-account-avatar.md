# 2026-09-08 — Account avatar: upload, social sync, override

## Goal
Give the signed-in account a real avatar: auto-synced from social logins via Zitadel when available, overridable by a manual upload, with a fallback to initials. Captured as the OpenSpec change `add-account-avatar`, implemented across two repos, then iterated into a profile-page modal with an idempotent e2e test.

## Outcome
Done, verified end-to-end against the live dev stack.

- **Backend** (`/root/emergent.memory`, PR #384, merged + deployed): `PUT/GET/DELETE /api/user/avatar` + `ProfileDTO.AvatarUrl`.
- **Gateway** (`/root/alfred`): userinfo `picture` fix, `AvatarOverrideURL` session claim, resolution precedence, profile-page photo modal (upload / remove-to-revert), avatar proxy route.
- **E2E**: Playwright spec (mutations project) uploads and removes a photo via the modal; passed against `alfred-dev.tail0358fa.ts.net` (2 passed, ~14s).
- Social auto-sync is **partially delivered**: the gateway reads the Zitadel `picture` claim correctly now, but IdP→Zitadel picture propagation (Zitadel Actions) is **deferred** (see [task](../tasks/zitadel-social-avatar-sync.md)).

## Decisions
- Override stored in **Memory object storage** (`core.user_profiles.avatar_object_key`), not Zitadel's Assets API — the existing column is unused, and Memory gives a clean "remove → revert to social" that Zitadel's single-slot model can't express.
- Resolution point is the **gateway** (`override ?? picture ?? initials`) — it already holds both inputs (session `Picture` + profile `AvatarUrl`) and renders the topbar.
- Avatar served through a **gateway proxy** (`GET /api/user/avatar` → memory stream), not a presigned URL — stable, cacheable, no short-TTL broken `<img>` in the topbar.
- **userinfo endpoint** is the identity source at login, not the ID token — Zitadel omits `profile` claims from ID tokens when an access token is issued (the old decode was a silent no-op).
- Two session fields (`Picture` = Zitadel social; `AvatarOverrideURL` = Memory override) — keeps "revert to social" explicit without a per-render `GetProfile`.
- Social sync (IdP→Zitadel via Actions) **deferred** — this change consumes whatever `picture` Zitadel already emits.
- Modal remove/upload buttons share one row via a `form="…"` attribute — two different form actions can't nest; the attribute is valid HTML5.

## Changes
- `apps/server/domain/userprofile/{entity,repository,service,handler,routes}.go` (emergent.memory) — `AvatarUrl` on DTO (`/api/user/avatar?v=<key>`), `Repository.SetAvatar`, `Service` gains `avatarStore`/`profileRepo` interfaces + `UploadAvatar`/`RemoveAvatar`/`GetAvatar`, three auth-gated handlers, avatar routes.
- `apps/server/internal/testutil/server.go` — updated `NewService` call for the new signature.
- `gateway/oidc.go` — `UserinfoEndpoint` on discovery + `fetchUserInfo`; `authCallback` uses userinfo for name/email/picture, resolves override from Memory profile.
- `gateway/session.go`, `session_context.go`, `account.go`, `auth.go` — `AvatarOverrideURL` claim threaded through session, account cache, and refresh.
- `gateway/memory.go`, `backend.go` — `UserProfileDto.AvatarUrl`; `UploadAvatar`/`DeleteAvatar`/`GetAvatar` on the client + `MemoryBackend` interface.
- `gateway/ui.go` — `resolveAvatar` helper; account menu + profile resolve override > picture.
- `gateway/org_members_ui.templ` — `profileAvatar` (click-to-edit + hover pencil), `profileAvatarModal` (upload + remove), handlers `uiUploadAvatar`/`uiRemoveAvatar`/`avatarProxy`.
- `gateway/main.go` — `POST /profile/avatar`, `POST /profile/avatar/remove`, `GET /api/user/avatar`.
- `gateway/avatar_test.go` — modal render + trigger wiring tests (replaced the inline-section test).
- `tests/e2e/specs/profile-avatar-ui.spec.ts` — idempotent upload/remove flow (remove-first → set → remove).
- `openspec/changes/add-account-avatar/` — proposal, design, spec, tasks (complete, not yet archived).

## Verification
- Backend: `go build ./...`, `go test ./domain/userprofile/... ./internal/storage/...`, `go vet ./domain/userprofile/...` — all pass.
- Gateway: `PATH="/root/go/bin:$PATH" templ generate`, `go build ./...`, `go test ./...`, `task lint` (0 issues) — pass.
- E2E: `npx playwright test specs/profile-avatar-ui.spec.ts` — `2 passed (14.2s)` against the live dev stack (first attempt hit a transient `ECONNREFUSED` while the gateway was mid-restart during deploy; retry passed).
- Commits: gateway `d17eb26`, `5211d6f`, `69c3951`; backend `d3afe67` → PR #384.

## Open questions / follow-ups
- Archive the `add-account-avatar` OpenSpec change (delta spec → main spec) — [task](../tasks/archive-add-account-avatar.md).
- IdP→Zitadel picture propagation via Zitadel Actions (deferred social sync) — [task](../tasks/zitadel-social-avatar-sync.md).
- Minor: gateway upload pre-check reuses the 10 MB `maxUploadSize` (documents) while the backend authoritatively caps at 512 KiB — tighten for a clearer client-side error.
- Server-side avatar re-encode/crop was deferred (sniff + size cap only for v1).

## Tasks
- [archive-add-account-avatar](../tasks/archive-add-account-avatar.md) — archive the OpenSpec change
- [zitadel-social-avatar-sync](../tasks/zitadel-social-avatar-sync.md) — IdP picture propagation via Zitadel Actions
