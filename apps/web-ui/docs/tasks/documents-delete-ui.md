# Document delete (gateway/UI)

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-retire-gateway-mcp](sessions/2026-09-09-retire-gateway-mcp.md)

## What

Add a delete path for ingested documents in the gateway web UI / API. The Documents list
and document detail page currently have no delete control, and there is no gateway route or
`MemoryClient` method for deleting a document (no `/documents/:id/delete`, no
`DeleteDocument`). The memory backend exposes document deletion; it is simply not surfaced.

Surfaced during upload-verification cleanup: a test document could not be removed from the
UI, leaving `hx-boost-upload-test.txt` (id `d7fad8bc-a80a-4846-883b-2aa238cddab1`, project
`f131d865`) in the dev tenant.

## Why

Documents are user data; users need to remove mis-uploads. It is a visible gap once upload
exists (a document is otherwise permanent in the UI).

## Depends on

- A memory-backend document-delete endpoint (exists; confirm path/params before wiring).

## Notes

- Add `MemoryClient.DeleteDocument` + a UI delete (confirm dialog, consistent with
  skill/agent delete flows; exempted from hx-boost) or an API route.
- Consider cascade semantics (chunks/extraction graph) — memory backend owns that.
