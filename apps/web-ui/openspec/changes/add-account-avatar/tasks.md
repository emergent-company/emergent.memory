## 1. Memory backend — avatar storage

- [x] 1.1 Add avatar upload/delete primitives to the storage service (reuse `internal/storage`; versioned keys + delete of superseded object). Verify with a unit test that upload stores a key and delete removes the object.
- [x] 1.2 Add `PUT /api/user/avatar` (multipart): validate by magic bytes (`image/` allowlist, reject SVG, 512 KiB cap), store object, set `core.user_profiles.avatar_object_key`. Verify unit tests cover accepted png/jpeg/webp, rejected svg, rejected non-image, rejected oversize.
- [x] 1.3 Add `GET /api/user/avatar` streaming endpoint with auth-gating + `Cache-Control` and version query param. Verify unit test returns 200 + bytes for an existing avatar and 404 for none.
- [x] 1.4 Add `DELETE /api/user/avatar` clearing `avatar_object_key` and deleting the object. Verify unit test clears the key and removes the object.
- [x] 1.5 Expose `AvatarUrl` on `UserProfileDto` (stable versioned URL, empty when no override). Verify unit test resolves a URL from `avatar_object_key` and empty when unset.

## 2. Gateway — OIDC userinfo + session fields

- [x] 2.1 Fetch the OIDC userinfo endpoint at login and read `picture`/name/email from it, replacing the ID-token decode as the identity source. Verify unit test maps a userinfo response with `picture` into the session claims.
- [x] 2.2 Add `AvatarOverrideURL` to `sessionClaims` (`omitempty`) and populate it at login from the Memory profile when an override exists. Verify unit test sets the field only when `AvatarObjectKey`/`AvatarUrl` is present.
- [x] 2.3 Re-issue the session cookie with the updated `AvatarOverrideURL` after upload and after remove. Verify unit test asserts the re-issued cookie reflects the new override (and clears it on remove).

## 3. Gateway — avatar resolution + UI

- [x] 3.1 Add a pure resolution helper (`override ?? picture ?? initials`) used by the account menu and profile page. Verify unit test covers all three precedence branches.
- [x] 3.2 Render the resolved avatar in the account menu trigger (override > social > initials). Verify UI test (extend `account_menu_test.go`) asserts override wins and initials fallback.
- [x] 3.3 Render the resolved avatar + upload/remove controls on the profile page (`profileAvatar`). Verify UI test asserts avatar shown when override exists and controls visible.

## 4. Gateway — upload/remove handlers + wiring

- [x] 4.1 Add `UploadAvatar`/`DeleteAvatar` to `MemoryBackend` + `MemoryClient` (multipart proxy to the backend endpoints). Verify unit test with the fake backend exercises both paths.
- [x] 4.2 Add upload handler (POST /profile/avatar): proxy upload, re-issue session with new override, redirect with feedback. Verify unit test confirms override propagates to the session on success and errors surface on failure.
- [x] 4.3 Add remove handler (POST /profile/avatar/remove): call delete, clear override in session, redirect with feedback. Verify unit test confirms revert behavior.
- [x] 4.4 Wire the routes in `main.go`. Verify `go build ./...` compiles and routes are registered.

## 5. Verification

- [x] 5.1 Run `templ generate`, `go build ./...`, and `task lint` clean in `gateway/`.
- [ ] 5.2 Manual browser check: upload an avatar on /profile, confirm account-menu and profile avatar update; remove it and confirm revert to Zitadel picture/initials.
