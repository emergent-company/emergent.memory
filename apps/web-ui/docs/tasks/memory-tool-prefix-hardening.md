# Memory tool-prefix hardening (remaining edges)

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md)

## What

Close the remaining prefix-handling edges left after Emergent Memory PRs #406/#410:

- `ParsePrefixedToolName` (naive first-`_` split) is still used by the manual-sync
  validation path (`domain/mcpregistry/service.go:544`), where it can mis-split server
  names containing underscores; align it with `SlugifyServerName` + the new resolver.
- External pool keys are capped at 64 chars (truncating the slugged server part); a
  pathological combined name then cannot be slug-resolved on call. Either keep a
  slug→server mapping for calls or document the hard limit in the server-name validation.
- `SlugifyServerName` is now shared **without** a migration note for existing persisted
  prefixed whitelists (if any) — add a comment/migration guard as the canonical slug evolves.

## Why

These are correctness edges in the tool-call path; #406/#410 fixed the observed failures
(bare-name whitelist resolution, invalid function names, routing), but the above can still
produce "server not found" or missing tools in corner cases.

## Depends on

- Emergent Memory repo `/root/emergent.memory` (`domain/mcpregistry`, `domain/agents`).

## Notes

- Upstream PRs #406 (`c8bf8656`) and #410 (`687c86da`) are merged; the >64-char limitation
  is documented in `proxy.go` v1 comments.
