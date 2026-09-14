# Verify multipart file upload under hx-boost

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-webui-perf-css](sessions/2026-09-08-webui-perf-css.md)

## What

Manually confirm the Documents upload form (`documents.templ`, `enctype="multipart/form-data"`)
still uploads under hx-boost, now that in-content forms are boosted.

## Why

Not browser-tested at change time: the devtools browser runs on a different machine, so a
local file path could not be handed to the upload input. htmx v4 source shows multipart
forms are sent via `FormData` (not URLSearchParams), so it should work — but it is unproven.

## Depends on

- The hx-boost change (`ab76ea9`). No task/spec dependency.

## Notes

- Upload a small text file on the /documents page and confirm it chunks/lands in the list.
- Also spot-check the "Delete skill/agent" confirm dialog still opens + cancels (it is
  exempt from boost via `hx-boost="false"` on `modalShell`).

**Resolved:** 2026-09-08 — verified via a synthetic `File`/`DataTransfer` in the live browser: the boosted multipart form uploaded successfully (`?uploaded=1`, no full navigation, file listed). htmx v4's FormData handling confirmed. Note: the gateway has no document-delete path, so the test document `hx-boost-upload-test.txt` (doc id `d7fad8bc-a80a-4846-883b-2aa238cddab1`, project f131d865) was left in place — delete via the memory backend/admin.
