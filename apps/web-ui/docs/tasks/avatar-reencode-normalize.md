# Avatar re-encode + normalize (square crop, strip EXIF)

**Status:** done
**Created:** 2026-09-08
**Landed:** 2026-09-10 (emergent.memory PR #425)
**Source:** [2026-09-08-account-avatar](../sessions/2026-09-08-account-avatar.md)

## Done (2026-09-10)

`UploadAvatar` now normalizes after validation and before upload: decode (first frame for GIF) →
center-crop to the largest square → `x/image/draw` CatmullRom scale to 512×512 → encode PNG when
the source has any non-opaque pixel, else JPEG q85 (re-encode strips EXIF/metadata). Storage key
extension/content-type follow the chosen format. New `avatar_normalize.go` + unit tests
(non-square JPEG, transparent PNG, animated GIF flatten, small square, service-level stored
object). Ratchet unchanged; build/vet/test green.

## What
Normalize uploaded avatars server-side: decode → center-crop to square → resize to
512×512 → re-encode (PNG or JPEG), stripping EXIF/metadata and flattening animated GIFs
to a single frame.

## Why
Upload validation only sniffs magic bytes + caps file size (and, after the validation
hardening, dimensions). It does not normalize: non-square images rely on client-side
`object-fit` cropping, EXIF/GPS metadata leaks location, and animated GIFs bloat storage
+ bandwidth. Re-encoding strips metadata and yields a consistent square.

## Depends on
- avatar upload dimension/decode validation (the 2a security hardening this builds on).

## Notes
- Backend `domain/userprofile/service.go` `UploadAvatar` is the place (after validation,
  before `storage.Upload`).
- Stdlib `image/png`/`image/jpeg` re-encode without new deps; use `golang.org/x/image/draw`
  for the center-crop + scale if it's already pulled in.
- Decide output format: PNG preserves transparency; JPEG is smaller for photos.
- Re-encode drops metadata automatically, so no separate EXIF-strip step is needed.
