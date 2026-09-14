# 2026-09-09 — Account avatar: upload hardening + validation

## Goal
Close the two gaps flagged after the account-avatar feature shipped: (1) the gateway upload pre-check used the 10 MB document cap instead of the backend's 512 KiB avatar cap; (2) the backend only sniffed magic bytes + capped file size, leaving decompression bombs and malformed images unvalidated. Also capture the deferred re-encode/normalize as a tracked task.

## Outcome
Done, verified.

- **Fix 1** (gateway size cap) — `avatarMaxUploadSize = 512 KiB` mirrors the backend; oversize rejected client-side before hitting memory. Committed `bc905e3`.
- **Fix 2a** (backend validation) — dimension cap (4096×4096) + full-decode integrity check on upload. Committed `c44ba68e` → **PR #392** (merged 2026-09-09, deployed).
- **Fix 2b** (normalize) — captured as task [avatar-reencode-normalize](../tasks/avatar-reencode-normalize.md), not implemented.
- **E2E** — `profile-avatar-ui.spec.ts` still passes against the live stack after the backend deploy (2 passed).

## Decisions
- Gateway cap mirrors the backend `maxAvatarSize` (512 KiB) — single source of truth; reject before the round-trip, with a clear message.
- `image.DecodeConfig` (header-only) gates dimensions **before** full `image.Decode` — rejects decompression bombs before the full pixel buffer is allocated.
- Pin `golang.org/x/image v0.45.0` (go 1.25) — `v0.46.0` requires go 1.26, but backend CI pins `GO_VERSION=1.25`; the `go` directive stays 1.25.0.
- Reverted the fixer's `go mod tidy` side-effects — it bumped the `go` directive to 1.26 and pruned swag/go-openapi requires (needed for swagger generation); replaced with a targeted `go get …@v0.45.0` + manual go.mod edit.
- Re-encode/normalize deferred to a task — privacy/consistency follow-up, not blocking (UI already `object-fit`s the round avatar).

## Changes
- `gateway/org_members_ui.go` — `avatarMaxUploadSize` const; `uiUploadAvatar` checks it.
- `gateway/avatar_test.go` — `TestUIUploadAvatarOversize`.
- `apps/server/domain/userprofile/handler.go` (emergent.memory) — `maxAvatarDimension` + `image.DecodeConfig`/`image.Decode` validation; decoder blank imports (`image/png`, `image/jpeg`, `image/gif`, `golang.org/x/image/webp`).
- `apps/server/domain/userprofile/handler_test.go` — real encoded 1×1 PNG/JPEG fixtures (old signature-only fixtures wouldn't decode); `TestUpload_OversizedDimensions_Rejected`, `TestUpload_MalformedPNG_Rejected`.
- `apps/server/go.mod`/`go.sum`/`go.work.sum` — add `golang.org/x/image v0.45.0` (direct) + transitive x/ bumps.
- `docs/tasks/avatar-reencode-normalize.md` + `docs/tasks/BACKLOG.md` — new task.

## Verification
- Gateway: `go build ./...`, `go test ./...` — pass.
- Backend: `go build ./...`, `go test ./domain/userprofile/...`, `go vet ./domain/userprofile/...`, `go mod verify` — pass.
- E2E: `npx playwright test specs/account/profile-avatar-ui.spec.ts` — `2 passed (11.6s)` (against the live dev stack, post-deploy).

## Open questions / follow-ups
- The e2e spec was moved by a parallel reorg: `tests/e2e/specs/profile-avatar-ui.spec.ts` → `tests/e2e/specs/account/profile-avatar-ui.spec.ts` (committed by that session).
- [avatar-reencode-normalize](../tasks/avatar-reencode-normalize.md) — re-encode/crop (2b), still open.
- Carried over from the prior session: [archive-add-account-avatar](../tasks/archive-add-account-avatar.md), [zitadel-social-avatar-sync](../tasks/zitadel-social-avatar-sync.md).

## Tasks
- [avatar-reencode-normalize](../tasks/avatar-reencode-normalize.md) — re-encode + normalize avatars (square crop, strip EXIF)
